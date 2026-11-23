// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Versioned connection credential heads and immutable private material.

use std::collections::HashMap;
use std::fmt;
use std::sync::Arc;

use base64::Engine;
use serde_json::Value;

use super::broker::SharedConnection;
use super::errors::{ProjectConfigError, ProjectConfigResult};
use super::models::{
    JsonObject, Scope, digest_bytes, go_canonical_json_bytes, is_connector_id, is_lower_hex,
    require_object,
};
use super::storage::{ObjectStore, ProjectObject};
use crate::timestamps::Rfc3339NanoTimestamp;

/// Connection head and credential envelope schema.
pub const CONNECTION_SCHEMA_VERSION: &str = "connectors.dex.dev/project-connection/v1alpha2";

const CONNECTION_HEAD_FIELDS: [&str; 10] = [
    "schemaVersion",
    "connection",
    "scope",
    "revision",
    "fence",
    "status",
    "expiresAt",
    "authMethod",
    "credential",
    "mutation",
];

/// Manifest connector ID and application-declared connection name.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ConnectionKey {
    /// Manifest connector ID.
    pub connector_id: String,
    /// Application-declared connection name.
    pub connection_name: String,
}

impl ConnectionKey {
    /// Creates a key without validating it.
    pub fn new(connector_id: impl Into<String>, connection_name: impl Into<String>) -> Self {
        Self {
            connector_id: connector_id.into(),
            connection_name: connection_name.into(),
        }
    }

    /// Encodes the stable connection key.
    pub fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert(
            "connectorId".into(),
            Value::String(self.connector_id.clone()),
        );
        object.insert(
            "connectionName".into(),
            Value::String(self.connection_name.clone()),
        );
        Value::Object(object)
    }

    /// Returns `<scope>/connections/<connector>/<unpadded base64url name>` after validation.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid connector ID or name.
    pub fn path(&self, scope: &Scope) -> ProjectConfigResult<String> {
        if !is_connector_id(&self.connector_id)
            || self.connection_name.is_empty()
            || self.connection_name.trim() != self.connection_name
            || self.connection_name.chars().count() > 256
            || self.connection_name.contains(['\0', '\r', '\n'])
        {
            return Err(ProjectConfigError::invalid(
                "invalid project connection key",
            ));
        }
        let encoded = base64::engine::general_purpose::URL_SAFE_NO_PAD
            .encode(self.connection_name.as_bytes());
        Ok(format!(
            "{}/connections/{}/{encoded}",
            scope.prefix()?,
            self.connector_id
        ))
    }
}

/// Private connector credential object; never place it in Flow state or logs.
#[derive(Clone, PartialEq)]
pub struct CredentialMaterial {
    /// Provider credential members in document order.
    pub credentials: JsonObject,
    /// Expiry when the provider declares one.
    pub expires_at: Option<Rfc3339NanoTimestamp>,
    /// Authorization method ID.
    pub auth_method: String,
}

impl fmt::Debug for CredentialMaterial {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("CredentialMaterial([REDACTED])")
    }
}

/// Safe authoritative connection metadata without credential bytes.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Connection {
    /// Connection key.
    pub key: ConnectionKey,
    /// Storage scope.
    pub scope: Scope,
    /// Head revision.
    pub revision: u64,
    /// Credential generation fence.
    pub fence: u64,
    /// `READY`, `REFRESHING`, `AUTHORIZING`, `REAUTHORIZATION_REQUIRED`, or `REVOKED`.
    pub status: String,
    /// Credential expiry when declared.
    pub expires_at: Option<Rfc3339NanoTimestamp>,
    /// Authorization method ID.
    pub auth_method: String,
}

/// Builds the credential envelope in sdkgo field order.
pub(crate) fn credential_envelope_wire(
    scope: &Scope,
    key: &ConnectionKey,
    fence: u64,
    credentials: &JsonObject,
    expires_at: Option<&Rfc3339NanoTimestamp>,
    auth_method: &str,
    attempt_id: &str,
) -> Value {
    let mut object = JsonObject::new();
    object.insert(
        "schemaVersion".into(),
        Value::String(CONNECTION_SCHEMA_VERSION.into()),
    );
    object.insert("scope".into(), scope.to_wire());
    object.insert("connection".into(), key.to_wire());
    object.insert("fence".into(), Value::from(fence));
    if !attempt_id.is_empty() {
        object.insert("attemptId".into(), Value::String(attempt_id.into()));
    }
    object.insert("credentials".into(), Value::Object(credentials.clone()));
    if let Some(expires_at) = expires_at {
        object.insert("expiresAt".into(), Value::String(expires_at.to_string()));
    }
    object.insert("authMethod".into(), Value::String(auth_method.into()));
    Value::Object(object)
}

/// Exact private credential object version recorded in a connection head.
#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) struct CredentialReference {
    pub(crate) key: String,
    pub(crate) version: String,
    pub(crate) digest: String,
}

impl CredentialReference {
    fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert("key".into(), Value::String(self.key.clone()));
        object.insert("version".into(), Value::String(self.version.clone()));
        object.insert("digest".into(), Value::String(self.digest.clone()));
        Value::Object(object)
    }

    pub(crate) fn from_record(value: Option<&Value>) -> ProjectConfigResult<Self> {
        let record = require_object(value, "credential reference")?;
        let text = |name: &str| match record.get(name) {
            Some(Value::String(text)) => Ok(text.clone()),
            _ => Err(ProjectConfigError::invalid(
                "project credential reference is invalid",
            )),
        };
        Ok(Self {
            key: text("key")?,
            version: text("version")?,
            digest: text("digest")?,
        })
    }
}

/// Credential exchange admission recorded in a `REFRESHING` or `AUTHORIZING` head.
#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) struct CredentialMutation {
    pub(crate) kind: String,
    pub(crate) invocation_id: String,
    pub(crate) id: String,
    pub(crate) fence: u64,
    pub(crate) base_revision: u64,
    pub(crate) deadline: Rfc3339NanoTimestamp,
    pub(crate) result_key: String,
}

impl CredentialMutation {
    fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert("kind".into(), Value::String(self.kind.clone()));
        object.insert(
            "invocationId".into(),
            Value::String(self.invocation_id.clone()),
        );
        object.insert("id".into(), Value::String(self.id.clone()));
        object.insert("fence".into(), Value::from(self.fence));
        object.insert("baseRevision".into(), Value::from(self.base_revision));
        object.insert("deadline".into(), Value::String(self.deadline.to_string()));
        object.insert("resultKey".into(), Value::String(self.result_key.clone()));
        Value::Object(object)
    }
}

/// Builds a connection head in sdkgo field order.
#[expect(
    clippy::too_many_arguments,
    reason = "mirrors every sdkgo connection head field in declaration order"
)]
pub(crate) fn connection_record_wire(
    scope: &Scope,
    key: &ConnectionKey,
    revision: u64,
    fence: u64,
    status: &str,
    expires_at: Option<&Rfc3339NanoTimestamp>,
    auth_method: &str,
    credential_reference: Option<&CredentialReference>,
    mutation: Option<&CredentialMutation>,
) -> Value {
    let mut object = JsonObject::new();
    object.insert(
        "schemaVersion".into(),
        Value::String(CONNECTION_SCHEMA_VERSION.into()),
    );
    object.insert("connection".into(), key.to_wire());
    object.insert("scope".into(), scope.to_wire());
    object.insert("revision".into(), Value::from(revision));
    object.insert("fence".into(), Value::from(fence));
    object.insert("status".into(), Value::String(status.into()));
    if let Some(expires_at) = expires_at {
        object.insert("expiresAt".into(), Value::String(expires_at.to_string()));
    }
    object.insert("authMethod".into(), Value::String(auth_method.into()));
    if let Some(reference) = credential_reference {
        object.insert("credential".into(), reference.to_wire());
    }
    if let Some(mutation) = mutation {
        object.insert("mutation".into(), mutation.to_wire());
    }
    Value::Object(object)
}

/// Validated connection head as stored.
#[derive(Clone, Debug)]
pub(crate) struct ConnectionRecord {
    pub(crate) document: JsonObject,
    pub(crate) revision: u64,
    pub(crate) fence: u64,
    pub(crate) status: String,
    pub(crate) expires_at: Option<Rfc3339NanoTimestamp>,
    pub(crate) auth_method: String,
    pub(crate) credential: Option<CredentialReference>,
    pub(crate) mutation: Option<CredentialMutation>,
}

pub(crate) fn parse_optional_timestamp(
    value: Option<&Value>,
) -> ProjectConfigResult<Option<Rfc3339NanoTimestamp>> {
    match value {
        None | Some(Value::Null) => Ok(None),
        Some(Value::String(text)) => Rfc3339NanoTimestamp::from_wire(text)
            .map(Some)
            .map_err(|_| ProjectConfigError::invalid("invalid credential expiry")),
        Some(_) => Err(ProjectConfigError::invalid("invalid credential expiry")),
    }
}

/// Reads ordinary credential material and routes loaded shared entries to the broker.
///
/// Shared entries are constructor-injected from the pinned project document; their
/// credentials stay broker-owned and never enter stored connection heads.
#[derive(Clone)]
pub struct ConnectionStore {
    objects: Arc<dyn ObjectStore>,
    scope: Scope,
    shared_connections: HashMap<ConnectionKey, Arc<SharedConnection>>,
}

impl ConnectionStore {
    /// Binds ordinary connections in `objects` beneath `scope`.
    pub fn new(objects: Arc<dyn ObjectStore>, scope: Scope) -> Self {
        Self::with_shared_connections(objects, scope, HashMap::new())
    }

    /// Binds ordinary connections plus broker-owned shared entries.
    pub fn with_shared_connections(
        objects: Arc<dyn ObjectStore>,
        scope: Scope,
        shared_connections: HashMap<ConnectionKey, Arc<SharedConnection>>,
    ) -> Self {
        Self {
            objects,
            scope,
            shared_connections,
        }
    }

    /// Returns the object store.
    pub fn objects(&self) -> &Arc<dyn ObjectStore> {
        &self.objects
    }

    /// Returns the scope.
    pub fn scope(&self) -> &Scope {
        &self.scope
    }

    pub(crate) fn shared_connection(&self, key: &ConnectionKey) -> Option<Arc<SharedConnection>> {
        self.shared_connections.get(key).cloned()
    }

    /// Reads safe metadata without refreshing credentials.
    ///
    /// # Errors
    ///
    /// Returns a storage error or [`ProjectConfigError::Invalid`] for an invalid head.
    pub fn read_connection(&self, key: &ConnectionKey) -> ProjectConfigResult<Connection> {
        let (record, _) = self.read_record(key)?;
        Ok(self.connection(key, &record))
    }

    /// Reads exact private material for trusted provider boundaries.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] when the credential is unavailable or its
    /// integrity or identity differs.
    pub fn read_credential_material(
        &self,
        key: &ConnectionKey,
    ) -> ProjectConfigResult<(CredentialMaterial, Connection)> {
        let (record, _) = self.read_record(key)?;
        let connection = self.connection(key, &record);
        if connection.status != "READY" {
            return Err(ProjectConfigError::invalid(
                "project credential is unavailable",
            ));
        }
        let material = self.read_material(key, &record)?;
        Ok((material, connection))
    }

    pub(crate) fn read_material(
        &self,
        key: &ConnectionKey,
        record: &ConnectionRecord,
    ) -> ProjectConfigResult<CredentialMaterial> {
        let reference = record
            .credential
            .as_ref()
            .ok_or_else(|| ProjectConfigError::invalid("project credential reference is absent"))?;
        let item = self
            .objects
            .read_object(&reference.key, &reference.version)?;
        if digest_bytes(&item.contents)[7..] != reference.digest {
            return Err(ProjectConfigError::invalid(
                "project credential integrity differs",
            ));
        }
        let envelope: Value = serde_json::from_slice(&item.contents).map_err(|_| {
            ProjectConfigError::invalid("credential envelope must be a JSON object")
        })?;
        let envelope = require_object(Some(&envelope), "credential envelope")?;
        if envelope.get("schemaVersion") != Some(&Value::String(CONNECTION_SCHEMA_VERSION.into()))
            || envelope.get("scope") != Some(&self.scope.to_wire())
            || envelope.get("connection") != Some(&key.to_wire())
            || envelope.get("authMethod") != record.document.get("authMethod")
            || envelope.get("expiresAt") != record.document.get("expiresAt")
        {
            return Err(ProjectConfigError::invalid(
                "project credential identity differs",
            ));
        }
        Ok(CredentialMaterial {
            credentials: require_object(envelope.get("credentials"), "credentials")?.clone(),
            expires_at: parse_optional_timestamp(envelope.get("expiresAt"))?,
            auth_method: record.auth_method.clone(),
        })
    }

    /// Fences earlier work and publishes a new immutable credential version.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::ObjectConflict`] when the stored revision differs from
    /// `expected_revision`, or a storage or validation error.
    pub fn replace_credential(
        &self,
        key: &ConnectionKey,
        expected_revision: u64,
        material: &CredentialMaterial,
    ) -> ProjectConfigResult<Connection> {
        if material.auth_method.is_empty() {
            return Err(ProjectConfigError::invalid("invalid credential material"));
        }
        let path = key.path(&self.scope)?;
        let (previous_revision, previous_fence, head) = match self.read_record(key) {
            Ok((record, head)) => (record.revision, record.fence, Some(head)),
            Err(ProjectConfigError::ObjectNotFound) if expected_revision == 0 => (0, 0, None),
            Err(error) => return Err(error),
        };
        if previous_revision != expected_revision {
            return Err(ProjectConfigError::ObjectConflict(
                "connection revision conflict".into(),
            ));
        }
        let fence = previous_fence + 1;
        let envelope = credential_envelope_wire(
            &self.scope,
            key,
            fence,
            &material.credentials,
            material.expires_at.as_ref(),
            &material.auth_method,
            "",
        );
        let contents = go_canonical_json_bytes(&envelope)?;
        let digest = digest_bytes(&contents)[7..].to_string();
        let immutable_key = format!("{path}/credentials/{digest}");
        let candidate = self.create_or_match(
            &immutable_key,
            &contents,
            "immutable credential content differs",
        )?;
        let reference = CredentialReference {
            key: immutable_key,
            version: candidate.version,
            digest,
        };
        let new_record = connection_record_wire(
            &self.scope,
            key,
            expected_revision + 1,
            fence,
            "READY",
            material.expires_at.as_ref(),
            &material.auth_method,
            Some(&reference),
            None,
        );
        let head_contents = go_canonical_json_bytes(&new_record)?;
        let head_key = format!("{path}/head");
        let written = match &head {
            None => self.objects.create_object(&head_key, &head_contents),
            Some(head) => {
                self.objects
                    .compare_and_swap_object(&head_key, &head.etag, &head_contents)
            }
        };
        match written {
            Ok(_) => {}
            Err(ProjectConfigError::OutcomeUnknown(message)) => {
                let observed = self.objects.read_object(&head_key, "")?;
                if observed.contents != head_contents {
                    return Err(ProjectConfigError::OutcomeUnknown(message));
                }
            }
            Err(error) => return Err(error),
        }
        Ok(Connection {
            key: key.clone(),
            scope: self.scope.clone(),
            revision: expected_revision + 1,
            fence,
            status: "READY".into(),
            expires_at: material.expires_at.clone(),
            auth_method: material.auth_method.clone(),
        })
    }

    /// Creates an immutable object, or accepts an identical existing one.
    pub(crate) fn create_or_match(
        &self,
        key: &str,
        contents: &[u8],
        differs_message: &str,
    ) -> ProjectConfigResult<ProjectObject> {
        match self.objects.create_object(key, contents) {
            Ok(item) => Ok(item),
            Err(ProjectConfigError::ObjectConflict(_) | ProjectConfigError::OutcomeUnknown(_)) => {
                let item = self.objects.read_object(key, "")?;
                if item.contents != contents {
                    return Err(ProjectConfigError::invalid(differs_message));
                }
                Ok(item)
            }
            Err(error) => Err(error),
        }
    }

    pub(crate) fn read_record(
        &self,
        key: &ConnectionKey,
    ) -> ProjectConfigResult<(ConnectionRecord, ProjectObject)> {
        let path = key.path(&self.scope)?;
        let item = self.objects.read_object(&format!("{path}/head"), "")?;
        let invalid = || ProjectConfigError::invalid("project connection identity is invalid");
        let document: Value = serde_json::from_slice(&item.contents)
            .map_err(|_| ProjectConfigError::invalid("connection head must be a JSON object"))?;
        let document = require_object(Some(&document), "connection head")?.clone();
        let positive = |name: &str| {
            document
                .get(name)
                .and_then(Value::as_u64)
                .filter(|value| *value > 0)
        };
        let status = document
            .get("status")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string();
        let (Some(revision), Some(fence)) = (positive("revision"), positive("fence")) else {
            return Err(invalid());
        };
        if item.version.is_empty()
            || item.etag.is_empty()
            || document
                .keys()
                .any(|field| !CONNECTION_HEAD_FIELDS.contains(&field.as_str()))
            || document.get("schemaVersion")
                != Some(&Value::String(CONNECTION_SCHEMA_VERSION.into()))
            || document.get("scope") != Some(&self.scope.to_wire())
            || document.get("connection") != Some(&key.to_wire())
            || !matches!(
                status.as_str(),
                "READY" | "REFRESHING" | "AUTHORIZING" | "REAUTHORIZATION_REQUIRED" | "REVOKED"
            )
        {
            return Err(invalid());
        }
        let reference_value = document.get("credential").filter(|value| !value.is_null());
        let mutation_value = document.get("mutation").filter(|value| !value.is_null());
        if matches!(status.as_str(), "READY" | "REFRESHING") && reference_value.is_none() {
            return Err(ProjectConfigError::invalid(
                "project credential reference is absent",
            ));
        }
        if status == "REVOKED" && (reference_value.is_some() || mutation_value.is_some()) {
            return Err(ProjectConfigError::invalid(
                "revoked connection retains active material",
            ));
        }
        let credential = match reference_value {
            None => None,
            Some(value) => {
                let reference = CredentialReference::from_record(Some(value))?;
                if !reference.key.starts_with(&format!("{path}/"))
                    || reference.version.is_empty()
                    || !is_lower_hex(&reference.digest, 64)
                {
                    return Err(ProjectConfigError::invalid(
                        "project credential reference is invalid",
                    ));
                }
                Some(reference)
            }
        };
        let mutation = if matches!(status.as_str(), "REFRESHING" | "AUTHORIZING") {
            Some(Self::parse_mutation(
                mutation_value,
                &status,
                revision,
                fence,
                &path,
            )?)
        } else if mutation_value.is_some() {
            return Err(ProjectConfigError::invalid(
                "inactive credential exchange admission is invalid",
            ));
        } else {
            None
        };
        let record = ConnectionRecord {
            revision,
            fence,
            expires_at: parse_optional_timestamp(document.get("expiresAt"))?,
            auth_method: document
                .get("authMethod")
                .and_then(Value::as_str)
                .unwrap_or_default()
                .to_string(),
            status,
            credential,
            mutation,
            document,
        };
        Ok((record, item))
    }

    fn parse_mutation(
        value: Option<&Value>,
        status: &str,
        revision: u64,
        fence: u64,
        path: &str,
    ) -> ProjectConfigResult<CredentialMutation> {
        let absent = || ProjectConfigError::invalid("credential exchange admission is absent");
        let invalid = || ProjectConfigError::invalid("credential exchange admission is invalid");
        let mutation = value.and_then(Value::as_object).ok_or_else(absent)?;
        let text = |name: &str| {
            mutation
                .get(name)
                .and_then(Value::as_str)
                .unwrap_or_default()
        };
        let attempt_id = text("id");
        let is_attempt_id = (1..=128).contains(&attempt_id.len())
            && attempt_id
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || byte == b'_' || byte == b'-');
        let deadline = mutation
            .get("deadline")
            .and_then(Value::as_str)
            .and_then(|deadline| Rfc3339NanoTimestamp::from_wire(deadline).ok());
        let (Some(deadline), true) = (deadline, is_attempt_id) else {
            return Err(invalid());
        };
        if text("kind") != status
            || text("invocationId").is_empty()
            || mutation.get("fence").and_then(Value::as_u64) != Some(fence)
            || mutation.get("baseRevision").and_then(Value::as_u64) != Some(revision - 1)
            || text("resultKey") != format!("{path}/exchange-results/{attempt_id}")
        {
            return Err(invalid());
        }
        Ok(CredentialMutation {
            kind: status.to_string(),
            invocation_id: text("invocationId").to_string(),
            id: attempt_id.to_string(),
            fence,
            base_revision: revision - 1,
            deadline,
            result_key: text("resultKey").to_string(),
        })
    }

    fn connection(&self, key: &ConnectionKey, record: &ConnectionRecord) -> Connection {
        Connection {
            key: key.clone(),
            scope: self.scope.clone(),
            revision: record.revision,
            fence: record.fence,
            status: record.status.clone(),
            expires_at: record.expires_at.clone(),
            auth_method: record.auth_method.clone(),
        }
    }
}

/// Test-visible encoders that the conformance tests compare byte for byte.
#[doc(hidden)]
pub mod wire {
    use super::*;

    /// Encodes a credential envelope; `attempt_id` is empty outside an exchange result.
    pub fn credential_envelope(
        scope: &Scope,
        key: &ConnectionKey,
        fence: u64,
        credentials: &JsonObject,
        expires_at: Option<&Rfc3339NanoTimestamp>,
        auth_method: &str,
        attempt_id: &str,
    ) -> Value {
        credential_envelope_wire(
            scope,
            key,
            fence,
            credentials,
            expires_at,
            auth_method,
            attempt_id,
        )
    }

    /// Encodes a connection head from wire `credential` and `mutation` objects.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for a malformed reference or mutation.
    #[expect(
        clippy::too_many_arguments,
        reason = "mirrors every sdkgo connection head field in declaration order"
    )]
    pub fn connection_record(
        scope: &Scope,
        key: &ConnectionKey,
        revision: u64,
        fence: u64,
        status: &str,
        expires_at: Option<&Rfc3339NanoTimestamp>,
        auth_method: &str,
        credential: Option<&Value>,
        mutation: Option<&Value>,
    ) -> ProjectConfigResult<Value> {
        let reference = credential
            .map(|value| CredentialReference::from_record(Some(value)))
            .transpose()?;
        let mutation = match mutation {
            None => None,
            Some(value) => {
                let record = require_object(Some(value), "credential mutation")?;
                let text = |name: &str| {
                    record
                        .get(name)
                        .and_then(Value::as_str)
                        .map(str::to_string)
                        .ok_or_else(|| {
                            ProjectConfigError::invalid("credential mutation is invalid")
                        })
                };
                let number = |name: &str| {
                    record.get(name).and_then(Value::as_u64).ok_or_else(|| {
                        ProjectConfigError::invalid("credential mutation is invalid")
                    })
                };
                Some(CredentialMutation {
                    kind: text("kind")?,
                    invocation_id: text("invocationId")?,
                    id: text("id")?,
                    fence: number("fence")?,
                    base_revision: number("baseRevision")?,
                    deadline: Rfc3339NanoTimestamp::from_wire(&text("deadline")?)?,
                    result_key: text("resultKey")?,
                })
            }
        };
        Ok(connection_record_wire(
            scope,
            key,
            revision,
            fence,
            status,
            expires_at,
            auth_method,
            reference.as_ref(),
            mutation.as_ref(),
        ))
    }
}
