// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Wire models for project configuration, local connections, and snapshots.

use std::collections::BTreeMap;
use std::net::IpAddr;

use serde_json::{Map, Value};
use sha2::{Digest, Sha256};

use super::errors::{ProjectConfigError, ProjectConfigResult};

/// JSON object whose member order is preserved, as Go's raw configuration objects are.
pub type JsonObject = Map<String, Value>;

/// Current project configuration document schema.
pub const CONFIGURATION_SCHEMA_VERSION: &str = "connectors.dex.dev/project-configuration/v1alpha3";
/// Previous schema, which cannot declare a credential broker.
pub const PREVIOUS_CONFIGURATION_SCHEMA_VERSION: &str =
    "connectors.dex.dev/project-configuration/v1alpha2";
/// Dex Web `connections.json` schema.
pub const LOCAL_CONNECTIONS_SCHEMA_VERSION: &str = "connectors.dex.dev/local-connections/v1alpha1";
/// Dex Web `use-configurations.json` schema.
pub const LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION: &str =
    "connectors.dex.dev/local-use-configurations/v1alpha1";
/// Reserved secret object name of the credential broker scope token.
pub const CREDENTIAL_BROKER_TOKEN_SECRET_NAME: &str = "DEX_CREDENTIAL_BROKER_TOKEN";

const RESERVED_ENVIRONMENT_NAMES: [&str; 12] = [
    "PATH",
    "HOME",
    "PORT",
    "HOST",
    "NODE_OPTIONS",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
    "PUBLIC_BASE_URL",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "NO_PROXY",
];
const RESERVED_ENVIRONMENT_PREFIXES: [&str; 6] = ["SUPERVERSE_", "DEX_", "AWS_", "LD_", "GO", "GIT_"];

/// Returns `sha256:` followed by the lowercase hex SHA-256 of `contents`.
pub fn digest_bytes(contents: &[u8]) -> String {
    format!("sha256:{}", sha256_hex(contents))
}

pub(crate) fn sha256_hex(contents: &[u8]) -> String {
    let digest = Sha256::digest(contents);
    let mut hex = String::with_capacity(64);
    for byte in digest {
        hex.push_str(&format!("{byte:02x}"));
    }
    hex
}

/// Encodes Go-compatible compact JSON, keeping object member order.
///
/// # Errors
///
/// Returns [`ProjectConfigError::Invalid`] when the value cannot be encoded.
pub fn go_canonical_json_bytes(value: &Value) -> ProjectConfigResult<Vec<u8>> {
    crate::go_json::to_go_json_vec(value)
        .map_err(|_| ProjectConfigError::invalid("JSON value cannot be encoded"))
}

pub(crate) fn require_object<'value>(
    value: Option<&'value Value>,
    label: &str,
) -> ProjectConfigResult<&'value JsonObject> {
    value
        .and_then(Value::as_object)
        .ok_or_else(|| ProjectConfigError::invalid(format!("{label} must be a JSON object")))
}

pub(crate) fn require_fields(
    record: &JsonObject,
    allowed: &[&str],
    label: &str,
) -> ProjectConfigResult<()> {
    if record.keys().any(|key| !allowed.contains(&key.as_str())) {
        return Err(ProjectConfigError::invalid(format!("{label} has unknown fields")));
    }
    Ok(())
}

/// Returns the member as text the way Python's `str(record.get(name, ""))` reads strings.
pub(crate) fn string_member(record: &JsonObject, name: &str) -> String {
    match record.get(name) {
        Some(Value::String(text)) => text.clone(),
        None | Some(Value::Null) => String::new(),
        Some(other) => other.to_string(),
    }
}

fn is_scope_id(value: &str) -> bool {
    let bytes = value.as_bytes();
    (1..=128).contains(&bytes.len())
        && bytes[0].is_ascii_alphanumeric()
        && bytes[1..]
            .iter()
            .all(|byte| byte.is_ascii_alphanumeric() || *byte == b'_' || *byte == b'-')
}

pub(crate) fn is_connector_id(value: &str) -> bool {
    let bytes = value.as_bytes();
    (2..=63).contains(&bytes.len())
        && bytes[0].is_ascii_lowercase()
        && bytes[1..]
            .iter()
            .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || *byte == b'-')
}

pub(crate) fn is_sha256_digest(value: &str) -> bool {
    value
        .strip_prefix("sha256:")
        .is_some_and(|hex| is_lower_hex(hex, 64))
}

pub(crate) fn is_lower_hex(value: &str, length: usize) -> bool {
    value.len() == length
        && value
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
}

fn is_shared_connection_id(value: &str) -> bool {
    let bytes = value.as_bytes();
    (1..=128).contains(&bytes.len())
        && bytes[0].is_ascii_alphanumeric()
        && bytes[1..]
            .iter()
            .all(|byte| byte.is_ascii_alphanumeric() || b"._:-".contains(byte))
}

/// Rejects names reserved for deployment, SDK, trust, and proxy settings.
pub fn is_application_environment_name(name: &str) -> bool {
    let bytes = name.as_bytes();
    let is_shaped = (1..=128).contains(&bytes.len())
        && bytes[0].is_ascii_uppercase()
        && bytes[1..]
            .iter()
            .all(|byte| byte.is_ascii_uppercase() || byte.is_ascii_digit() || *byte == b'_');
    is_shaped
        && !RESERVED_ENVIRONMENT_NAMES.contains(&name)
        && !RESERVED_ENVIRONMENT_PREFIXES
            .iter()
            .any(|prefix| name.starts_with(prefix))
}

/// Limits UTF-8 startup values to 32768 bytes without NUL characters.
///
/// # Errors
///
/// Returns [`ProjectConfigError::Invalid`] for a NUL or oversized value.
pub fn validate_environment_value(value: &str) -> ProjectConfigResult<()> {
    if value.contains('\0') || value.len() > 32768 {
        return Err(ProjectConfigError::invalid("invalid application environment value"));
    }
    Ok(())
}

/// Requires an exact secret object beneath `scope` and the field `name`.
///
/// # Errors
///
/// Returns [`ProjectConfigError::Invalid`] when the reference differs from the scope.
pub fn validate_secret_reference(
    reference: &ApplicationSecretRef,
    scope: &Scope,
    name: &str,
) -> ProjectConfigResult<()> {
    let prefix = format!("{}/app-secrets/{name}/", scope.prefix()?);
    let is_valid = reference
        .key
        .strip_prefix(&prefix)
        .is_some_and(|suffix| is_lower_hex(suffix, 32))
        && !reference.version.is_empty()
        && reference.version != "null"
        && is_sha256_digest(&reference.digest);
    if !is_valid {
        return Err(ProjectConfigError::invalid(
            "application secret reference differs from scope",
        ));
    }
    Ok(())
}

pub(crate) struct ParsedUrl<'url> {
    pub(crate) scheme: &'url str,
    pub(crate) authority: &'url str,
    pub(crate) host: String,
    pub(crate) has_user_info: bool,
    pub(crate) path: &'url str,
    pub(crate) has_query_or_fragment: bool,
}

pub(crate) fn parse_url(value: &str) -> Option<ParsedUrl<'_>> {
    let (scheme, rest) = value.split_once("://")?;
    let authority_end = rest.find(['/', '?', '#']).unwrap_or(rest.len());
    let authority = &rest[..authority_end];
    let remainder = &rest[authority_end..];
    let path_end = remainder.find(['?', '#']).unwrap_or(remainder.len());
    let has_user_info = authority.contains('@');
    let host_port = authority.rsplit('@').next().unwrap_or("");
    let host = if let Some(bracketed) = host_port.strip_prefix('[') {
        bracketed.split(']').next().unwrap_or("").to_string()
    } else {
        host_port.split(':').next().unwrap_or("").to_string()
    };
    Some(ParsedUrl {
        scheme,
        authority,
        host: host.to_ascii_lowercase(),
        has_user_info,
        path: &remainder[..path_end],
        has_query_or_fragment: path_end < remainder.len(),
    })
}

pub(crate) fn is_private_ip(address: IpAddr) -> bool {
    match address {
        IpAddr::V4(v4) => v4.is_private() || v4.is_loopback() || v4.is_link_local(),
        IpAddr::V6(v6) => {
            v6.is_loopback()
                || (v6.segments()[0] & 0xfe00) == 0xfc00
                || (v6.segments()[0] & 0xffc0) == 0xfe80
        }
    }
}

/// Requires a bounded absolute endpoint; only private hosts may use HTTP.
///
/// # Errors
///
/// Returns [`ProjectConfigError::Invalid`] for an invalid or public HTTP endpoint.
pub fn validate_broker_endpoint(value: &str) -> ProjectConfigResult<()> {
    let invalid =
        || ProjectConfigError::invalid("credential broker endpoint must be an absolute HTTP or HTTPS URL");
    let parsed = parse_url(value).ok_or_else(invalid)?;
    if value.len() > 2048
        || !matches!(parsed.scheme, "http" | "https")
        || parsed.host.is_empty()
        || parsed.has_user_info
        || parsed.has_query_or_fragment
    {
        return Err(invalid());
    }
    if parsed.scheme == "https" {
        return Ok(());
    }
    let host = parsed.host.as_str();
    let is_private = matches!(host, "localhost" | "host.docker.internal")
        || !host.contains('.')
        || host.ends_with(".svc")
        || host.ends_with(".svc.cluster.local")
        || host.parse::<IpAddr>().is_ok_and(is_private_ip);
    if !is_private {
        return Err(ProjectConfigError::invalid(
            "credential broker HTTP endpoint must use a private host",
        ));
    }
    Ok(())
}

/// Trusted project and Live or Preview storage boundary.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct Scope {
    /// Project identifier, 1 to 128 characters.
    pub project_id: String,
    /// `live` or `preview`.
    pub kind: String,
}

impl Scope {
    /// Creates a scope without validating it.
    pub fn new(project_id: impl Into<String>, kind: impl Into<String>) -> Self {
        Self {
            project_id: project_id.into(),
            kind: kind.into(),
        }
    }

    /// Returns the canonical prefix `projects/<project>/<kind>` after validation.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid project ID or kind.
    pub fn prefix(&self) -> ProjectConfigResult<String> {
        if !is_scope_id(&self.project_id) || !matches!(self.kind.as_str(), "live" | "preview") {
            return Err(ProjectConfigError::invalid("invalid project configuration scope"));
        }
        Ok(format!("projects/{}/{}", self.project_id, self.kind))
    }

    /// Encodes the Go scope shape.
    pub fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert("projectId".into(), Value::String(self.project_id.clone()));
        object.insert("kind".into(), Value::String(self.kind.clone()));
        Value::Object(object)
    }

    /// Decodes and validates a scope object.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for a malformed or invalid scope.
    pub fn from_wire(value: &Value) -> ProjectConfigResult<Self> {
        let record = require_object(Some(value), "scope")?;
        require_fields(record, &["projectId", "kind"], "scope")?;
        let scope = Scope::new(string_member(record, "projectId"), string_member(record, "kind"));
        scope.prefix()?;
        Ok(scope)
    }
}

/// Exact version and digest of an immutable ordinary configuration document.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SnapshotRef {
    /// `<scope prefix>/configuration/head`.
    pub key: String,
    /// Exact storage version.
    pub version: String,
    /// `sha256:` digest of the document bytes.
    pub digest: String,
    /// Always `application/json`.
    pub media_type: String,
}

impl SnapshotRef {
    /// Creates a JSON snapshot reference.
    pub fn new(
        key: impl Into<String>,
        version: impl Into<String>,
        digest: impl Into<String>,
    ) -> Self {
        Self {
            key: key.into(),
            version: version.into(),
            digest: digest.into(),
            media_type: "application/json".into(),
        }
    }

    /// Requires this scope's head, an exact version, and a SHA-256 digest.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for any mismatch.
    pub fn validate(&self, scope: &Scope) -> ProjectConfigResult<()> {
        if self.key != format!("{}/configuration/head", scope.prefix()?)
            || self.version.is_empty()
            || self.version == "null"
            || !is_sha256_digest(&self.digest)
            || self.media_type != "application/json"
        {
            return Err(ProjectConfigError::invalid(
                "invalid configuration snapshot reference",
            ));
        }
        Ok(())
    }

    /// Encodes the Go snapshot reference shape.
    pub fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert("key".into(), Value::String(self.key.clone()));
        object.insert("version".into(), Value::String(self.version.clone()));
        object.insert("digest".into(), Value::String(self.digest.clone()));
        object.insert("mediaType".into(), Value::String(self.media_type.clone()));
        Value::Object(object)
    }

    /// Decodes a complete exact-version reference.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for unknown fields or a non-object.
    pub fn from_wire(value: &Value) -> ProjectConfigResult<Self> {
        let record = require_object(Some(value), "snapshot")?;
        require_fields(record, &["key", "version", "digest", "mediaType"], "snapshot")?;
        Ok(Self {
            key: string_member(record, "key"),
            version: string_member(record, "version"),
            digest: string_member(record, "digest"),
            media_type: string_member(record, "mediaType"),
        })
    }
}

/// Exact private object version that holds one application secret.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApplicationSecretRef {
    /// Object key beneath `<scope>/app-secrets/<name>/`.
    pub key: String,
    /// Exact storage version.
    pub version: String,
    /// `sha256:` digest of the object bytes.
    pub digest: String,
}

impl ApplicationSecretRef {
    /// Creates a reference.
    pub fn new(
        key: impl Into<String>,
        version: impl Into<String>,
        digest: impl Into<String>,
    ) -> Self {
        Self {
            key: key.into(),
            version: version.into(),
            digest: digest.into(),
        }
    }

    /// Encodes a pinned application secret reference.
    pub fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert("key".into(), Value::String(self.key.clone()));
        object.insert("version".into(), Value::String(self.version.clone()));
        object.insert("digest".into(), Value::String(self.digest.clone()));
        Value::Object(object)
    }

    /// Decodes a private reference without loading its material.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for unknown fields, no version, or a bad digest.
    pub fn from_wire(value: Option<&Value>) -> ProjectConfigResult<Self> {
        let record = require_object(value, "application secret reference")?;
        require_fields(record, &["key", "version", "digest"], "application secret reference")?;
        let reference = Self {
            key: string_member(record, "key"),
            version: string_member(record, "version"),
            digest: string_member(record, "digest"),
        };
        if reference.version.is_empty() || !is_sha256_digest(&reference.digest) {
            return Err(ProjectConfigError::invalid("invalid application secret reference"));
        }
        Ok(reference)
    }
}

/// Either one ordinary value or one private pinned secret reference.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum EnvironmentValue {
    /// Ordinary startup value.
    Value(String),
    /// Pinned private secret version.
    SecretRef(ApplicationSecretRef),
}

impl EnvironmentValue {
    /// Encodes the mutually exclusive selection.
    pub fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        match self {
            EnvironmentValue::Value(value) => {
                object.insert("value".into(), Value::String(value.clone()));
            }
            EnvironmentValue::SecretRef(reference) => {
                object.insert("secretRef".into(), reference.to_wire());
            }
        }
        Value::Object(object)
    }

    /// Decodes one selection without exposing secret contents.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] unless exactly one member is present.
    pub fn from_wire(value: &Value) -> ProjectConfigResult<Self> {
        let record = require_object(Some(value), "environment value")?;
        if record.len() == 1 {
            if let Some(Value::String(text)) = record.get("value") {
                return Ok(EnvironmentValue::Value(text.clone()));
            }
            if let Some(reference) = record.get("secretRef") {
                return Ok(EnvironmentValue::SecretRef(ApplicationSecretRef::from_wire(
                    Some(reference),
                )?));
            }
        }
        Err(ProjectConfigError::invalid("invalid environment selection"))
    }
}

/// Ordinary settings and logical credential identity for one named connector.
#[derive(Clone, Debug, PartialEq)]
pub struct ConnectionConfiguration {
    /// Manifest connector ID.
    pub connector_id: String,
    /// Application-declared connection name.
    pub connection_name: String,
    /// Go module path of the connector release.
    pub module_path: String,
    /// Provider name.
    pub provider: String,
    /// Ordinary settings in document member order.
    pub configuration: JsonObject,
    /// Selected authorization method; empty when omitted.
    pub auth_method_id: String,
    /// Broker-owned shared connection ID; empty for ordinary connections.
    pub shared_connection_id: String,
}

impl ConnectionConfiguration {
    /// Creates an ordinary entry without an auth method or shared connection.
    pub fn new(
        connector_id: impl Into<String>,
        connection_name: impl Into<String>,
        module_path: impl Into<String>,
        provider: impl Into<String>,
        configuration: JsonObject,
    ) -> Self {
        Self {
            connector_id: connector_id.into(),
            connection_name: connection_name.into(),
            module_path: module_path.into(),
            provider: provider.into(),
            configuration,
            auth_method_id: String::new(),
            shared_connection_id: String::new(),
        }
    }

    /// Encodes the entry with omitted optional members.
    pub fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert("connectorId".into(), Value::String(self.connector_id.clone()));
        object.insert("connectionName".into(), Value::String(self.connection_name.clone()));
        object.insert("modulePath".into(), Value::String(self.module_path.clone()));
        object.insert("provider".into(), Value::String(self.provider.clone()));
        if !self.auth_method_id.is_empty() {
            object.insert("authMethodId".into(), Value::String(self.auth_method_id.clone()));
        }
        if !self.shared_connection_id.is_empty() {
            object.insert(
                "sharedConnectionId".into(),
                Value::String(self.shared_connection_id.clone()),
            );
        }
        object.insert("configuration".into(), Value::Object(self.configuration.clone()));
        Value::Object(object)
    }

    /// Decodes an ordinary or shared entry.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for unknown fields or a non-object configuration.
    pub fn from_wire(value: &Value) -> ProjectConfigResult<Self> {
        let record = require_object(Some(value), "connection")?;
        require_fields(
            record,
            &[
                "connectorId",
                "connectionName",
                "modulePath",
                "provider",
                "configuration",
                "authMethodId",
                "sharedConnectionId",
            ],
            "connection",
        )?;
        Ok(Self {
            connector_id: string_member(record, "connectorId"),
            connection_name: string_member(record, "connectionName"),
            module_path: string_member(record, "modulePath"),
            provider: string_member(record, "provider"),
            configuration: require_object(record.get("configuration"), "connection configuration")?
                .clone(),
            auth_method_id: string_member(record, "authMethodId"),
            shared_connection_id: string_member(record, "sharedConnectionId"),
        })
    }
}

/// Broker endpoint and exact scope-token reference for shared credentials.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CredentialBrokerConfiguration {
    /// Absolute broker read endpoint.
    pub endpoint: String,
    /// Pinned scope token secret.
    pub token_secret_ref: ApplicationSecretRef,
}

impl CredentialBrokerConfiguration {
    /// Encodes the broker configuration.
    pub fn to_wire(&self) -> Value {
        let mut object = JsonObject::new();
        object.insert("endpoint".into(), Value::String(self.endpoint.clone()));
        object.insert("tokenSecretRef".into(), self.token_secret_ref.to_wire());
        Value::Object(object)
    }

    /// Decodes one broker configuration.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for unknown fields or an invalid token reference.
    pub fn from_wire(value: &Value) -> ProjectConfigResult<Self> {
        let record = require_object(Some(value), "credential broker")?;
        require_fields(record, &["endpoint", "tokenSecretRef"], "credential broker")?;
        Ok(Self {
            endpoint: string_member(record, "endpoint"),
            token_secret_ref: ApplicationSecretRef::from_wire(record.get("tokenSecretRef"))?,
        })
    }
}

/// Revisioned, secret-free project configuration snapshot.
#[derive(Clone, Debug, PartialEq)]
pub struct Configuration {
    /// Storage boundary.
    pub scope: Scope,
    /// Positive revision.
    pub revision: u64,
    /// Connection entries.
    pub connections: Vec<ConnectionConfiguration>,
    /// Trigger binding objects in document order.
    pub trigger_bindings: Vec<JsonObject>,
    /// Operation configuration objects in document order.
    pub operation_configurations: Vec<JsonObject>,
    /// Application environment, sorted by name.
    pub environment: BTreeMap<String, EnvironmentValue>,
    /// Broker for shared connections.
    pub credential_broker: Option<CredentialBrokerConfiguration>,
    /// Document schema version.
    pub schema_version: String,
}

impl Configuration {
    /// Creates a current-schema document with no bindings, environment, or broker.
    pub fn new(scope: Scope, revision: u64, connections: Vec<ConnectionConfiguration>) -> Self {
        Self {
            scope,
            revision,
            connections,
            trigger_bindings: Vec::new(),
            operation_configurations: Vec::new(),
            environment: BTreeMap::new(),
            credential_broker: None,
            schema_version: CONFIGURATION_SCHEMA_VERSION.into(),
        }
    }

    /// Rejects duplicate identities, an invalid schema, and a missing broker selection.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] describing the first violation.
    pub fn validate(&self) -> ProjectConfigResult<()> {
        self.scope.prefix()?;
        if self.schema_version != CONFIGURATION_SCHEMA_VERSION
            && self.schema_version != PREVIOUS_CONFIGURATION_SCHEMA_VERSION
        {
            return Err(ProjectConfigError::invalid("unsupported configuration schema"));
        }
        if self.revision < 1 {
            return Err(ProjectConfigError::invalid("configuration revision is required"));
        }
        if self.connections.len() > 1000
            || self.trigger_bindings.len() > 10000
            || self.operation_configurations.len() > 10000
        {
            return Err(ProjectConfigError::invalid(
                "configuration document exceeds binding limits",
            ));
        }
        let mut identities = std::collections::HashSet::new();
        for entry in &self.connections {
            if !identities.insert((entry.connector_id.as_str(), entry.connection_name.as_str())) {
                return Err(ProjectConfigError::invalid("duplicate project connection"));
            }
            if !is_connector_id(&entry.connector_id)
                || entry.connection_name.is_empty()
                || entry.module_path.is_empty()
                || entry.provider.is_empty()
            {
                return Err(ProjectConfigError::invalid("invalid project connection"));
            }
        }
        let mut trigger_identities = std::collections::HashSet::new();
        for binding in &self.trigger_bindings {
            let identity = [
                string_member(binding, "connectorId"),
                string_member(binding, "connectionName"),
                string_member(binding, "triggerName"),
                string_member(binding, "bindingName"),
            ];
            if !identities.contains(&(identity[0].as_str(), identity[1].as_str()))
                || identity[2].is_empty()
                || identity[3].is_empty()
                || !binding.get("configuration").is_some_and(Value::is_object)
                || !trigger_identities.insert(identity.clone())
            {
                return Err(ProjectConfigError::invalid(
                    "configuration trigger is invalid or duplicated",
                ));
            }
        }
        let mut operation_identities = std::collections::HashSet::new();
        for operation in &self.operation_configurations {
            let identity = [
                string_member(operation, "connectorId"),
                string_member(operation, "connectionName"),
                string_member(operation, "operationId"),
                string_member(operation, "flowType"),
                string_member(operation, "stepType"),
            ];
            if !identities.contains(&(identity[0].as_str(), identity[1].as_str()))
                || identity[2..].iter().any(String::is_empty)
                || !operation.get("configuration").is_some_and(Value::is_object)
                || !operation_identities.insert(identity.clone())
            {
                return Err(ProjectConfigError::invalid(
                    "configuration operation is invalid or duplicated",
                ));
            }
        }
        let has_shared = self
            .connections
            .iter()
            .any(|entry| !entry.shared_connection_id.is_empty());
        if has_shared && self.credential_broker.is_none() {
            return Err(ProjectConfigError::invalid(
                "shared connection requires a credential broker",
            ));
        }
        if self.schema_version == PREVIOUS_CONFIGURATION_SCHEMA_VERSION
            && self.credential_broker.is_some()
        {
            return Err(ProjectConfigError::invalid(
                "previous configuration schema cannot declare a broker",
            ));
        }
        if self.connections.iter().any(|entry| {
            !entry.shared_connection_id.is_empty()
                && !is_shared_connection_id(&entry.shared_connection_id)
        }) {
            return Err(ProjectConfigError::invalid(
                "configuration shared connection ID is invalid",
            ));
        }
        if self.environment.len() > 128 {
            return Err(ProjectConfigError::invalid(
                "application environment exceeds field limit",
            ));
        }
        for (name, entry) in &self.environment {
            if !is_application_environment_name(name) {
                return Err(ProjectConfigError::invalid("invalid application environment name"));
            }
            match entry {
                EnvironmentValue::Value(value) => validate_environment_value(value)?,
                EnvironmentValue::SecretRef(reference) => {
                    validate_secret_reference(reference, &self.scope, name)?;
                }
            }
        }
        if let Some(broker) = &self.credential_broker {
            validate_broker_endpoint(&broker.endpoint)?;
            validate_secret_reference(
                &broker.token_secret_ref,
                &self.scope,
                CREDENTIAL_BROKER_TOKEN_SECRET_NAME,
            )?;
        }
        Ok(())
    }

    /// Encodes the shared project configuration schema after validation.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid document.
    pub fn to_wire(&self) -> ProjectConfigResult<Value> {
        self.validate()?;
        let mut object = JsonObject::new();
        object.insert("schemaVersion".into(), Value::String(self.schema_version.clone()));
        object.insert("scope".into(), self.scope.to_wire());
        object.insert("revision".into(), Value::from(self.revision));
        object.insert(
            "connections".into(),
            Value::Array(self.connections.iter().map(ConnectionConfiguration::to_wire).collect()),
        );
        object.insert(
            "triggerBindings".into(),
            Value::Array(self.trigger_bindings.iter().cloned().map(Value::Object).collect()),
        );
        object.insert(
            "operationConfigurations".into(),
            Value::Array(
                self.operation_configurations
                    .iter()
                    .cloned()
                    .map(Value::Object)
                    .collect(),
            ),
        );
        if !self.environment.is_empty() {
            let environment = self
                .environment
                .iter()
                .map(|(name, value)| (name.clone(), value.to_wire()))
                .collect();
            object.insert("environment".into(), Value::Object(environment));
        }
        if let Some(broker) = &self.credential_broker {
            object.insert("credentialBroker".into(), broker.to_wire());
        }
        Ok(Value::Object(object))
    }

    /// Decodes and validates the shared document, including previous-schema documents.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for a malformed or invalid document.
    pub fn from_wire(value: &Value) -> ProjectConfigResult<Self> {
        let record = require_object(Some(value), "configuration")?;
        require_fields(
            record,
            &[
                "schemaVersion",
                "scope",
                "revision",
                "connections",
                "triggerBindings",
                "operationConfigurations",
                "environment",
                "credentialBroker",
            ],
            "configuration",
        )?;
        let arrays_required = || ProjectConfigError::invalid("configuration arrays are required");
        let connections = record
            .get("connections")
            .and_then(Value::as_array)
            .ok_or_else(arrays_required)?;
        let triggers = record
            .get("triggerBindings")
            .and_then(Value::as_array)
            .ok_or_else(arrays_required)?;
        let operations = record
            .get("operationConfigurations")
            .and_then(Value::as_array)
            .ok_or_else(arrays_required)?;
        let empty = Value::Object(JsonObject::new());
        let environment = require_object(Some(record.get("environment").unwrap_or(&empty)), "environment")?;
        let revision = match record.get("revision") {
            None => 0,
            Some(revision) => revision
                .as_u64()
                .ok_or_else(|| ProjectConfigError::invalid("configuration revision is required"))?,
        };
        let configuration = Self {
            scope: Scope::from_wire(record.get("scope").unwrap_or(&Value::Null))?,
            revision,
            connections: connections
                .iter()
                .map(ConnectionConfiguration::from_wire)
                .collect::<ProjectConfigResult<_>>()?,
            trigger_bindings: triggers
                .iter()
                .map(|entry| require_object(Some(entry), "trigger").cloned())
                .collect::<ProjectConfigResult<_>>()?,
            operation_configurations: operations
                .iter()
                .map(|entry| require_object(Some(entry), "operation").cloned())
                .collect::<ProjectConfigResult<_>>()?,
            environment: environment
                .iter()
                .map(|(name, value)| Ok((name.clone(), EnvironmentValue::from_wire(value)?)))
                .collect::<ProjectConfigResult<_>>()?,
            credential_broker: match record.get("credentialBroker") {
                None | Some(Value::Null) => None,
                Some(broker) => Some(CredentialBrokerConfiguration::from_wire(broker)?),
            },
            schema_version: string_member(record, "schemaVersion"),
        };
        configuration.validate()?;
        Ok(configuration)
    }
}
