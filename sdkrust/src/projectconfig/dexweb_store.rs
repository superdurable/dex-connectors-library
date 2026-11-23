// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Process-local Dex Web credential projection and guarded file write-back.

use std::collections::HashMap;
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

use base64::Engine;
use serde_json::Value;
use sha2::{Digest, Sha256};

use super::connections::{ConnectionKey, ConnectionStore, CredentialMaterial};
use super::errors::{ProjectConfigError, ProjectConfigResult};
use super::models::{
    JsonObject, LOCAL_CONNECTIONS_SCHEMA_VERSION, Scope, go_canonical_json_bytes, require_object,
    sha256_hex,
};
use super::refresh_lock::{FileRefreshLock, HeldRefreshLock};
use super::storage::{
    InMemoryObjectStore, ObjectStore, ProjectObject, local_object_byte_limit, lock,
    validate_object_key,
};
use crate::timestamps::Rfc3339NanoTimestamp;

/// Reads a Dex Web owner-only regular JSON file without following symlinks.
pub(crate) fn read_private_file(path: &Path) -> ProjectConfigResult<JsonObject> {
    decode_private_file(path, &read_private_bytes(path)?)
}

fn private_file_name(path: &Path) -> String {
    path.file_name()
        .map_or_else(String::new, |name| name.to_string_lossy().into_owned())
}

/// Reads the exact bytes of a Dex Web owner-only regular file without following symlinks.
pub(crate) fn read_private_bytes(path: &Path) -> ProjectConfigResult<Vec<u8>> {
    let name = private_file_name(path);
    let metadata = fs::symlink_metadata(path)
        .map_err(|_| ProjectConfigError::Io(format!("{name} cannot be read")))?;
    if !metadata.file_type().is_file() || !has_owner_only_mode(&metadata) {
        return Err(ProjectConfigError::invalid(format!(
            "{name} must be a regular 0600 file"
        )));
    }
    fs::read(path).map_err(|_| ProjectConfigError::Io(format!("{name} cannot be read")))
}

/// Decodes bytes read by [`read_private_bytes`] as one JSON object.
pub(crate) fn decode_private_file(path: &Path, contents: &[u8]) -> ProjectConfigResult<JsonObject> {
    let name = private_file_name(path);
    let document: Value = serde_json::from_slice(contents)
        .map_err(|_| ProjectConfigError::invalid(format!("{name} must be a JSON object")))?;
    Ok(require_object(Some(&document), &name)?.clone())
}

#[cfg(unix)]
fn has_owner_only_mode(metadata: &fs::Metadata) -> bool {
    use std::os::unix::fs::PermissionsExt;
    metadata.permissions().mode() & 0o7777 == 0o600
}

#[cfg(not(unix))]
fn has_owner_only_mode(_metadata: &fs::Metadata) -> bool {
    true
}

/// Current Dex Web credentials and how many records list each connection.
struct LocalConnectionsListing {
    /// Material of every connection listed exactly once.
    credentials: HashMap<ConnectionKey, CredentialMaterial>,
    listing_counts: HashMap<ConnectionKey, usize>,
}

impl LocalConnectionsListing {
    /// Returns the connection whose head is `head_key`, or an error unless listed exactly once.
    fn require_only_listing_of_head(
        &self,
        scope: &Scope,
        head_key: &str,
    ) -> ProjectConfigResult<()> {
        for (key, count) in &self.listing_counts {
            if format!("{}/head", key.path(scope)?) == head_key {
                return require_only_listing(key, *count);
            }
        }
        Err(ProjectConfigError::ConnectionNotInFile(
            "the connection is no longer in connections.json".into(),
        ))
    }

    fn require_only_listing(&self, key: &ConnectionKey) -> ProjectConfigResult<()> {
        require_only_listing(key, self.listing_counts.get(key).copied().unwrap_or(0))
    }
}

fn require_only_listing(key: &ConnectionKey, count: usize) -> ProjectConfigResult<()> {
    match count {
        1 => Ok(()),
        0 => Err(ProjectConfigError::ConnectionNotInFile(format!(
            "connection {:?} of connector {:?} is no longer in connections.json",
            key.connection_name, key.connector_id
        ))),
        _ => Err(ProjectConfigError::ConnectionNotInFile(format!(
            "connection {:?} of connector {:?} is listed more than once in connections.json",
            key.connection_name, key.connector_id
        ))),
    }
}

/// Reads current Dex Web credential objects without changing frozen settings.
///
/// As in Go, a duplicated connection fails only that connection's resolutions.
fn decode_local_credentials(source: &JsonObject) -> ProjectConfigResult<LocalConnectionsListing> {
    if source.get("schemaVersion") != Some(&Value::String(LOCAL_CONNECTIONS_SCHEMA_VERSION.into()))
    {
        return Err(ProjectConfigError::invalid(
            "unsupported connector configuration schema version",
        ));
    }
    let connections = source
        .get("connections")
        .and_then(Value::as_array)
        .ok_or_else(|| ProjectConfigError::invalid("connections.json requires connections"))?;
    let local_scope = Scope::new("local", "preview");
    let mut credentials = HashMap::new();
    let mut listing_counts: HashMap<ConnectionKey, usize> = HashMap::new();
    for (index, raw) in connections.iter().enumerate() {
        let record = require_object(Some(raw), &format!("connection {index}"))?;
        let key = ConnectionKey::new(
            super::models::string_member(record, "connectorId"),
            super::models::string_member(record, "connectionName"),
        );
        key.path(&local_scope)?;
        *listing_counts.entry(key.clone()).or_default() += 1;
        let material = require_object(record.get("credentials"), "credentials")?.clone();
        let auth_method = match record.get("authMethodId").and_then(Value::as_str) {
            Some(method) if !method.is_empty() => method.to_string(),
            _ => match record.get("authMethodIds").and_then(Value::as_array) {
                Some(methods) if methods.len() == 1 => match &methods[0] {
                    Value::String(method) => method.clone(),
                    other => other.to_string(),
                },
                _ => "local".to_string(),
            },
        };
        let expires_at = match record.get("credentialExpiresAt") {
            None | Some(Value::Null) => None,
            Some(Value::String(text)) if text.is_empty() => None,
            Some(Value::String(text)) => {
                Some(Rfc3339NanoTimestamp::from_wire(text).map_err(|_| {
                    ProjectConfigError::invalid("local credential expiry is invalid")
                })?)
            }
            Some(_) => {
                return Err(ProjectConfigError::invalid(
                    "local credential expiry is invalid",
                ));
            }
        };
        credentials.insert(
            key,
            CredentialMaterial {
                credentials: material,
                expires_at,
                auth_method,
            },
        );
    }
    credentials.retain(|key, _| listing_counts.get(key) == Some(&1));
    Ok(LocalConnectionsListing {
        credentials,
        listing_counts,
    })
}

fn sorted_json(value: &Value) -> Value {
    match value {
        Value::Object(object) => {
            let mut entries: Vec<(&String, &Value)> = object.iter().collect();
            entries.sort_by(|left, right| left.0.cmp(right.0));
            Value::Object(
                entries
                    .into_iter()
                    .map(|(key, value)| (key.clone(), sorted_json(value)))
                    .collect(),
            )
        }
        Value::Array(items) => Value::Array(items.iter().map(sorted_json).collect()),
        other => other.clone(),
    }
}

fn to_utc(value: &Rfc3339NanoTimestamp) -> Rfc3339NanoTimestamp {
    value.to_utc()
}

/// Identifies local material independently of member order and spacing.
fn credential_digest(material: &CredentialMaterial) -> String {
    let canonical =
        go_canonical_json_bytes(&sorted_json(&Value::Object(material.credentials.clone())))
            .unwrap_or_default();
    let mut hasher = Sha256::new();
    hasher.update(&canonical);
    if let Some(expires_at) = &material.expires_at {
        hasher.update(b"|");
        hasher.update(to_utc(expires_at).as_wire_str().as_bytes());
    }
    hasher
        .finalize()
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

/// Project object store backed by memory and Dex Web's owner-only `connections.json`.
///
/// Connection head reads adopt credentials Dex Web changed. A successful head write
/// publishes refreshed material back to the file only if Dex Web has not saved another
/// credential meanwhile. Trigger inbox objects live as private files beside the file.
///
/// A head is readable and writable only while the file lists its connection exactly once.
/// Credential providers refresh while holding the exclusive `flock(2)` of
/// `connections.json.lock` beside the file, the lock sdkgo processes take, after proving
/// that the file can be written back.
pub struct DexWebObjectStore {
    path: PathBuf,
    inboxes: LocalFileObjectStore,
    memory: Arc<InMemoryObjectStore>,
    connections: ConnectionStore,
    refresh_lock: FileRefreshLock,
    sync_lock: Mutex<()>,
    imported: Mutex<HashMap<ConnectionKey, String>>,
}

impl DexWebObjectStore {
    /// Loads local Trigger inboxes, then imports the current private credentials.
    ///
    /// # Errors
    ///
    /// Returns an error when a file is unreadable, loosely permissioned, or invalid.
    pub fn open(connections_path: &Path, scope: Scope) -> ProjectConfigResult<Self> {
        let directory = connections_path
            .parent()
            .map_or_else(|| PathBuf::from("."), Path::to_path_buf);
        let memory = Arc::new(InMemoryObjectStore::new());
        let memory_objects: Arc<dyn ObjectStore> = memory.clone();
        let store = Self {
            path: connections_path.to_path_buf(),
            inboxes: LocalFileObjectStore::open(directory.join("trigger-inboxes"))?,
            connections: ConnectionStore::new(memory_objects, scope),
            memory,
            refresh_lock: FileRefreshLock::beside(connections_path),
            sync_lock: Mutex::new(()),
            imported: Mutex::new(HashMap::new()),
        };
        {
            let _sync = lock(&store.sync_lock);
            store.sync()?;
        }
        Ok(store)
    }

    /// Adopts credentials Dex Web saved since the last call and returns the listing read.
    /// The caller holds `sync_lock`.
    fn sync(&self) -> ProjectConfigResult<LocalConnectionsListing> {
        let listing = decode_local_credentials(&read_private_file(&self.path)?)?;
        for (key, material) in &listing.credentials {
            let digest = credential_digest(material);
            if lock(&self.imported).get(key) == Some(&digest) {
                continue;
            }
            let revision = match self.connections.read_connection(key) {
                Ok(connection) => connection.revision,
                Err(ProjectConfigError::ObjectNotFound) => 0,
                Err(error) => return Err(error),
            };
            self.connections
                .replace_credential(key, revision, material)?;
            lock(&self.imported).insert(key.clone(), digest);
        }
        Ok(listing)
    }

    /// Waits for the cross-process refresh lock of the connections file.
    ///
    /// Never call it while holding `sync_lock`.
    pub(crate) fn lock_credential_refresh(&self) -> ProjectConfigResult<HeldRefreshLock<'_>> {
        self.refresh_lock.acquire()
    }

    /// Rewrites the file unchanged through the write-back path before a provider rotates a
    /// token, as Go's `verifyCredentialWriteBack` does.
    ///
    /// Returns `Ok(false)` when the file's credential differs from the one last imported,
    /// so the caller rereads instead of refreshing. The caller holds the refresh lock.
    pub(crate) fn verify_credential_write_back(
        &self,
        key: &ConnectionKey,
    ) -> ProjectConfigResult<bool> {
        let _sync = lock(&self.sync_lock);
        let contents = read_private_bytes(&self.path)?;
        let listing = decode_local_credentials(&decode_private_file(&self.path, &contents)?)?;
        listing.require_only_listing(key)?;
        let file_digest = listing.credentials.get(key).map(credential_digest);
        if file_digest.is_none() || file_digest != lock(&self.imported).get(key).cloned() {
            return Ok(false);
        }
        // A writer that skips the lock could replace the file after this read; keep its write.
        write_private_file_checked(&self.path, &contents, || {
            if read_private_bytes(&self.path)? == contents {
                Ok(())
            } else {
                Err(ProjectConfigError::ObjectConflict(
                    "connections.json changed during the write-back check".into(),
                ))
            }
        })?;
        Ok(true)
    }

    fn write_back(&self, head: &ProjectObject) {
        if let Err(error) = self.try_write_back(head) {
            tracing::warn!(reason = %error, "local connector credential write-back skipped");
        }
    }

    fn try_write_back(&self, head: &ProjectObject) -> ProjectConfigResult<()> {
        let document: Value = serde_json::from_slice(&head.contents)
            .map_err(|_| ProjectConfigError::invalid("connection head is invalid"))?;
        let record = require_object(Some(&document), "connection head")?;
        if record.get("status").and_then(Value::as_str) != Some("READY")
            || !record.get("credential").is_some_and(Value::is_object)
        {
            return Ok(());
        }
        let identity = require_object(record.get("connection"), "connection identity")?;
        let key = ConnectionKey::new(
            super::models::string_member(identity, "connectorId"),
            super::models::string_member(identity, "connectionName"),
        );
        let (material, _) = self.connections.read_credential_material(&key)?;
        let digest = credential_digest(&material);
        if lock(&self.imported).get(&key) == Some(&digest) {
            return Ok(());
        }
        self.replace_in_file(&key, &material)?;
        lock(&self.imported).insert(key, digest);
        Ok(())
    }

    fn replace_in_file(
        &self,
        key: &ConnectionKey,
        material: &CredentialMaterial,
    ) -> ProjectConfigResult<()> {
        let mut source = read_private_file(&self.path)?;
        let imported_digest = lock(&self.imported).get(key).cloned();
        let records = source
            .get_mut("connections")
            .and_then(Value::as_array_mut)
            .ok_or_else(|| ProjectConfigError::invalid("connections.json requires connections"))?;
        let record = records
            .iter_mut()
            .filter_map(Value::as_object_mut)
            .find(|record| {
                record.get("connectorId").and_then(Value::as_str) == Some(key.connector_id.as_str())
                    && record.get("connectionName").and_then(Value::as_str)
                        == Some(key.connection_name.as_str())
            })
            .ok_or_else(|| {
                ProjectConfigError::invalid("the connection is no longer in the file")
            })?;
        let prior = require_object(record.get("credentials"), "credentials")?.clone();
        let prior_expiry = match record.get("credentialExpiresAt") {
            None | Some(Value::Null) => None,
            Some(Value::String(text)) if text.is_empty() => None,
            Some(Value::String(text)) => Some(Rfc3339NanoTimestamp::from_wire(text)?),
            Some(_) => {
                return Err(ProjectConfigError::invalid(
                    "local credential expiry is invalid",
                ));
            }
        };
        let prior_digest = credential_digest(&CredentialMaterial {
            credentials: prior,
            expires_at: prior_expiry,
            auth_method: material.auth_method.clone(),
        });
        if Some(prior_digest) != imported_digest {
            return Err(ProjectConfigError::invalid(
                "the file holds a newer credential",
            ));
        }
        record.insert(
            "credentials".into(),
            Value::Object(material.credentials.clone()),
        );
        match &material.expires_at {
            None => {
                record.remove("credentialExpiresAt");
            }
            Some(expires_at) => {
                record.insert(
                    "credentialExpiresAt".into(),
                    Value::String(to_utc(expires_at).to_string()),
                );
            }
        }
        let mut encoded = serde_json::to_vec_pretty(&Value::Object(source))
            .map_err(|_| ProjectConfigError::invalid("connections.json cannot be encoded"))?;
        encoded.push(b'\n');
        write_private_file(&self.path, &encoded)
    }
}

impl ObjectStore for DexWebObjectStore {
    fn read_object(&self, key: &str, version: &str) -> ProjectConfigResult<ProjectObject> {
        if is_trigger_inbox_key(key) {
            return self.inboxes.read_object(key, version);
        }
        if is_connection_head(key) {
            let _sync = lock(&self.sync_lock);
            self.sync()?
                .require_only_listing_of_head(self.connections.scope(), key)?;
            return self.memory.read_object(key, version);
        }
        self.memory.read_object(key, version)
    }

    fn dex_web_connections_file(&self) -> Option<&DexWebObjectStore> {
        Some(self)
    }

    fn create_object(&self, key: &str, contents: &[u8]) -> ProjectConfigResult<ProjectObject> {
        if is_trigger_inbox_key(key) {
            return self.inboxes.create_object(key, contents);
        }
        self.memory.create_object(key, contents)
    }

    fn compare_and_swap_object(
        &self,
        key: &str,
        expected_etag: &str,
        contents: &[u8],
    ) -> ProjectConfigResult<ProjectObject> {
        if is_trigger_inbox_key(key) {
            return self
                .inboxes
                .compare_and_swap_object(key, expected_etag, contents);
        }
        if !is_connection_head(key) {
            return self
                .memory
                .compare_and_swap_object(key, expected_etag, contents);
        }
        let _sync = lock(&self.sync_lock);
        self.sync()?
            .require_only_listing_of_head(self.connections.scope(), key)?;
        let result = self
            .memory
            .compare_and_swap_object(key, expected_etag, contents)?;
        self.write_back(&result);
        Ok(result)
    }
}

/// Writes `contents` to `path` atomically as an owner-only file.
pub(crate) fn write_private_file(path: &Path, contents: &[u8]) -> ProjectConfigResult<()> {
    write_private_file_checked(path, contents, || Ok(()))
}

/// Is [`write_private_file`] that calls `check_before_rename` last and keeps `path` when it
/// fails.
fn write_private_file_checked(
    path: &Path,
    contents: &[u8],
    check_before_rename: impl FnOnce() -> ProjectConfigResult<()>,
) -> ProjectConfigResult<()> {
    let directory = path
        .parent()
        .map_or_else(|| PathBuf::from("."), Path::to_path_buf);
    let temporary = directory.join(format!(".write-{}", uuid::Uuid::new_v4().simple()));
    let mut check_failure = None;
    let result = (|| -> std::io::Result<()> {
        let mut options = fs::OpenOptions::new();
        options.write(true).create_new(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600);
        }
        let mut file = options.open(&temporary)?;
        file.write_all(contents)?;
        file.sync_all()?;
        if let Err(error) = check_before_rename() {
            check_failure = Some(error);
            return Err(std::io::Error::other("check before rename failed"));
        }
        fs::rename(&temporary, path)?;
        #[cfg(unix)]
        if let Ok(directory_file) = fs::File::open(&directory) {
            // Directory fsync makes the rename durable; failure leaves only durability weaker.
            if let Err(error) = directory_file.sync_all() {
                tracing::debug!(error = %error, "private file directory sync failed");
            }
        }
        Ok(())
    })();
    if result.is_err()
        && let Err(error) = fs::remove_file(&temporary)
    {
        tracing::debug!(error = %error, "temporary private file cleanup failed");
    }
    if let Some(error) = check_failure {
        return Err(error);
    }
    result.map_err(|_| ProjectConfigError::Io("private file write failed".into()))
}

/// Versioned objects kept in memory and as private files, in the Go and TypeScript layout.
///
/// Each object is `<sha256 of key>-<version>.json` holding its key, version, ETag, and base64
/// contents.
struct LocalFileObjectStore {
    directory: PathBuf,
    objects: Mutex<HashMap<String, ProjectObject>>,
}

impl LocalFileObjectStore {
    fn open(directory: PathBuf) -> ProjectConfigResult<Self> {
        let io_error = |_| ProjectConfigError::Io("local project storage cannot be read".into());
        let mut builder = fs::DirBuilder::new();
        builder.recursive(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::DirBuilderExt;
            builder.mode(0o700);
        }
        builder.create(&directory).map_err(io_error)?;
        let mut loaded: HashMap<String, Vec<ProjectObject>> = HashMap::new();
        let mut paths: Vec<PathBuf> = fs::read_dir(&directory)
            .map_err(io_error)?
            .filter_map(Result::ok)
            .map(|entry| entry.path())
            .collect();
        paths.sort();
        for path in paths {
            let name = path
                .file_name()
                .map(|name| name.to_string_lossy().into_owned());
            let is_object_file = path
                .extension()
                .is_some_and(|extension| extension == "json")
                && name.is_some_and(|name| !name.starts_with('.'));
            if !is_object_file {
                continue;
            }
            let item = decode_local_object(&fs::read(&path).map_err(io_error)?)?;
            loaded.entry(item.key.clone()).or_default().push(item);
        }
        let store = Self {
            directory,
            objects: Mutex::new(HashMap::new()),
        };
        for (key, mut versions) in loaded {
            versions.sort_by_key(|item| item.version.parse::<u64>().unwrap_or(0));
            // A crash between writing a new version and deleting the old one leaves both.
            let Some(latest) = versions.pop() else {
                continue;
            };
            for stale in &versions {
                store.remove_file(stale);
            }
            lock(&store.objects).insert(key, latest);
        }
        Ok(store)
    }

    fn read_object(&self, key: &str, version: &str) -> ProjectConfigResult<ProjectObject> {
        lock(&self.objects)
            .get(key)
            .filter(|item| version.is_empty() || item.version == version)
            .cloned()
            .ok_or(ProjectConfigError::ObjectNotFound)
    }

    fn create_object(&self, key: &str, contents: &[u8]) -> ProjectConfigResult<ProjectObject> {
        let mut objects = lock(&self.objects);
        if objects.contains_key(key) {
            return Err(ProjectConfigError::ObjectConflict(
                "project object already exists".into(),
            ));
        }
        let item = self.write(key, contents, None)?;
        objects.insert(key.to_string(), item.clone());
        Ok(item)
    }

    fn compare_and_swap_object(
        &self,
        key: &str,
        expected_etag: &str,
        contents: &[u8],
    ) -> ProjectConfigResult<ProjectObject> {
        let mut objects = lock(&self.objects);
        let current = objects
            .get(key)
            .cloned()
            .ok_or(ProjectConfigError::ObjectNotFound)?;
        if expected_etag.is_empty() || current.etag != expected_etag {
            return Err(ProjectConfigError::ObjectConflict(
                "project object compare-and-swap conflict".into(),
            ));
        }
        let item = self.write(key, contents, Some(&current))?;
        objects.insert(key.to_string(), item.clone());
        Ok(item)
    }

    fn write(
        &self,
        key: &str,
        contents: &[u8],
        previous: Option<&ProjectObject>,
    ) -> ProjectConfigResult<ProjectObject> {
        // The new version is durable before the superseded file is deleted.
        validate_object_key(key)?;
        if contents.is_empty() || contents.len() > local_object_byte_limit(key) {
            return Err(ProjectConfigError::invalid(
                "project object body is empty or oversized",
            ));
        }
        let version = previous
            .map_or(1, |item| item.version.parse::<u64>().unwrap_or(0) + 1)
            .to_string();
        let item = ProjectObject {
            key: key.to_string(),
            // Go's format; files with an earlier `sha256:` prefix stay readable and swappable.
            etag: format!("{}-{version}", sha256_hex(contents)),
            version,
            contents: contents.to_vec(),
        };
        let mut record = JsonObject::new();
        record.insert("key".into(), Value::String(item.key.clone()));
        record.insert("version".into(), Value::String(item.version.clone()));
        record.insert("etag".into(), Value::String(item.etag.clone()));
        record.insert(
            "contents".into(),
            Value::String(base64::engine::general_purpose::STANDARD.encode(contents)),
        );
        let encoded = go_canonical_json_bytes(&Value::Object(record))?;
        write_private_file(&self.file(&item), &encoded)?;
        if let Some(previous) = previous {
            self.remove_file(previous);
        }
        Ok(item)
    }

    fn file(&self, item: &ProjectObject) -> PathBuf {
        self.directory.join(format!(
            "{}-{}.json",
            sha256_hex(item.key.as_bytes()),
            item.version
        ))
    }

    fn remove_file(&self, item: &ProjectObject) {
        // A failure leaves a stale file that the next load discards.
        if let Err(error) = fs::remove_file(self.file(item)) {
            tracing::debug!(error = %error, "stale local project object was not removed");
        }
    }
}

fn decode_local_object(contents: &[u8]) -> ProjectConfigResult<ProjectObject> {
    let invalid = || ProjectConfigError::invalid("local project storage file is invalid");
    let document: Value = serde_json::from_slice(contents).map_err(|_| invalid())?;
    let record = document.as_object().ok_or_else(invalid)?;
    let text = |name: &str| record.get(name).and_then(Value::as_str).ok_or_else(invalid);
    let (key, version, etag, encoded) = (
        text("key")?,
        text("version")?,
        text("etag")?,
        text("contents")?,
    );
    let is_version = version
        .bytes()
        .next()
        .is_some_and(|first| (b'1'..=b'9').contains(&first))
        && version.bytes().all(|byte| byte.is_ascii_digit());
    if key.is_empty() || etag.is_empty() || !is_version {
        return Err(invalid());
    }
    let decoded = base64::engine::general_purpose::STANDARD
        .decode(encoded)
        .map_err(|_| invalid())?;
    Ok(ProjectObject {
        key: key.into(),
        version: version.into(),
        etag: etag.into(),
        contents: decoded,
    })
}

fn is_trigger_inbox_key(key: &str) -> bool {
    key.contains("/trigger-inboxes/")
}

fn is_connection_head(key: &str) -> bool {
    key.contains("/connections/") && key.ends_with("/head")
}
