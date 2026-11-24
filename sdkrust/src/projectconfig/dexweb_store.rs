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
use serde_json::value::RawValue;
use sha2::{Digest, Sha256};

use super::connections::{ConnectionKey, ConnectionStore, CredentialMaterial};
use super::errors::{ProjectConfigError, ProjectConfigResult};
use super::models::{
    JsonObject, LOCAL_CONNECTIONS_SCHEMA_VERSION, Scope, go_canonical_json_bytes, require_object,
    sha256_hex,
};
use super::raw_json::{
    RawJsonMembers, decode_raw_members, decode_raw_object_array, raw_member_value,
};
use super::refresh_lock::{FileRefreshLock, HeldRefreshLock};
use super::storage::{
    InMemoryObjectStore, ObjectStore, ProjectObject, lock, validate_local_object_write,
};
use crate::timestamps::Rfc3339NanoTimestamp;

pub(crate) fn private_file_name(path: &Path) -> String {
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

/// Decodes private file bytes as one object whose members stay raw, so `1e400` survives.
pub(crate) fn decode_private_raw_file(
    path: &Path,
    contents: &[u8],
) -> ProjectConfigResult<RawJsonMembers> {
    let name = private_file_name(path);
    let text = std::str::from_utf8(contents)
        .map_err(|_| ProjectConfigError::invalid(format!("{name} must be a JSON object")))?;
    decode_raw_members(text, &name)
}

/// Dex Web `connections.json` decoded one level deep from one read of its bytes.
pub(crate) struct DexWebConnectionsFile {
    members: RawJsonMembers,
    connections: Vec<RawJsonMembers>,
}

impl DexWebConnectionsFile {
    /// Decodes the file and requires its schema version and `connections` array.
    pub(crate) fn decode(path: &Path, contents: &[u8]) -> ProjectConfigResult<Self> {
        let members = decode_private_raw_file(path, contents)?;
        if raw_member_value(&members, "schemaVersion")?
            != Some(Value::String(LOCAL_CONNECTIONS_SCHEMA_VERSION.into()))
        {
            return Err(ProjectConfigError::invalid(
                "unsupported connector configuration schema version",
            ));
        }
        let connections = decode_raw_object_array(&members, "connections", "connection")?
            .ok_or_else(|| ProjectConfigError::invalid("connections.json requires connections"))?;
        Ok(Self {
            members,
            connections,
        })
    }

    /// Returns the raw connection records in file order.
    pub(crate) fn connections(&self) -> &[RawJsonMembers] {
        &self.connections
    }

    /// Returns the raw top-level members.
    pub(crate) fn members(&self) -> &RawJsonMembers {
        &self.members
    }

    /// Encodes the file with `key`'s credential replaced, as Go's raw `MarshalIndent` write-back.
    fn encode_with_credential(
        &self,
        key: &ConnectionKey,
        material: &CredentialMaterial,
    ) -> ProjectConfigResult<Vec<u8>> {
        let encoding_failed = || ProjectConfigError::invalid("connections.json cannot be encoded");
        let raw_text = |text: String| RawValue::from_string(text).map_err(|_| encoding_failed());
        let mut records = self.connections.clone();
        let record = records
            .iter_mut()
            .find(|record| record_key(record).is_ok_and(|record_key| record_key == *key))
            .ok_or_else(|| {
                ProjectConfigError::ConnectionNotInFile(
                    "the connection is no longer in connections.json".into(),
                )
            })?;
        let credentials = go_canonical_json_bytes(&Value::Object(material.credentials.clone()))?;
        record.insert(
            "credentials".into(),
            raw_text(String::from_utf8(credentials).map_err(|_| encoding_failed())?)?,
        );
        match &material.expires_at {
            None => {
                record.remove("credentialExpiresAt");
            }
            Some(expires_at) => {
                let expiry = crate::go_json::to_go_json_string(&to_utc(expires_at).to_string())
                    .map_err(|_| encoding_failed())?;
                record.insert("credentialExpiresAt".into(), raw_text(expiry)?);
            }
        }
        let mut file = self.members.clone();
        let records = crate::go_json::to_go_json_string(&records).map_err(|_| encoding_failed())?;
        file.insert("connections".into(), raw_text(records)?);
        let compacted: RawJsonMembers = file
            .into_iter()
            .map(|(name, raw)| {
                Ok((
                    name,
                    raw_text(crate::go_json::compact_go_raw_message(raw.get()))?,
                ))
            })
            .collect::<ProjectConfigResult<_>>()?;
        let compact =
            crate::go_json::to_go_json_string(&compacted).map_err(|_| encoding_failed())?;
        let mut encoded = crate::go_json::indent_go_json(&compact, "  ").into_bytes();
        encoded.push(b'\n');
        Ok(encoded)
    }
}

/// Returns a raw record's connection identity, read as Go's typed decode reads strings.
pub(crate) fn record_key(record: &RawJsonMembers) -> ProjectConfigResult<ConnectionKey> {
    let text = |name: &str| -> ProjectConfigResult<String> {
        Ok(match raw_member_value(record, name)? {
            Some(Value::String(text)) => text,
            None | Some(Value::Null) => String::new(),
            Some(other) => other.to_string(),
        })
    };
    Ok(ConnectionKey::new(
        text("connectorId")?,
        text("connectionName")?,
    ))
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
    /// `authorizationId` of every connection listed exactly once; `""` when absent, as in Go.
    authorization_ids: HashMap<ConnectionKey, String>,
    listing_counts: HashMap<ConnectionKey, usize>,
}

/// Identity of one imported credential: its material and the authorization that produced it.
#[derive(Clone, Debug, PartialEq, Eq)]
struct ImportedCredential {
    digest: String,
    authorization_id: String,
}

impl ImportedCredential {
    fn new(material: &CredentialMaterial, authorization_id: &str) -> Self {
        Self {
            digest: credential_digest(material),
            authorization_id: authorization_id.to_string(),
        }
    }
}

impl LocalConnectionsListing {
    /// Returns the file identity of `key`'s credential when it is listed exactly once.
    fn imported_credential(&self, key: &ConnectionKey) -> Option<ImportedCredential> {
        self.credentials
            .get(key)
            .map(|material| ImportedCredential::new(material, &self.authorization_id(key)))
    }

    fn authorization_id(&self, key: &ConnectionKey) -> String {
        self.authorization_ids.get(key).cloned().unwrap_or_default()
    }

    /// Returns the connection whose head is `head_key`, or an error unless listed exactly once.
    fn require_only_listing_of_head(
        &self,
        scope: &Scope,
        head_key: &str,
    ) -> ProjectConfigResult<ConnectionKey> {
        for (key, count) in &self.listing_counts {
            if format!("{}/head", key.path(scope)?) == head_key {
                return require_only_listing(key, *count).map(|()| key.clone());
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
fn decode_local_credentials(
    file: &DexWebConnectionsFile,
) -> ProjectConfigResult<LocalConnectionsListing> {
    let local_scope = Scope::new("local", "preview");
    let mut credentials = HashMap::new();
    let mut authorization_ids = HashMap::new();
    let mut listing_counts: HashMap<ConnectionKey, usize> = HashMap::new();
    for record in file.connections() {
        let key = record_key(record)?;
        key.path(&local_scope)?;
        *listing_counts.entry(key.clone()).or_default() += 1;
        let authorization_id = match raw_member_value(record, "authorizationId")? {
            Some(Value::String(text)) => text,
            None | Some(Value::Null) => String::new(),
            Some(other) => other.to_string(),
        };
        authorization_ids.insert(key.clone(), authorization_id);
        let material = require_object(
            raw_member_value(record, "credentials")?.as_ref(),
            "credentials",
        )?
        .clone();
        let auth_method = match raw_member_value(record, "authMethodId")? {
            Some(Value::String(method)) if !method.is_empty() => method,
            _ => match raw_member_value(record, "authMethodIds")? {
                Some(Value::Array(methods)) if methods.len() == 1 => match &methods[0] {
                    Value::String(method) => method.clone(),
                    other => other.to_string(),
                },
                _ => "local".to_string(),
            },
        };
        let expires_at = match raw_member_value(record, "credentialExpiresAt")?.as_ref() {
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
    authorization_ids.retain(|key, _| listing_counts.get(key) == Some(&1));
    Ok(LocalConnectionsListing {
        credentials,
        authorization_ids,
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
/// Connection head reads adopt credentials Dex Web changed. A READY head write first writes
/// its credential back to the file, only if Dex Web has not saved another credential
/// meanwhile, and publishes the head only after that write succeeds. A failed file write,
/// such as a read-only directory or a full disk, returns [`ProjectConfigError::Io`] and
/// leaves the head unchanged. A refresh provider then keeps its exchange result in this
/// process, and its next resolution writes that result back without contacting the provider
/// again; a restart before then loses the result. Trigger inbox objects live as private
/// files beside the file.
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
    imported: Mutex<HashMap<ConnectionKey, ImportedCredential>>,
    /// Runs once inside the next pre-rename check, so tests interleave another writer.
    #[cfg(test)]
    before_rename_check: Mutex<Option<Box<dyn FnOnce() + Send>>>,
    /// Number of upcoming pre-rename reads that fail with this I/O error kind.
    #[cfg(test)]
    final_read_failures: Mutex<Option<(usize, std::io::ErrorKind)>>,
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
            #[cfg(test)]
            before_rename_check: Mutex::new(None),
            #[cfg(test)]
            final_read_failures: Mutex::new(None),
        };
        {
            let _sync = lock(&store.sync_lock);
            store.sync()?;
        }
        Ok(store)
    }

    /// Adopts credentials Dex Web saved since the last call and returns the listing read.
    /// The caller holds `sync_lock`.
    ///
    /// Forgetting a deleted connection makes its recreation re-import, which bumps the fence
    /// and so supersedes any admission and held exchange result of the deleted one.
    fn sync(&self) -> ProjectConfigResult<LocalConnectionsListing> {
        let contents = read_private_bytes(&self.path)?;
        let listing =
            decode_local_credentials(&DexWebConnectionsFile::decode(&self.path, &contents)?)?;
        lock(&self.imported).retain(|key, _| listing.listing_counts.contains_key(key));
        for (key, material) in &listing.credentials {
            let identity = ImportedCredential::new(material, &listing.authorization_id(key));
            if lock(&self.imported).get(key) == Some(&identity) {
                continue;
            }
            let revision = match self.connections.read_connection(key) {
                Ok(connection) => connection.revision,
                Err(ProjectConfigError::ObjectNotFound) => 0,
                Err(error) => return Err(error),
            };
            self.connections
                .replace_credential(key, revision, material)?;
            lock(&self.imported).insert(key.clone(), identity);
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
        let listing =
            decode_local_credentials(&DexWebConnectionsFile::decode(&self.path, &contents)?)?;
        listing.require_only_listing(key)?;
        let file_identity = listing.imported_credential(key);
        if file_identity.is_none() || file_identity != lock(&self.imported).get(key).cloned() {
            return Ok(false);
        }
        // A writer that skips the lock could replace the file after this read; keep its write.
        self.write_if_file_unchanged(&contents, &contents)?;
        Ok(true)
    }

    /// Writes a READY head's credential to the file first; an error means do not publish.
    ///
    /// Returns the written identity, or `None` when nothing needs writing. Unlike Go, which
    /// logs an I/O failure and publishes anyway, an I/O failure is an error, so memory never
    /// runs ahead of the file that other processes and a restart read.
    fn write_back_before_publish(
        &self,
        key: &ConnectionKey,
        authorization_id: &str,
        head_contents: &[u8],
    ) -> ProjectConfigResult<Option<ImportedCredential>> {
        let material = match self.connections.decode_record(key, head_contents) {
            Ok(record) if record.status == "READY" && record.credential.is_some() => {
                match self.connections.read_material(key, &record) {
                    Ok(material) => material,
                    Err(error) => {
                        tracing::warn!(reason = %error, "local connector credential write-back skipped");
                        return Ok(None);
                    }
                }
            }
            Ok(_) => return Ok(None),
            Err(error) => {
                tracing::warn!(reason = %error, "local connector credential write-back skipped");
                return Ok(None);
            }
        };
        let identity = ImportedCredential::new(&material, authorization_id);
        if lock(&self.imported).get(key) == Some(&identity) {
            return Ok(None);
        }
        match self.replace_in_file(key, &material) {
            Ok(written) => Ok(Some(written)),
            Err(ProjectConfigError::Io(message)) => {
                tracing::error!(
                    reason = %message,
                    "local connector credential write-back failed; the credential is not published"
                );
                Err(ProjectConfigError::Io(format!(
                    "local connector credential write-back failed, so the credential is not published; a refreshed credential stays held in this process and a retry writes it back: {message}"
                )))
            }
            Err(error) => Err(error),
        }
    }

    /// Replaces `key`'s credential, building and checking the new file from one read, and
    /// returns the written identity.
    ///
    /// Deleted or duplicated: `ConnectionNotInFile`; newer credential or authorization, or
    /// changed file: `ObjectConflict`.
    fn replace_in_file(
        &self,
        key: &ConnectionKey,
        material: &CredentialMaterial,
    ) -> ProjectConfigResult<ImportedCredential> {
        let contents = read_private_bytes(&self.path)?;
        let file = DexWebConnectionsFile::decode(&self.path, &contents)?;
        let listing = decode_local_credentials(&file)?;
        listing.require_only_listing(key)?;
        if listing.imported_credential(key) != lock(&self.imported).get(key).cloned() {
            return Err(ProjectConfigError::ObjectConflict(
                "connections.json holds a newer credential".into(),
            ));
        }
        let updated = file.encode_with_credential(key, material)?;
        self.write_if_file_unchanged(&contents, &updated)?;
        Ok(ImportedCredential::new(
            material,
            &listing.authorization_id(key),
        ))
    }

    /// Replaces the file with `updated` only while it still holds exactly `read`.
    ///
    /// A changed or deleted file is `ObjectConflict`; a failed read is a retryable `Io`. A
    /// lock-skipping writer can still land between check and rename.
    fn write_if_file_unchanged(&self, read: &[u8], updated: &[u8]) -> ProjectConfigResult<()> {
        write_private_file_checked(&self.path, updated, || {
            #[cfg(test)]
            if let Some(hook) = lock(&self.before_rename_check).take() {
                hook();
            }
            let changed = || {
                ProjectConfigError::ObjectConflict(
                    "connections.json changed before the credential write-back".into(),
                )
            };
            match self.read_before_rename() {
                Ok(current) if current == read => Ok(()),
                Err(ProjectConfigError::Io(message)) if !is_missing(&self.path) => {
                    Err(ProjectConfigError::Io(format!(
                        "connections.json cannot be verified before the credential write-back: {message}"
                    )))
                }
                _ => Err(changed()),
            }
        })
    }

    fn read_before_rename(&self) -> ProjectConfigResult<Vec<u8>> {
        #[cfg(test)]
        {
            let mut failures = lock(&self.final_read_failures);
            if let Some((remaining, kind)) = failures.as_mut()
                && *remaining > 0
            {
                *remaining -= 1;
                return Err(ProjectConfigError::Io(
                    std::io::Error::from(*kind).to_string(),
                ));
            }
        }
        read_private_bytes(&self.path)
    }
}

/// Reports whether `path` is confirmed absent, rather than unreadable.
fn is_missing(path: &Path) -> bool {
    fs::symlink_metadata(path).is_err_and(|error| error.kind() == std::io::ErrorKind::NotFound)
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
        // The memory swap repeats this check; a write it would reject must not reach the file.
        validate_local_object_write(key, contents)?;
        let _sync = lock(&self.sync_lock);
        let listing = self.sync()?;
        let connection_key = listing.require_only_listing_of_head(self.connections.scope(), key)?;
        // Every head write holds `sync_lock`, so this check stays true until the swap below.
        let current = self.memory.read_object(key, "")?;
        if expected_etag.is_empty() || current.etag != expected_etag {
            return Err(ProjectConfigError::ObjectConflict(
                "project object compare-and-swap conflict".into(),
            ));
        }
        let written_identity = self.write_back_before_publish(
            &connection_key,
            &listing.authorization_id(&connection_key),
            contents,
        )?;
        let result = self
            .memory
            .compare_and_swap_object(key, expected_etag, contents)?;
        if let Some(identity) = written_identity {
            lock(&self.imported).insert(connection_key, identity);
        }
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
        validate_local_object_write(key, contents)?;
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

#[cfg(all(test, unix))]
mod tests {
    use std::os::unix::fs::PermissionsExt;

    use super::*;

    /// Dex Web file with exact nested numbers that a `Value` cannot hold.
    const CONNECTIONS: &str = r#"{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{"connectorId":"sample","connectionName":"main","modulePath":"example.com/sample","provider":"sample","authMethodId":"oauth2","configuration":{"huge": 1e400, "account": 12345678901234567890123},"credentials":{"access":"old","refresh":"r0"},"credentialExpiresAt":"2030-01-01T00:00:00Z","dexWebOnly":{"keep": 1.50}},{"connectorId":"sample","connectionName":"other","modulePath":"example.com/sample","provider":"sample","authMethodId":"apiKey","configuration":{"ratio":2.5E+3, "tiny":-0.0e-0},"credentials":{"apiKey":"k"}}],"triggerBindings":[{"connectorId":"sample","connectionName":"main","triggerName":"poll","bindingName":"nightly","configuration":{"limit":1.50,"big":1e400}}]}"#;

    struct Fixture {
        _directory: tempfile::TempDir,
        path: PathBuf,
        store: Arc<DexWebObjectStore>,
        connections: ConnectionStore,
        head_key: String,
    }

    fn write_private(path: &Path, contents: &[u8]) {
        fs::write(path, contents).expect("writes");
        fs::set_permissions(path, fs::Permissions::from_mode(0o600)).expect("chmod");
    }

    fn open(contents: &str) -> Fixture {
        let directory = tempfile::tempdir().expect("temporary directory");
        let path = directory.path().join("connections.json");
        write_private(&path, contents.as_bytes());
        let scope = Scope::new("local", "preview");
        let store = Arc::new(DexWebObjectStore::open(&path, scope.clone()).expect("opens"));
        let objects: Arc<dyn ObjectStore> = store.clone();
        let head_key = format!("{}/head", main_key().path(&scope).expect("path"));
        Fixture {
            _directory: directory,
            path,
            store,
            connections: ConnectionStore::new(objects, scope),
            head_key,
        }
    }

    fn main_key() -> ConnectionKey {
        ConnectionKey::new("sample", "main")
    }

    fn refreshed_material() -> CredentialMaterial {
        let mut credentials = JsonObject::new();
        credentials.insert("access".into(), Value::String("new".into()));
        credentials.insert("refresh".into(), Value::String("r1".into()));
        CredentialMaterial {
            credentials,
            expires_at: Some(
                Rfc3339NanoTimestamp::from_wire("2030-01-02T03:04:05.123456789+02:00")
                    .expect("timestamp"),
            ),
            auth_method: "oauth2".into(),
        }
    }

    /// Saves the refreshed credential, running `interleave` between the final read and rename.
    fn refresh_with(
        fixture: &Fixture,
        interleave: impl FnOnce() + Send + 'static,
    ) -> ProjectConfigResult<()> {
        *lock(&fixture.store.before_rename_check) = Some(Box::new(interleave));
        let revision = fixture
            .connections
            .read_connection(&main_key())
            .expect("reads")
            .revision;
        fixture
            .connections
            .replace_credential(&main_key(), revision, &refreshed_material())
            .map(|_| ())
    }

    #[test]
    fn write_back_matches_go_and_keeps_untouched_raw_members() {
        let fixture = open(CONNECTIONS);
        refresh_with(&fixture, || {}).expect("refreshes");
        // Derived by running Go 1.26.5 (GOWORK=off, under /tmp) on CONNECTIONS with the code of
        // sdkgo dexWebStore.replaceInFile: map[string]json.RawMessage records, json.MarshalIndent.
        let expected = "{\n  \"connections\": [\n    {\n      \"authMethodId\": \"oauth2\",\n      \"configuration\": {\n        \"huge\": 1e400,\n        \"account\": 12345678901234567890123\n      },\n      \"connectionName\": \"main\",\n      \"connectorId\": \"sample\",\n      \"credentialExpiresAt\": \"2030-01-02T01:04:05.123456789Z\",\n      \"credentials\": {\n        \"access\": \"new\",\n        \"refresh\": \"r1\"\n      },\n      \"dexWebOnly\": {\n        \"keep\": 1.50\n      },\n      \"modulePath\": \"example.com/sample\",\n      \"provider\": \"sample\"\n    },\n    {\n      \"authMethodId\": \"apiKey\",\n      \"configuration\": {\n        \"ratio\": 2.5E+3,\n        \"tiny\": -0.0e-0\n      },\n      \"connectionName\": \"other\",\n      \"connectorId\": \"sample\",\n      \"credentials\": {\n        \"apiKey\": \"k\"\n      },\n      \"modulePath\": \"example.com/sample\",\n      \"provider\": \"sample\"\n    }\n  ],\n  \"schemaVersion\": \"connectors.dex.dev/local-connections/v1alpha1\",\n  \"triggerBindings\": [\n    {\n      \"connectorId\": \"sample\",\n      \"connectionName\": \"main\",\n      \"triggerName\": \"poll\",\n      \"bindingName\": \"nightly\",\n      \"configuration\": {\n        \"limit\": 1.50,\n        \"big\": 1e400\n      }\n    }\n  ]\n}\n";
        assert_eq!(
            String::from_utf8(fs::read(&fixture.path).expect("reads")).expect("UTF-8"),
            expected
        );
        let (material, _) = fixture
            .connections
            .read_credential_material(&main_key())
            .expect("reads the published credential");
        assert_eq!(material.credentials["access"], "new");
    }

    #[test]
    fn deletion_before_rename_aborts_write_back_and_publish() {
        let fixture = open(CONNECTIONS);
        let head_before = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        let deleted =
            r#"{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[]}"#;
        let deleted_path = fixture.path.clone();
        let error = refresh_with(&fixture, move || {
            write_private(&deleted_path, deleted.as_bytes());
        })
        .expect_err("aborts");
        assert!(
            matches!(error, ProjectConfigError::ObjectConflict(_)),
            "{error}"
        );
        assert_eq!(fs::read(&fixture.path).expect("reads"), deleted.as_bytes());
        let head_after = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        assert_eq!(head_after.contents, head_before.contents);
        assert!(matches!(
            fixture.store.read_object(&fixture.head_key, ""),
            Err(ProjectConfigError::ConnectionNotInFile(_))
        ));
    }

    #[test]
    fn new_authorization_before_rename_aborts_write_back_and_is_adopted() {
        let fixture = open(CONNECTIONS);
        let head_before = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        let reauthorized = CONNECTIONS.replace(
            r#"{"access":"old","refresh":"r0"}"#,
            r#"{"access":"dex-web-new","refresh":"r9"}"#,
        );
        let reauthorized_path = fixture.path.clone();
        let written = reauthorized.clone();
        let error = refresh_with(&fixture, move || {
            write_private(&reauthorized_path, written.as_bytes());
        })
        .expect_err("aborts");
        assert!(
            matches!(error, ProjectConfigError::ObjectConflict(_)),
            "{error}"
        );
        assert_eq!(
            fs::read(&fixture.path).expect("reads"),
            reauthorized.as_bytes()
        );
        let head_after = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        assert_eq!(head_after.contents, head_before.contents);
        let (material, _) = fixture
            .connections
            .read_credential_material(&main_key())
            .expect("adopts the new authorization");
        assert_eq!(material.credentials["access"], "dex-web-new");
    }

    #[test]
    fn failed_write_back_returns_an_error_and_keeps_the_head() {
        let fixture = open(CONNECTIONS);
        let directory = fixture.path.parent().expect("directory").to_path_buf();
        let head_before = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        let revision = fixture
            .connections
            .read_connection(&main_key())
            .expect("reads")
            .revision;
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o500)).expect("read-only");
        let outcome =
            fixture
                .connections
                .replace_credential(&main_key(), revision, &refreshed_material());
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).expect("restores");
        let error = outcome.expect_err("write-back fails");
        assert!(matches!(error, ProjectConfigError::Io(_)), "{error}");
        assert!(error.is_retryable());
        assert_eq!(
            fs::read(&fixture.path).expect("reads"),
            CONNECTIONS.as_bytes()
        );
        let head_after = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        assert_eq!(head_after.contents, head_before.contents);
        let reopened = Arc::new(
            DexWebObjectStore::open(&fixture.path, Scope::new("local", "preview")).expect("opens"),
        );
        let reopened_objects: Arc<dyn ObjectStore> = reopened;
        let reopened_connections =
            ConnectionStore::new(reopened_objects, Scope::new("local", "preview"));
        for connections in [&fixture.connections, &reopened_connections] {
            let (material, _) = connections
                .read_credential_material(&main_key())
                .expect("reads");
            assert_eq!(material.credentials["access"], "old");
        }

        fixture
            .connections
            .replace_credential(&main_key(), revision, &refreshed_material())
            .expect("retry writes back");
        let (material, _) = fixture
            .connections
            .read_credential_material(&main_key())
            .expect("reads");
        assert_eq!(material.credentials["access"], "new");
        let written = String::from_utf8(fs::read(&fixture.path).expect("reads")).expect("UTF-8");
        assert!(written.contains("\"access\": \"new\""), "{written}");
    }

    #[test]
    fn failed_final_read_is_retryable_io_and_keeps_the_file_and_head() {
        let fixture = open(CONNECTIONS);
        let head_before = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        *lock(&fixture.store.final_read_failures) = Some((1, std::io::ErrorKind::Other));
        let error = refresh_with(&fixture, || {}).expect_err("unverified");
        assert!(matches!(error, ProjectConfigError::Io(_)), "{error:?}");
        assert!(error.is_retryable());
        assert_eq!(
            fs::read(&fixture.path).expect("reads"),
            CONNECTIONS.as_bytes()
        );
        let head_after = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        assert_eq!(head_after.contents, head_before.contents);
        refresh_with(&fixture, || {}).expect("a retry writes back");
        let written = String::from_utf8(fs::read(&fixture.path).expect("reads")).expect("UTF-8");
        assert!(written.contains("\"access\": \"new\""), "{written}");
    }

    /// Refreshes the `old` access token once, failing the publish's final read meanwhile.
    struct FinalReadFailingDriver {
        store: Arc<DexWebObjectStore>,
        exchanges: std::sync::atomic::AtomicUsize,
    }

    impl crate::CredentialRefreshDriver<JsonObject> for FinalReadFailingDriver {
        fn is_refresh_required(
            &self,
            state: &crate::CredentialRefreshState<'_, JsonObject>,
        ) -> bool {
            state.credentials["access"] == "old"
        }

        fn refresh(
            &self,
            _state: &crate::CredentialRefreshState<'_, JsonObject>,
        ) -> Result<crate::CredentialRefreshResult<JsonObject>, crate::CredentialError> {
            self.exchanges
                .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            // Runs after the write-back precheck, so only the publish's reads fail.
            *lock(&self.store.final_read_failures) = Some((usize::MAX, std::io::ErrorKind::Other));
            Ok(crate::CredentialRefreshResult {
                credentials: refreshed_material().credentials,
                expires_at: Rfc3339NanoTimestamp::from_system_time_utc(
                    std::time::SystemTime::now() + std::time::Duration::from_secs(3600),
                ),
            })
        }
    }

    #[test]
    fn failed_final_read_returns_io_and_recovery_publishes_once_it_clears() {
        let fixture = open(CONNECTIONS);
        let identity: super::super::provider::CredentialDecoder<JsonObject> =
            Arc::new(|members: &JsonObject| Ok(members.clone()));
        let provider = super::super::provider::RefreshingProjectCredentialProvider::new(
            fixture.connections.clone(),
            main_key(),
            identity,
            Arc::new(|credential: &JsonObject| Ok(credential.clone())),
            None,
        )
        .expect("provider");
        let call = crate::Call::new(
            crate::CallId::new("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
            crate::ConnectionRef::new("sample", "main"),
            crate::OperationRef::new("sample", "createItem"),
        )
        .expect("call");
        let driver = FinalReadFailingDriver {
            store: fixture.store.clone(),
            exchanges: std::sync::atomic::AtomicUsize::new(0),
        };
        let io_error = |error: &crate::CredentialError| {
            matches!(
                error.downcast_ref::<ProjectConfigError>(),
                Some(ProjectConfigError::Io(_))
            )
        };
        let error = crate::resolve_credential(&provider, &call, &driver).expect_err("unverified");
        assert!(io_error(&error), "{error:?}");
        // The recovery branch returns the persisting failure instead of looping.
        let error =
            crate::resolve_credential(&provider, &call, &driver).expect_err("still failing");
        assert!(io_error(&error), "{error:?}");
        assert_eq!(
            fs::read(&fixture.path).expect("reads"),
            CONNECTIONS.as_bytes()
        );
        *lock(&fixture.store.final_read_failures) = None;
        let credential = crate::resolve_credential(&provider, &call, &driver)
            .expect("publishes the held result");
        assert_eq!(credential["access"], "new");
        assert_eq!(
            driver.exchanges.load(std::sync::atomic::Ordering::SeqCst),
            1
        );
        let written = String::from_utf8(fs::read(&fixture.path).expect("reads")).expect("UTF-8");
        assert!(written.contains("\"refresh\": \"r1\""), "{written}");
    }

    #[test]
    fn duplicated_listing_aborts_publish_without_writing() {
        let fixture = open(CONNECTIONS);
        let revision = fixture
            .connections
            .read_connection(&main_key())
            .expect("reads")
            .revision;
        let head = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        let duplicated =
            CONNECTIONS.replace(r#""connectionName":"other""#, r#""connectionName":"main""#);
        write_private(&fixture.path, duplicated.as_bytes());
        let error = fixture
            .connections
            .replace_credential(&main_key(), revision, &refreshed_material())
            .expect_err("duplicated");
        assert!(
            matches!(error, ProjectConfigError::ConnectionNotInFile(_)),
            "{error}"
        );
        assert_eq!(
            fs::read(&fixture.path).expect("reads"),
            duplicated.as_bytes()
        );
        let head_after = fixture
            .store
            .memory
            .read_object(&fixture.head_key, "")
            .expect("head");
        assert_eq!(head_after.contents, head.contents);
    }
}
