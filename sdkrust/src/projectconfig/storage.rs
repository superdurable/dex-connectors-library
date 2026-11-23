// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Versioned private project objects, snapshot CAS, and the S3 boundary.

use std::collections::HashMap;
use std::fmt;
use std::sync::{Arc, Mutex, PoisonError};

use serde_json::Value;

use super::dexweb_store::DexWebObjectStore;
use super::errors::{ProjectConfigError, ProjectConfigResult};
use super::models::{
    ApplicationSecretRef, CREDENTIAL_BROKER_TOKEN_SECRET_NAME, Configuration, JsonObject, Scope,
    SnapshotRef, digest_bytes, go_canonical_json_bytes, is_application_environment_name,
    sha256_hex, validate_environment_value, validate_secret_reference,
};

/// Largest object accepted by S3 and in-memory project storage.
pub const MAXIMUM_OBJECT_BYTES: usize = 4 << 20;
const MAXIMUM_LOCAL_TRIGGER_STATE_BYTES: usize = 8 << 20;

/// Versioned private bytes; `Debug` shows only the identity.
#[derive(Clone, Eq, PartialEq)]
pub struct ProjectObject {
    /// Object key.
    pub key: String,
    /// Exact storage version.
    pub version: String,
    /// Opaque ETag; pass it unchanged as the CAS precondition.
    pub etag: String,
    /// Private contents; never log or persist them in Flow state.
    pub contents: Vec<u8>,
}

impl fmt::Debug for ProjectObject {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "ProjectObject(key={:?}, version={:?})",
            self.key, self.version
        )
    }
}

/// Exact-version reads and single-attempt conditional writes.
///
/// Implementations return [`ProjectConfigError::ObjectNotFound`] for an absent key or version,
/// [`ProjectConfigError::ObjectConflict`] for a failed precondition, and
/// [`ProjectConfigError::OutcomeUnknown`] when a write may have reached storage.
pub trait ObjectStore: Send + Sync {
    /// Reads the exact `version`, or the current head when `version` is empty.
    ///
    /// # Errors
    ///
    /// Returns a storage error as documented on the trait.
    fn read_object(&self, key: &str, version: &str) -> ProjectConfigResult<ProjectObject>;

    /// Creates the object only if the key is absent.
    ///
    /// # Errors
    ///
    /// Returns a storage error as documented on the trait.
    fn create_object(&self, key: &str, contents: &[u8]) -> ProjectConfigResult<ProjectObject>;

    /// Replaces the object only when its current ETag equals `expected_etag`.
    ///
    /// # Errors
    ///
    /// Returns a storage error as documented on the trait.
    fn compare_and_swap_object(
        &self,
        key: &str,
        expected_etag: &str,
        contents: &[u8],
    ) -> ProjectConfigResult<ProjectObject>;

    /// Returns this store as a Dex Web connections file store, if it is one.
    ///
    /// Credential providers then hold the file's cross-process refresh lock and verify that
    /// the file still lists a connection before using it. Other stores keep the default
    /// `None`.
    fn dex_web_connections_file(&self) -> Option<&DexWebObjectStore> {
        None
    }
}

/// Rejects traversal, separators, controls, and oversized object keys.
///
/// # Errors
///
/// Returns [`ProjectConfigError::Invalid`] for an invalid key.
pub fn validate_object_key(key: &str) -> ProjectConfigResult<()> {
    if key.is_empty()
        || key.len() > 1024
        || key.starts_with('/')
        || key.contains(['\\', '\0', '\r', '\n'])
        || key
            .split('/')
            .any(|segment| matches!(segment, "" | "." | ".."))
    {
        return Err(ProjectConfigError::invalid("invalid project object key"));
    }
    Ok(())
}

/// Allows Go's 8 MiB Trigger inbox and poll cursor outside S3; other objects keep 4 MiB.
pub(crate) fn local_object_byte_limit(key: &str) -> usize {
    let is_trigger_state = ["/trigger-inboxes/", "/poll-cursors/", "/poll-feeds/"]
        .iter()
        .any(|part| key.contains(part));
    if is_trigger_state {
        MAXIMUM_LOCAL_TRIGGER_STATE_BYTES
    } else {
        MAXIMUM_OBJECT_BYTES
    }
}

/// Rejects an invalid key or an empty or oversized body, as every local store's write does.
pub(crate) fn validate_local_object_write(key: &str, contents: &[u8]) -> ProjectConfigResult<()> {
    validate_object_key(key)?;
    if contents.is_empty() || contents.len() > local_object_byte_limit(key) {
        return Err(ProjectConfigError::invalid(
            "project object body is empty or oversized",
        ));
    }
    Ok(())
}

pub(crate) fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    // Guarded maps stay consistent even if a panicking thread held the lock.
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

/// Deterministic versioned store for local Dex Web projects and tests.
///
/// Versions count from 1 and each ETag is the lowercase hex SHA-256 of the contents
/// followed by `-<version>`, as sdkgo's local store writes it.
#[derive(Default)]
pub struct InMemoryObjectStore {
    objects: Mutex<HashMap<String, Vec<ProjectObject>>>,
}

impl InMemoryObjectStore {
    /// Creates an empty store.
    pub fn new() -> Self {
        Self::default()
    }

    fn append(
        objects: &mut HashMap<String, Vec<ProjectObject>>,
        key: &str,
        contents: &[u8],
    ) -> ProjectObject {
        let versions = objects.entry(key.to_string()).or_default();
        let version = (versions.len() + 1).to_string();
        let item = ProjectObject {
            key: key.to_string(),
            etag: format!("{}-{version}", sha256_hex(contents)),
            version,
            contents: contents.to_vec(),
        };
        versions.push(item.clone());
        item
    }
}

impl ObjectStore for InMemoryObjectStore {
    fn read_object(&self, key: &str, version: &str) -> ProjectConfigResult<ProjectObject> {
        validate_object_key(key)?;
        let objects = lock(&self.objects);
        objects
            .get(key)
            .and_then(|versions| {
                versions
                    .iter()
                    .rev()
                    .find(|item| version.is_empty() || item.version == version)
            })
            .cloned()
            .ok_or(ProjectConfigError::ObjectNotFound)
    }

    fn create_object(&self, key: &str, contents: &[u8]) -> ProjectConfigResult<ProjectObject> {
        validate_local_object_write(key, contents)?;
        let mut objects = lock(&self.objects);
        if objects.contains_key(key) {
            return Err(ProjectConfigError::ObjectConflict(
                "project object already exists".into(),
            ));
        }
        Ok(Self::append(&mut objects, key, contents))
    }

    fn compare_and_swap_object(
        &self,
        key: &str,
        expected_etag: &str,
        contents: &[u8],
    ) -> ProjectConfigResult<ProjectObject> {
        validate_local_object_write(key, contents)?;
        let mut objects = lock(&self.objects);
        let is_current = objects
            .get(key)
            .and_then(|versions| versions.last())
            .is_some_and(|current| !expected_etag.is_empty() && current.etag == expected_etag);
        if !is_current {
            return Err(ProjectConfigError::ObjectConflict(
                "project object compare-and-swap conflict".into(),
            ));
        }
        Ok(Self::append(&mut objects, key, contents))
    }
}

/// Response of one S3 `GetObject`.
#[derive(Clone, Default)]
pub struct S3GetObjectOutput {
    /// `VersionId` header.
    pub version_id: String,
    /// `ETag` header, quotes included.
    pub etag: String,
    /// `x-amz-server-side-encryption` header.
    pub server_side_encryption: String,
    /// `x-amz-server-side-encryption-aws-kms-key-id` header.
    pub sse_kms_key_id: String,
    /// Body bytes; the client reads at most `maximum_bytes + 1`.
    pub body: Vec<u8>,
}

/// One S3 `PutObject` request with exactly one precondition.
///
/// `Debug` omits the body, which may hold credentials or application secrets.
#[derive(Clone, Default, Eq, PartialEq)]
pub struct S3PutObjectInput {
    /// Bucket name.
    pub bucket: String,
    /// Full object key, prefix included.
    pub key: String,
    /// Body bytes.
    pub body: Vec<u8>,
    /// Always `application/json`.
    pub content_type: String,
    /// `If-Match` ETag for a compare-and-swap; empty for a create.
    pub if_match: String,
    /// `*` for a create; empty for a compare-and-swap.
    pub if_none_match: String,
    /// `aws:kms` when a KMS key is configured.
    pub server_side_encryption: String,
    /// KMS key ARN when configured.
    pub sse_kms_key_id: String,
}

impl fmt::Debug for S3PutObjectInput {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("S3PutObjectInput")
            .field("bucket", &self.bucket)
            .field("key", &self.key)
            .field("body_len", &self.body.len())
            .field("if_match", &self.if_match)
            .field("if_none_match", &self.if_none_match)
            .finish_non_exhaustive()
    }
}

/// Response of one S3 `PutObject`.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct S3PutObjectOutput {
    /// `VersionId` header.
    pub version_id: String,
    /// `ETag` header.
    pub etag: String,
}

/// S3 request failure classified by HTTP status.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct S3Error {
    /// HTTP status when the service answered; `None` for a transport failure.
    pub http_status: Option<u16>,
}

/// Blocking S3 operations the project store needs.
///
/// Implement it over the application's AWS client, configured without automatic retries
/// so each conditional write is a single attempt. The crate ships no AWS credential chain.
pub trait S3Client: Send + Sync {
    /// Returns the bucket versioning status, such as `Enabled`.
    ///
    /// # Errors
    ///
    /// Returns [`S3Error`] for a failed request.
    fn get_bucket_versioning(&self, bucket: &str) -> Result<String, S3Error>;

    /// Reads an object, optionally at an exact version, bounded to `maximum_bytes + 1` bytes.
    ///
    /// # Errors
    ///
    /// Returns [`S3Error`] for a failed request.
    fn get_object(
        &self,
        bucket: &str,
        key: &str,
        version_id: &str,
        maximum_bytes: usize,
    ) -> Result<S3GetObjectOutput, S3Error>;

    /// Writes an object once with the request's precondition.
    ///
    /// # Errors
    ///
    /// Returns [`S3Error`] for a failed request.
    fn put_object(&self, input: S3PutObjectInput) -> Result<S3PutObjectOutput, S3Error>;
}

fn is_kms_key_arn(value: &str) -> bool {
    let parts: Vec<&str> = value.splitn(6, ':').collect();
    let is_segment = |segment: &str| {
        !segment.is_empty()
            && segment
                .bytes()
                .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-')
    };
    parts.len() == 6
        && parts[0] == "arn"
        && is_segment(parts[1])
        && parts[2] == "kms"
        && is_segment(parts[3])
        && parts[4].len() == 12
        && parts[4].bytes().all(|byte| byte.is_ascii_digit())
        && parts[5].strip_prefix("key/").is_some_and(|key_id| {
            !key_id.is_empty()
                && key_id
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
        })
}

/// Versioned S3 object store with exact KMS and conditional-write boundaries.
pub struct S3ObjectStore {
    client: Arc<dyn S3Client>,
    bucket: String,
    prefix: String,
    kms_key_arn: String,
}

impl S3ObjectStore {
    /// Validates the configuration and that bucket versioning is enabled.
    ///
    /// An empty `kms_key_arn` is accepted only with `allow_unencrypted`.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid bucket, prefix, key, or disabled
    /// versioning, and [`ProjectConfigError::Io`] when versioning cannot be read.
    pub fn create(
        client: Arc<dyn S3Client>,
        bucket: &str,
        prefix: &str,
        kms_key_arn: &str,
        allow_unencrypted: bool,
    ) -> ProjectConfigResult<Self> {
        if bucket.is_empty()
            || bucket.contains(['/', '\\', '\0'])
            || !(is_kms_key_arn(kms_key_arn) || (allow_unencrypted && kms_key_arn.is_empty()))
        {
            return Err(ProjectConfigError::invalid(
                "versioned project storage and encryption configuration are required",
            ));
        }
        let trimmed_prefix = prefix.trim_end_matches('/');
        if !prefix.is_empty() {
            validate_object_key(trimmed_prefix)?;
        }
        let status = client
            .get_bucket_versioning(bucket)
            .map_err(|_| ProjectConfigError::Io("project bucket versioning read failed".into()))?;
        if status != "Enabled" {
            return Err(ProjectConfigError::invalid(
                "project object bucket versioning must be enabled",
            ));
        }
        Ok(Self {
            client,
            bucket: bucket.to_string(),
            prefix: if prefix.is_empty() {
                String::new()
            } else {
                format!("{trimmed_prefix}/")
            },
            kms_key_arn: kms_key_arn.to_string(),
        })
    }

    fn write_object(
        &self,
        key: &str,
        contents: &[u8],
        expected_etag: &str,
    ) -> ProjectConfigResult<ProjectObject> {
        validate_object_key(key)?;
        if contents.is_empty() || contents.len() > MAXIMUM_OBJECT_BYTES {
            return Err(ProjectConfigError::invalid(
                "project object body is empty or oversized",
            ));
        }
        let mut input = S3PutObjectInput {
            bucket: self.bucket.clone(),
            key: format!("{}{key}", self.prefix),
            body: contents.to_vec(),
            content_type: "application/json".into(),
            ..S3PutObjectInput::default()
        };
        if expected_etag.is_empty() {
            input.if_none_match = "*".into();
        } else {
            input.if_match = expected_etag.into();
        }
        if !self.kms_key_arn.is_empty() {
            input.server_side_encryption = "aws:kms".into();
            input.sse_kms_key_id = self.kms_key_arn.clone();
        }
        let output = self
            .client
            .put_object(input)
            .map_err(|error| match error.http_status {
                Some(409 | 412) => ProjectConfigError::ObjectConflict(
                    "project object conditional write conflict".into(),
                ),
                _ => ProjectConfigError::OutcomeUnknown("project write outcome is unknown".into()),
            })?;
        if output.version_id.is_empty() || output.version_id == "null" || output.etag.is_empty() {
            return Err(ProjectConfigError::OutcomeUnknown(
                "project write has no exact version identity".into(),
            ));
        }
        Ok(ProjectObject {
            key: key.to_string(),
            version: output.version_id,
            etag: output.etag,
            contents: contents.to_vec(),
        })
    }
}

impl ObjectStore for S3ObjectStore {
    fn read_object(&self, key: &str, version: &str) -> ProjectConfigResult<ProjectObject> {
        validate_object_key(key)?;
        let output = self
            .client
            .get_object(
                &self.bucket,
                &format!("{}{key}", self.prefix),
                version,
                MAXIMUM_OBJECT_BYTES,
            )
            .map_err(|error| match error.http_status {
                Some(404) => ProjectConfigError::ObjectNotFound,
                Some(409 | 412) => ProjectConfigError::ObjectConflict(
                    "project object conditional write conflict".into(),
                ),
                _ => ProjectConfigError::Io("project object read failed".into()),
            })?;
        if output.version_id.is_empty()
            || output.version_id == "null"
            || output.etag.is_empty()
            || (!version.is_empty() && output.version_id != version)
        {
            return Err(ProjectConfigError::invalid(
                "project object lacks exact version identity",
            ));
        }
        if !self.kms_key_arn.is_empty()
            && (output.server_side_encryption != "aws:kms"
                || output.sse_kms_key_id != self.kms_key_arn)
        {
            return Err(ProjectConfigError::invalid(
                "project object encryption boundary differs",
            ));
        }
        if output.body.is_empty() || output.body.len() > MAXIMUM_OBJECT_BYTES {
            return Err(ProjectConfigError::invalid(
                "project object body is unavailable or oversized",
            ));
        }
        Ok(ProjectObject {
            key: key.to_string(),
            version: output.version_id,
            etag: output.etag,
            contents: output.body,
        })
    }

    fn create_object(&self, key: &str, contents: &[u8]) -> ProjectConfigResult<ProjectObject> {
        self.write_object(key, contents, "")
    }

    fn compare_and_swap_object(
        &self,
        key: &str,
        expected_etag: &str,
        contents: &[u8],
    ) -> ProjectConfigResult<ProjectObject> {
        if expected_etag.is_empty() {
            return Err(ProjectConfigError::invalid(
                "object compare-and-swap requires an ETag",
            ));
        }
        self.write_object(key, contents, expected_etag)
    }
}

/// Revision CAS and exact-version reads of the ordinary configuration document.
#[derive(Clone)]
pub struct ConfigurationStore {
    objects: Arc<dyn ObjectStore>,
    scope: Scope,
    key: String,
}

impl ConfigurationStore {
    /// Binds the configuration head of `scope`.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid scope.
    pub fn new(objects: Arc<dyn ObjectStore>, scope: Scope) -> ProjectConfigResult<Self> {
        let key = format!("{}/configuration/head", scope.prefix()?);
        Ok(Self {
            objects,
            scope,
            key,
        })
    }

    /// Returns the bound scope.
    pub fn scope(&self) -> &Scope {
        &self.scope
    }

    /// Reads and validates the current document and its object identity.
    ///
    /// # Errors
    ///
    /// Returns a storage or validation error.
    pub fn read_configuration(&self) -> ProjectConfigResult<(Configuration, ProjectObject)> {
        let item = self.objects.read_object(&self.key, "")?;
        Ok((self.decode(&item)?, item))
    }

    /// Conditionally replaces the document when the stored revision equals
    /// `expected_revision`, writing revision `expected_revision + 1`.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::ObjectConflict`] for a revision conflict, or a storage or
    /// validation error.
    pub fn write_configuration(
        &self,
        expected_revision: u64,
        configuration: &Configuration,
    ) -> ProjectConfigResult<(Configuration, ProjectObject)> {
        let previous = match self.read_configuration() {
            Ok(previous) => Some(previous),
            Err(ProjectConfigError::ObjectNotFound) if expected_revision == 0 => None,
            Err(error) => return Err(error),
        };
        let previous_revision = previous
            .as_ref()
            .map_or(0, |(document, _)| document.revision);
        if previous_revision != expected_revision {
            return Err(ProjectConfigError::ObjectConflict(
                "configuration revision conflict".into(),
            ));
        }
        let is_shared = configuration.credential_broker.is_some()
            || configuration
                .connections
                .iter()
                .any(|entry| !entry.shared_connection_id.is_empty());
        let mut updated = configuration.clone();
        updated.scope = self.scope.clone();
        updated.revision = expected_revision + 1;
        updated.schema_version = if is_shared {
            super::models::CONFIGURATION_SCHEMA_VERSION.into()
        } else {
            super::models::PREVIOUS_CONFIGURATION_SCHEMA_VERSION.into()
        };
        let contents = updated.to_go_json_bytes()?;
        let written = match &previous {
            None => self.objects.create_object(&self.key, &contents),
            Some((_, previous_object)) => {
                self.objects
                    .compare_and_swap_object(&self.key, &previous_object.etag, &contents)
            }
        };
        let item = match written {
            Ok(item) => item,
            Err(ProjectConfigError::OutcomeUnknown(message)) => {
                let observed = self.objects.read_object(&self.key, "")?;
                if observed.contents != contents {
                    return Err(ProjectConfigError::OutcomeUnknown(message));
                }
                observed
            }
            Err(error) => return Err(error),
        };
        Ok((updated, item))
    }

    /// Pins the current exact version when its revision equals `expected_revision`.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::ObjectConflict`] for a revision conflict.
    pub fn freeze_configuration(&self, expected_revision: u64) -> ProjectConfigResult<SnapshotRef> {
        let (configuration, item) = self.read_configuration()?;
        if configuration.revision != expected_revision {
            return Err(ProjectConfigError::ObjectConflict(
                "configuration revision conflict".into(),
            ));
        }
        Ok(SnapshotRef::new(
            item.key,
            item.version,
            digest_bytes(&item.contents),
        ))
    }

    /// Reads a pinned snapshot, verifying scope, exact version, and full digest.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for a mismatched reference or digest.
    pub fn read_snapshot(&self, reference: &SnapshotRef) -> ProjectConfigResult<Configuration> {
        reference.validate(&self.scope)?;
        let item = self
            .objects
            .read_object(&reference.key, &reference.version)?;
        if digest_bytes(&item.contents) != reference.digest {
            return Err(ProjectConfigError::invalid(
                "configuration snapshot digest differs",
            ));
        }
        self.decode(&item)
    }

    /// Writes one immutable private value beneath this scope and application name.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for a reserved name or invalid value.
    pub fn create_application_secret(
        &self,
        name: &str,
        value: &str,
    ) -> ProjectConfigResult<ApplicationSecretRef> {
        if !is_application_environment_name(name) {
            return Err(ProjectConfigError::invalid(
                "invalid application secret name",
            ));
        }
        validate_environment_value(value)?;
        self.create_secret(name, value)
    }

    /// Writes one immutable broker scope token outside application environment names.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an empty or invalid token.
    pub fn create_credential_broker_token(
        &self,
        token: &str,
    ) -> ProjectConfigResult<ApplicationSecretRef> {
        if token.is_empty() {
            return Err(ProjectConfigError::invalid(
                "credential broker token is required",
            ));
        }
        validate_environment_value(token)?;
        self.create_secret(CREDENTIAL_BROKER_TOKEN_SECRET_NAME, token)
    }

    /// Reads exact private bytes and verifies digest, scope, and field identity.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for any integrity or identity mismatch.
    pub fn read_secret_value(
        &self,
        reference: &ApplicationSecretRef,
        name: &str,
    ) -> ProjectConfigResult<String> {
        validate_secret_reference(reference, &self.scope, name)?;
        let item = self
            .objects
            .read_object(&reference.key, &reference.version)?;
        if item.version != reference.version || digest_bytes(&item.contents) != reference.digest {
            return Err(ProjectConfigError::invalid(
                "application secret integrity differs",
            ));
        }
        let document: Value = serde_json::from_slice(&item.contents)
            .map_err(|_| ProjectConfigError::invalid("application secret must be a JSON object"))?;
        let record = super::models::require_object(Some(&document), "application secret")?;
        let value = match record.get("value") {
            Some(Value::String(value))
                if record.get("scope") == Some(&self.scope.to_wire())
                    && record.get("name") == Some(&Value::String(name.to_string())) =>
            {
                value.clone()
            }
            _ => {
                return Err(ProjectConfigError::invalid(
                    "application secret scope or identity differs",
                ));
            }
        };
        validate_environment_value(&value)?;
        Ok(value)
    }

    fn create_secret(&self, name: &str, value: &str) -> ProjectConfigResult<ApplicationSecretRef> {
        let key = format!(
            "{}/app-secrets/{name}/{}",
            self.scope.prefix()?,
            uuid::Uuid::new_v4().simple()
        );
        let mut record = JsonObject::new();
        record.insert("scope".into(), self.scope.to_wire());
        record.insert("name".into(), Value::String(name.to_string()));
        record.insert("value".into(), Value::String(value.to_string()));
        let contents = go_canonical_json_bytes(&Value::Object(record))?;
        let item = match self.objects.create_object(&key, &contents) {
            Ok(item) => item,
            Err(ProjectConfigError::ObjectConflict(_) | ProjectConfigError::OutcomeUnknown(_)) => {
                let item = self.objects.read_object(&key, "")?;
                if item.contents != contents {
                    return Err(ProjectConfigError::invalid(
                        "application secret content differs",
                    ));
                }
                item
            }
            Err(error) => return Err(error),
        };
        Ok(ApplicationSecretRef::new(
            key,
            item.version,
            digest_bytes(&contents),
        ))
    }

    fn decode(&self, item: &ProjectObject) -> ProjectConfigResult<Configuration> {
        if item.key != self.key || item.version.is_empty() || item.etag.is_empty() {
            return Err(ProjectConfigError::invalid(
                "configuration document identity is invalid",
            ));
        }
        let configuration = Configuration::from_json_bytes(&item.contents)?;
        if configuration.scope != self.scope {
            return Err(ProjectConfigError::invalid(
                "configuration document scope differs",
            ));
        }
        Ok(configuration)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn put_object_input_debug_omits_the_body() {
        let input = S3PutObjectInput {
            bucket: "bucket".into(),
            key: "prefix/secret".into(),
            body: b"super-secret-token".to_vec(),
            content_type: "application/json".into(),
            if_none_match: "*".into(),
            ..S3PutObjectInput::default()
        };
        let rendered = format!("{input:?}");
        assert!(rendered.contains("body_len: 18"), "{rendered}");
        assert!(rendered.contains("prefix/secret"), "{rendered}");
        assert!(!rendered.contains("super-secret-token"), "{rendered}");
        assert!(!rendered.contains("[115, "), "{rendered}");
    }
}
