// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Typed project credential providers with durable refresh admission.

use std::sync::Arc;
use std::time::{Duration, SystemTime};

use serde_json::Value;

use super::broker::SharedConnection;
use super::connections::{
    CONNECTION_SCHEMA_VERSION, ConnectionKey, ConnectionRecord, ConnectionStore,
    CredentialMaterial, CredentialMutation, CredentialReference, connection_record_wire,
    credential_envelope_wire, parse_optional_timestamp,
};
use super::errors::{ProjectConfigError, ProjectConfigResult};
use super::models::{JsonObject, digest_bytes, go_canonical_json_bytes, require_object};
use super::storage::ProjectObject;
use crate::credentials::{
    CredentialError, CredentialProvider, CredentialRefreshDriver, CredentialRefreshState,
    ReauthorizationRequiredError, is_reauthorization_required,
};
use crate::timestamps::Rfc3339NanoTimestamp;
use crate::types::Call;

const ADMISSION_TIMEOUT: Duration = Duration::from_secs(40);
const RECOVERY_POLL_INTERVAL: Duration = Duration::from_millis(100);

/// Decodes private credential members at the connector boundary.
pub type CredentialDecoder<Credential> =
    Arc<dyn Fn(&JsonObject) -> Result<Credential, String> + Send + Sync>;
/// Encodes complete replacement credential members for persistence.
pub type CredentialEncoder<Credential> =
    Arc<dyn Fn(&Credential) -> Result<JsonObject, String> + Send + Sync>;

fn now() -> Rfc3339NanoTimestamp {
    Rfc3339NanoTimestamp::now_utc()
}

fn credential_error(error: ProjectConfigError) -> CredentialError {
    Box::new(error)
}

/// Resolves a non-refreshing project credential for one connector connection.
///
/// Ordinary entries read current storage on every call. Shared entries use the loaded
/// project's broker and the optional shared decoder. Credentials never enter Flow state.
pub struct ProjectCredentialProvider<Credential> {
    store: ConnectionStore,
    key: ConnectionKey,
    decode: CredentialDecoder<Credential>,
    decode_shared: Option<CredentialDecoder<Credential>>,
    shared: Option<Arc<SharedConnection>>,
}

impl<Credential> ProjectCredentialProvider<Credential> {
    /// Binds `key` in `store`; `decode_shared` decodes broker material when it differs.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid key.
    pub fn new(
        store: ConnectionStore,
        key: ConnectionKey,
        decode: CredentialDecoder<Credential>,
        decode_shared: Option<CredentialDecoder<Credential>>,
    ) -> ProjectConfigResult<Self> {
        key.path(store.scope())?;
        let shared = store.shared_connection(&key);
        Ok(Self {
            store,
            key,
            decode,
            decode_shared,
            shared,
        })
    }

    fn validate_call(&self, call: &Call<'_>) -> Result<(), CredentialError> {
        if call.operation.connector_id != self.key.connector_id
            || call.connection.name != self.key.connection_name
        {
            return Err(credential_error(ProjectConfigError::invalid(
                "connector call differs from configured project connection",
            )));
        }
        call.operation
            .validate()
            .map_err(|error| Box::new(error) as CredentialError)
    }

    fn decode_material(
        &self,
        material: &CredentialMaterial,
    ) -> Result<Credential, CredentialError> {
        (self.decode)(&material.credentials).map_err(|_| {
            credential_error(ProjectConfigError::invalid(
                "project credential decoding failed",
            ))
        })
    }

    fn resolve_shared(
        &self,
        shared: &SharedConnection,
        call: &Call<'_>,
        is_after_rejection: bool,
    ) -> Result<Credential, CredentialError> {
        let material = if is_after_rejection {
            shared.resolve_after_rejection(call.id.as_str())
        } else {
            shared.resolve(call.id.as_str())
        }
        .map_err(super::broker::BrokerReadError::into_credential_error)?;
        let decoder = self.decode_shared.as_ref().unwrap_or(&self.decode);
        decoder(&material).map_err(|_| {
            credential_error(ProjectConfigError::invalid(
                "shared project credential decoding failed",
            ))
        })
    }
}

impl<Credential: Send + Sync> CredentialProvider<Credential>
    for ProjectCredentialProvider<Credential>
{
    fn resolve(&self, call: &Call<'_>) -> Result<Credential, CredentialError> {
        self.validate_call(call)?;
        if let Some(shared) = &self.shared {
            return self.resolve_shared(shared, call, false);
        }
        let (material, connection) = self
            .store
            .read_credential_material(&self.key)
            .map_err(credential_error)?;
        if connection
            .expires_at
            .is_some_and(|expires_at| expires_at.unix_nanoseconds() <= now().unix_nanoseconds())
        {
            return Err(credential_error(ProjectConfigError::invalid(
                "expired project credential requires refresh",
            )));
        }
        self.decode_material(&material)
    }
}

#[derive(Clone, Debug)]
struct Admission {
    kind: String,
    attempt_id: String,
    fence: u64,
    deadline: Rfc3339NanoTimestamp,
    result_key: String,
}

/// Refreshes project credentials once per generation across replicas.
///
/// For ordinary entries a conditional head write grants exactly one caller provider
/// dispatch; observers recover the immutable result or restore an abandoned admission.
/// Shared entries use the broker without consulting the refresh driver. A driver's refresh
/// runs synchronously and must bound its own provider I/O.
pub struct RefreshingProjectCredentialProvider<Credential> {
    base: ProjectCredentialProvider<Credential>,
    encode: CredentialEncoder<Credential>,
}

impl<Credential> RefreshingProjectCredentialProvider<Credential> {
    /// Binds `key` in `store` with a decoder and an encoder of complete replacements.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid key.
    pub fn new(
        store: ConnectionStore,
        key: ConnectionKey,
        decode: CredentialDecoder<Credential>,
        encode: CredentialEncoder<Credential>,
        decode_shared: Option<CredentialDecoder<Credential>>,
    ) -> ProjectConfigResult<Self> {
        Ok(Self {
            base: ProjectCredentialProvider::new(store, key, decode, decode_shared)?,
            encode,
        })
    }

    fn store(&self) -> &ConnectionStore {
        &self.base.store
    }

    fn key(&self) -> &ConnectionKey {
        &self.base.key
    }

    fn admit_refresh(
        &self,
        record: &ConnectionRecord,
        decided_at: &Rfc3339NanoTimestamp,
    ) -> ProjectConfigResult<Admission> {
        let attempt_id = uuid::Uuid::new_v4().simple().to_string();
        let path = self.key().path(self.store().scope())?;
        let deadline = timestamp_after(decided_at, ADMISSION_TIMEOUT);
        let mutation = CredentialMutation {
            kind: "REFRESHING".into(),
            invocation_id: uuid::Uuid::new_v4().simple().to_string(),
            id: attempt_id.clone(),
            fence: record.fence + 1,
            base_revision: record.revision,
            deadline: deadline.clone(),
            result_key: format!("{path}/exchange-results/{attempt_id}"),
        };
        let updated = connection_record_wire(
            self.store().scope(),
            self.key(),
            record.revision + 1,
            record.fence + 1,
            "REFRESHING",
            record.expires_at.as_ref(),
            &record.auth_method,
            record.credential.as_ref(),
            Some(&mutation),
        );
        self.write_head(record, &updated)?;
        Ok(Admission {
            kind: "REFRESHING".into(),
            attempt_id,
            fence: record.fence + 1,
            deadline,
            result_key: mutation.result_key,
        })
    }

    fn refresh_admitted(
        &self,
        admission: &Admission,
        prior: &CredentialMaterial,
        state: &CredentialRefreshState<'_, Credential>,
        driver: &dyn CredentialRefreshDriver<Credential>,
    ) -> Result<Credential, CredentialError> {
        let result = match driver.refresh(state) {
            Ok(result) => result,
            Err(error) if is_reauthorization_required(&*error) => {
                self.finish_exchange(admission, "REAUTHORIZATION_REQUIRED")
                    .map_err(credential_error)?;
                return Err(error);
            }
            Err(_) => {
                self.finish_exchange(admission, "READY")
                    .map_err(credential_error)?;
                return Err(credential_error(ProjectConfigError::RefreshFailed(
                    "project credential refresh failed; current authorization is kept".into(),
                )));
            }
        };
        if result.expires_at.unix_nanoseconds() <= now().unix_nanoseconds() {
            self.finish_exchange(admission, "READY")
                .map_err(credential_error)?;
            return Err(credential_error(ProjectConfigError::RefreshFailed(
                "refreshed credential has no future expiry".into(),
            )));
        }
        let encoded = match (self.encode)(&result.credentials) {
            Ok(encoded) if !encoded.is_empty() => encoded,
            _ => {
                self.finish_exchange(admission, "READY")
                    .map_err(credential_error)?;
                return Err(credential_error(ProjectConfigError::RefreshFailed(
                    "refreshed credential encoding failed".into(),
                )));
            }
        };
        let material = CredentialMaterial {
            credentials: encoded,
            expires_at: Some(result.expires_at),
            auth_method: prior.auth_method.clone(),
        };
        self.commit_refresh(admission, &material)
            .map_err(credential_error)?;
        Ok(result.credentials)
    }

    fn recover_or_wait(&self, record: &ConnectionRecord) -> ProjectConfigResult<()> {
        let mutation = record.mutation.as_ref().ok_or_else(|| {
            ProjectConfigError::invalid("credential exchange admission is absent")
        })?;
        let admission = Admission {
            kind: record.status.clone(),
            attempt_id: mutation.id.clone(),
            fence: record.fence,
            deadline: mutation.deadline.clone(),
            result_key: mutation.result_key.clone(),
        };
        match self
            .store()
            .objects()
            .read_object(&admission.result_key, "")
        {
            Ok(result) => self.publish_result(&admission, &result),
            Err(ProjectConfigError::ObjectNotFound) => {
                if now().unix_nanoseconds() < admission.deadline.unix_nanoseconds() {
                    // Another replica owns dispatch; poll for its published result.
                    std::thread::sleep(RECOVERY_POLL_INTERVAL);
                    return Ok(());
                }
                let status = if admission.kind == "AUTHORIZING" {
                    "REAUTHORIZATION_REQUIRED"
                } else {
                    "READY"
                };
                self.finish_exchange(&admission, status)
            }
            Err(error) => Err(error),
        }
    }

    fn commit_refresh(
        &self,
        admission: &Admission,
        material: &CredentialMaterial,
    ) -> ProjectConfigResult<()> {
        let envelope = credential_envelope_wire(
            self.store().scope(),
            self.key(),
            admission.fence,
            &material.credentials,
            material.expires_at.as_ref(),
            &material.auth_method,
            &admission.attempt_id,
        );
        let contents = go_canonical_json_bytes(&envelope)?;
        let result = self.store().create_or_match(
            &admission.result_key,
            &contents,
            "immutable credential result differs",
        )?;
        self.publish_result(admission, &result)
    }

    fn publish_result(
        &self,
        admission: &Admission,
        result: &ProjectObject,
    ) -> ProjectConfigResult<()> {
        let (record, _) = self.store().read_record(self.key())?;
        let is_published = |record: &ConnectionRecord| {
            record.status == "READY"
                && record
                    .credential
                    .as_ref()
                    .is_some_and(|reference| reference.key == result.key)
        };
        if is_published(&record) {
            return Ok(());
        }
        require_admission(&record, admission)?;
        let envelope: Value = serde_json::from_slice(&result.contents)
            .map_err(|_| ProjectConfigError::invalid("credential result must be a JSON object"))?;
        let envelope = require_object(Some(&envelope), "credential result")?;
        if envelope.get("schemaVersion") != Some(&Value::String(CONNECTION_SCHEMA_VERSION.into()))
            || envelope.get("scope") != Some(&self.store().scope().to_wire())
            || envelope.get("connection") != Some(&self.key().to_wire())
            || envelope.get("fence").and_then(Value::as_u64) != Some(admission.fence)
            || envelope.get("attemptId").and_then(Value::as_str)
                != Some(admission.attempt_id.as_str())
        {
            return Err(ProjectConfigError::invalid(
                "credential exchange result identity is invalid",
            ));
        }
        let reference = CredentialReference {
            key: result.key.clone(),
            version: result.version.clone(),
            digest: digest_bytes(&result.contents)[7..].to_string(),
        };
        let updated = connection_record_wire(
            self.store().scope(),
            self.key(),
            record.revision + 1,
            record.fence,
            "READY",
            parse_optional_timestamp(envelope.get("expiresAt"))?.as_ref(),
            envelope
                .get("authMethod")
                .and_then(Value::as_str)
                .unwrap_or_default(),
            Some(&reference),
            None,
        );
        match self.write_head(&record, &updated) {
            Err(ProjectConfigError::ObjectConflict(message)) => {
                let (observed, _) = self.store().read_record(self.key())?;
                if is_published(&observed) {
                    Ok(())
                } else {
                    Err(ProjectConfigError::ObjectConflict(message))
                }
            }
            other => other,
        }
    }

    fn finish_exchange(&self, admission: &Admission, status: &str) -> ProjectConfigResult<()> {
        let (record, _) = self.store().read_record(self.key())?;
        require_admission(&record, admission)?;
        let updated = connection_record_wire(
            self.store().scope(),
            self.key(),
            record.revision + 1,
            record.fence + 1,
            status,
            record.expires_at.as_ref(),
            &record.auth_method,
            record.credential.as_ref(),
            None,
        );
        self.write_head(&record, &updated)
    }

    fn write_head(&self, prior: &ConnectionRecord, updated: &Value) -> ProjectConfigResult<()> {
        let head_key = format!("{}/head", self.key().path(self.store().scope())?);
        let (current, head) = self.store().read_record(self.key())?;
        if current.document != prior.document {
            return Err(ProjectConfigError::ObjectConflict(
                "connection revision conflict".into(),
            ));
        }
        let contents = go_canonical_json_bytes(updated)?;
        match self
            .store()
            .objects()
            .compare_and_swap_object(&head_key, &head.etag, &contents)
        {
            Ok(_) => Ok(()),
            Err(ProjectConfigError::OutcomeUnknown(message)) => {
                let observed = self.store().objects().read_object(&head_key, "")?;
                if observed.contents != contents {
                    return Err(ProjectConfigError::OutcomeUnknown(message));
                }
                Ok(())
            }
            Err(error) => Err(error),
        }
    }
}

impl<Credential: Send + Sync> CredentialProvider<Credential>
    for RefreshingProjectCredentialProvider<Credential>
{
    fn resolve(&self, call: &Call<'_>) -> Result<Credential, CredentialError> {
        self.base.resolve(call)
    }

    fn resolve_with_refresh(
        &self,
        call: &Call<'_>,
        driver: &dyn CredentialRefreshDriver<Credential>,
    ) -> Result<Credential, CredentialError> {
        self.base.validate_call(call)?;
        if let Some(shared) = &self.base.shared {
            return self.base.resolve_shared(shared, call, false);
        }
        loop {
            let (record, _) = self
                .store()
                .read_record(self.key())
                .map_err(credential_error)?;
            match record.status.as_str() {
                "REVOKED" | "REAUTHORIZATION_REQUIRED" => {
                    return Err(Box::new(ReauthorizationRequiredError::new(None)));
                }
                "AUTHORIZING" | "REFRESHING" => {
                    let is_pending_authorization = record.status == "AUTHORIZING"
                        && record.mutation.as_ref().is_some_and(|mutation| {
                            now().unix_nanoseconds() < mutation.deadline.unix_nanoseconds()
                        });
                    if is_pending_authorization {
                        return Err(credential_error(ProjectConfigError::ExchangePending));
                    }
                    match self.recover_or_wait(&record) {
                        Ok(()) | Err(ProjectConfigError::ObjectConflict(_)) => continue,
                        Err(error) => return Err(credential_error(error)),
                    }
                }
                "READY" => {}
                _ => {
                    return Err(credential_error(ProjectConfigError::invalid(
                        "project credential status is invalid",
                    )));
                }
            }
            let material = self
                .store()
                .read_material(self.key(), &record)
                .map_err(credential_error)?;
            let credentials = self.base.decode_material(&material)?;
            let decided_at = now();
            let state = CredentialRefreshState {
                credentials: &credentials,
                expires_at: material.expires_at.clone(),
                now: decided_at.clone(),
            };
            let is_unexpired = material.expires_at.as_ref().is_none_or(|expires_at| {
                expires_at.unix_nanoseconds() > decided_at.unix_nanoseconds()
            });
            if is_unexpired && !driver.is_refresh_required(&state) {
                return Ok(credentials);
            }
            let admission = match self.admit_refresh(&record, &decided_at) {
                Ok(admission) => admission,
                Err(ProjectConfigError::ObjectConflict(_)) => continue,
                Err(error) => return Err(credential_error(error)),
            };
            return self.refresh_admitted(&admission, &material, &state, driver);
        }
    }

    fn supports_refresh_after_rejection(&self) -> bool {
        true
    }

    fn resolve_after_rejection(
        &self,
        call: &Call<'_>,
        driver: &dyn CredentialRefreshDriver<Credential>,
    ) -> Result<Credential, CredentialError> {
        self.base.validate_call(call)?;
        if let Some(shared) = &self.base.shared {
            return self.base.resolve_shared(shared, call, true);
        }
        let connection = self
            .store()
            .read_connection(self.key())
            .map_err(credential_error)?;
        let is_expired = connection
            .expires_at
            .is_some_and(|expires_at| expires_at.unix_nanoseconds() <= now().unix_nanoseconds());
        if !is_expired {
            return Err(credential_error(
                ProjectConfigError::RejectedCredentialNotRefreshed(
                    "authentication rejection does not prove credential expiry".into(),
                ),
            ));
        }
        self.resolve_with_refresh(call, driver)
    }
}

fn require_admission(record: &ConnectionRecord, admission: &Admission) -> ProjectConfigResult<()> {
    let is_current = record.status == admission.kind
        && record.fence == admission.fence
        && record.mutation.as_ref().is_some_and(|mutation| {
            mutation.id == admission.attempt_id && mutation.kind == admission.kind
        });
    if !is_current {
        return Err(ProjectConfigError::ObjectConflict(
            "credential exchange admission was superseded".into(),
        ));
    }
    Ok(())
}

fn timestamp_after(start: &Rfc3339NanoTimestamp, duration: Duration) -> Rfc3339NanoTimestamp {
    let start_nanoseconds = start.unix_nanoseconds();
    let magnitude =
        Duration::from_nanos(u64::try_from(start_nanoseconds.max(0)).unwrap_or(u64::MAX));
    Rfc3339NanoTimestamp::from_system_time_utc(SystemTime::UNIX_EPOCH + magnitude + duration)
}
