// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Dex Web connections file refresh lock, write-back precheck, deletion, and ETag tests.
#![cfg(unix)]

use std::collections::BTreeMap;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::mpsc::{Receiver, Sender, channel};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime};

use base64::Engine;
use dex_connectors_sdk::projectconfig::{
    ConnectionKey, ConnectionStore, CredentialDecoder, CredentialEncoder, CredentialMaterial,
    DexWebObjectStore, JsonObject, MAXIMUM_OBJECT_BYTES, ObjectStore, ProjectConfigError,
    ProjectObject, RefreshingProjectCredentialProvider, Scope, load_dex_web_project,
};
use dex_connectors_sdk::{
    Call, CallId, ConnectionRef, CredentialError, CredentialProvider, CredentialRefreshDriver,
    CredentialRefreshResult, CredentialRefreshState, OperationRef, Rfc3339NanoTimestamp,
    resolve_credential,
};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};

type Credential = BTreeMap<String, String>;

fn key() -> ConnectionKey {
    ConnectionKey::new("sample", "main")
}

fn call() -> Call<'static> {
    Call::new(
        CallId::new("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
        ConnectionRef::new("sample", "main"),
        OperationRef::new("sample", "createItem"),
    )
    .expect("call")
}

fn decode() -> CredentialDecoder<Credential> {
    Arc::new(|members: &JsonObject| {
        members
            .iter()
            .map(|(name, value)| {
                value
                    .as_str()
                    .map(|text| (name.clone(), text.to_string()))
                    .ok_or_else(|| "credential member is not a string".to_string())
            })
            .collect()
    })
}

fn encode() -> CredentialEncoder<Credential> {
    Arc::new(|credential: &Credential| {
        Ok(credential
            .iter()
            .map(|(name, value)| (name.clone(), Value::String(value.clone())))
            .collect())
    })
}

fn provider(store: ConnectionStore) -> RefreshingProjectCredentialProvider<Credential> {
    RefreshingProjectCredentialProvider::new(store, key(), decode(), encode(), None)
        .expect("provider")
}

fn in_one_hour() -> Rfc3339NanoTimestamp {
    Rfc3339NanoTimestamp::from_system_time_utc(SystemTime::now() + Duration::from_secs(3600))
}

fn write_private(path: &Path, contents: &[u8]) {
    std::fs::write(path, contents).expect("writes");
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600)).expect("chmod");
}

/// Writes `connections.json` listing the sample connection once per credential pair.
fn write_connections(path: &Path, credentials: &[(&str, &str)]) {
    let connections: Vec<Value> = credentials
        .iter()
        .map(|(access, refresh)| {
            json!({
                "connectorId": "sample", "connectionName": "main", "modulePath": "example",
                "provider": "sample", "configuration": {},
                "credentials": {"access": access, "refresh": refresh},
                "credentialExpiresAt": in_one_hour().as_wire_str(),
            })
        })
        .collect();
    let document = json!({
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": connections,
        "triggerBindings": [],
    });
    write_private(path, &serde_json::to_vec(&document).expect("JSON"));
}

fn connections_path(directory: &tempfile::TempDir) -> PathBuf {
    directory.path().join("connections.json")
}

fn file_credential(path: &Path) -> Value {
    let written: Value =
        serde_json::from_slice(&std::fs::read(path).expect("reads")).expect("JSON");
    written["connections"][0]["credentials"].clone()
}

fn project_error(error: &CredentialError) -> &ProjectConfigError {
    error
        .downcast_ref::<ProjectConfigError>()
        .unwrap_or_else(|| panic!("not a project error: {error}"))
}

/// Rotating provider: each refresh token works once, and a reuse is counted, not honored.
struct RotatingProvider {
    current_refresh_token: Mutex<String>,
    exchanges: AtomicUsize,
    reused_tokens: AtomicUsize,
}

impl RotatingProvider {
    fn new(refresh_token: &str) -> Arc<Self> {
        Arc::new(Self {
            current_refresh_token: Mutex::new(refresh_token.into()),
            exchanges: AtomicUsize::new(0),
            reused_tokens: AtomicUsize::new(0),
        })
    }

    fn exchange(&self, refresh_token: &str) -> Result<Credential, CredentialError> {
        self.exchanges.fetch_add(1, Ordering::SeqCst);
        let mut current = self.current_refresh_token.lock().expect("lock");
        if *current != refresh_token {
            self.reused_tokens.fetch_add(1, Ordering::SeqCst);
            return Err("refresh token reuse detected".into());
        }
        let generation = self.exchanges.load(Ordering::SeqCst);
        *current = format!("refresh-{generation}");
        Ok(BTreeMap::from([
            ("access".to_string(), format!("access-{generation}")),
            ("refresh".to_string(), current.clone()),
        ]))
    }
}

/// Refreshes the `old` access token through a [`RotatingProvider`], with optional test hooks.
struct RotatingDriver {
    provider: Arc<RotatingProvider>,
    on_old_evaluated: Option<Mutex<Sender<()>>>,
    on_exchange_started: Option<Mutex<Sender<()>>>,
    release_exchange: Option<Mutex<Receiver<()>>>,
    before_exchange: Option<Box<dyn Fn() + Send + Sync>>,
}

impl RotatingDriver {
    fn new(provider: Arc<RotatingProvider>) -> Self {
        Self {
            provider,
            on_old_evaluated: None,
            on_exchange_started: None,
            release_exchange: None,
            before_exchange: None,
        }
    }
}

impl CredentialRefreshDriver<Credential> for RotatingDriver {
    fn is_refresh_required(&self, state: &CredentialRefreshState<'_, Credential>) -> bool {
        let is_old = state.credentials["access"] == "old";
        if is_old && let Some(sender) = &self.on_old_evaluated {
            // The receiver reads only the first signal; later sends may fail harmlessly.
            if sender.lock().expect("lock").send(()).is_err() {
                return true;
            }
        }
        is_old
    }

    fn refresh(
        &self,
        state: &CredentialRefreshState<'_, Credential>,
    ) -> Result<CredentialRefreshResult<Credential>, CredentialError> {
        if let Some(started) = &self.on_exchange_started {
            started.lock().expect("lock").send(()).expect("signals");
        }
        if let Some(release) = &self.release_exchange {
            release.lock().expect("lock").recv().expect("released");
        }
        if let Some(before_exchange) = &self.before_exchange {
            before_exchange();
        }
        Ok(CredentialRefreshResult {
            credentials: self.provider.exchange(&state.credentials["refresh"])?,
            expires_at: in_one_hour(),
        })
    }
}

#[test]
fn two_processes_exchange_a_rotating_refresh_token_once() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    // Each load is an independent store, as in a separate process.
    let first_project = load_dex_web_project(&path).expect("first loads");
    let second_project = load_dex_web_project(&path).expect("second loads");
    let first = provider(first_project.connections().clone());
    let second = provider(second_project.connections().clone());
    let rotating = RotatingProvider::new("refresh-0");
    let (started_sender, started) = channel();
    let (release, release_receiver) = channel();
    let first_driver = RotatingDriver {
        on_exchange_started: Some(Mutex::new(started_sender)),
        release_exchange: Some(Mutex::new(release_receiver)),
        ..RotatingDriver::new(rotating.clone())
    };
    let (evaluated_sender, second_evaluated_old) = channel();
    let second_driver = RotatingDriver {
        on_old_evaluated: Some(Mutex::new(evaluated_sender)),
        ..RotatingDriver::new(rotating.clone())
    };
    let (first_result, second_result) = std::thread::scope(|scope| {
        let first_thread = scope.spawn(|| resolve_credential(&first, &call(), &first_driver));
        started
            .recv()
            .expect("first exchange starts while holding the lock");
        let second_thread = scope.spawn(|| resolve_credential(&second, &call(), &second_driver));
        second_evaluated_old
            .recv()
            .expect("second sees the old credential before the lock");
        release.send(()).expect("releases first");
        (
            first_thread.join().expect("first thread"),
            second_thread.join().expect("second thread"),
        )
    });
    let first_credential = first_result.expect("first resolves");
    let second_credential = second_result.expect("second adopts the write-back");
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 1);
    assert_eq!(rotating.reused_tokens.load(Ordering::SeqCst), 0);
    assert_eq!(first_credential["refresh"], "refresh-1");
    assert_eq!(second_credential, first_credential);
    assert_eq!(
        file_credential(&path),
        json!({"access": "access-1", "refresh": "refresh-1"})
    );
    for project in [&first_project, &second_project] {
        let (material, _) = project
            .connections()
            .read_credential_material(&key())
            .expect("reads");
        assert_eq!(material.credentials["refresh"], "refresh-1");
    }
    let lock_metadata =
        std::fs::metadata(directory.path().join("connections.json.lock")).expect("lock file");
    assert_eq!(lock_metadata.permissions().mode() & 0o777, 0o600);
}

#[test]
fn unwritable_directory_refuses_refresh_before_any_provider_request() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    // The lock file exists, so only the write-back precheck can fail.
    write_private(&directory.path().join("connections.json.lock"), b"");
    let project = load_dex_web_project(&path).expect("loads");
    let provider = provider(project.connections().clone());
    let rotating = RotatingProvider::new("refresh-0");
    let driver = RotatingDriver::new(rotating.clone());
    std::fs::set_permissions(directory.path(), std::fs::Permissions::from_mode(0o500))
        .expect("makes the directory read-only");
    let outcome = resolve_credential(&provider, &call(), &driver);
    std::fs::set_permissions(directory.path(), std::fs::Permissions::from_mode(0o700))
        .expect("restores the directory");
    let error = outcome.expect_err("write-back precheck fails");
    let error = project_error(&error);
    assert!(
        matches!(error, ProjectConfigError::RefreshFailed(_)),
        "{error:?}"
    );
    assert!(error.is_retryable());
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 0);
    assert_eq!(
        file_credential(&path),
        json!({"access": "old", "refresh": "refresh-0"})
    );
}

fn open_connections(path: &Path) -> (Arc<DexWebObjectStore>, ConnectionStore) {
    let scope = Scope::new("local", "preview");
    let store = Arc::new(DexWebObjectStore::open(path, scope.clone()).expect("opens"));
    let objects: Arc<dyn ObjectStore> = store.clone();
    (store, ConnectionStore::new(objects, scope))
}

fn head_key() -> String {
    format!(
        "{}/head",
        key().path(&Scope::new("local", "preview")).expect("path")
    )
}

#[test]
fn late_write_back_failure_holds_the_exchange_result_and_a_retry_persists_it() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    let file_before = std::fs::read(&path).expect("reads");
    let (store, connections) = open_connections(&path);
    let provider = provider(connections.clone());
    let rotating = RotatingProvider::new("refresh-0");
    let read_only_directory = directory.path().to_path_buf();
    // Runs after the write-back precheck, as Go's readOnlyDuringRefreshDriver does.
    let driver = RotatingDriver {
        before_exchange: Some(Box::new(move || {
            std::fs::set_permissions(&read_only_directory, std::fs::Permissions::from_mode(0o500))
                .expect("makes the directory read-only");
        })),
        ..RotatingDriver::new(rotating.clone())
    };
    let outcome = resolve_credential(&provider, &call(), &driver);
    std::fs::set_permissions(directory.path(), std::fs::Permissions::from_mode(0o700))
        .expect("restores the directory");
    let error = outcome.expect_err("late write-back fails");
    let error = project_error(&error);
    assert!(matches!(error, ProjectConfigError::Io(_)), "{error:?}");
    assert!(error.is_retryable());
    assert!(!error.to_string().contains("refresh-1"), "{error}");
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 1);
    assert_eq!(std::fs::read(&path).expect("reads"), file_before);
    let head = store.read_object(&head_key(), "").expect("head");
    assert!(
        String::from_utf8_lossy(&head.contents).contains("\"REFRESHING\""),
        "the exchange result stays unpublished"
    );
    let (_, reopened) = open_connections(&path);
    let (material, _) = reopened.read_credential_material(&key()).expect("reads");
    assert_eq!(material.credentials["refresh"], "refresh-0");

    let retried = resolve_credential(&provider, &call(), &RotatingDriver::new(rotating.clone()))
        .expect("retry publishes the held result");
    assert_eq!(retried["refresh"], "refresh-1");
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 1);
    assert_eq!(rotating.reused_tokens.load(Ordering::SeqCst), 0);
    assert_eq!(
        file_credential(&path),
        json!({"access": "access-1", "refresh": "refresh-1"})
    );
    let (_, reopened) = open_connections(&path);
    let (material, _) = reopened.read_credential_material(&key()).expect("reads");
    assert_eq!(material.credentials["refresh"], "refresh-1");
}

/// Writes `connections.json` listing the sample connection once under `authorization_id`.
fn write_authorized_connection(path: &Path, expires_at: &str, authorization_id: Option<&str>) {
    let mut connection = json!({
        "connectorId": "sample", "connectionName": "main", "modulePath": "example",
        "provider": "sample", "configuration": {},
        "credentials": {"access": "old", "refresh": "refresh-0"},
        "credentialExpiresAt": expires_at,
    });
    if let Some(authorization_id) = authorization_id {
        connection["authorizationId"] = json!(authorization_id);
    }
    let document = json!({
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [connection],
        "triggerBindings": [],
    });
    write_private(path, &serde_json::to_vec(&document).expect("JSON"));
}

/// A held exchange result of a deleted authorization must never reach its recreation.
fn assert_recreated_connection_never_receives_the_held_result(is_deletion_observed: bool) {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    let expires_at = in_one_hour();
    write_authorized_connection(&path, expires_at.as_wire_str(), Some("authorization-1"));
    let (_, connections) = open_connections(&path);
    let provider = provider(connections);
    let rotating = RotatingProvider::new("refresh-0");
    let read_only_directory = directory.path().to_path_buf();
    let driver = RotatingDriver {
        before_exchange: Some(Box::new(move || {
            std::fs::set_permissions(&read_only_directory, std::fs::Permissions::from_mode(0o500))
                .expect("makes the directory read-only");
        })),
        ..RotatingDriver::new(rotating.clone())
    };
    let outcome = resolve_credential(&provider, &call(), &driver);
    std::fs::set_permissions(directory.path(), std::fs::Permissions::from_mode(0o700))
        .expect("restores the directory");
    let error = outcome.expect_err("write-back fails");
    assert!(
        matches!(project_error(&error), ProjectConfigError::Io(_)),
        "{error:?}"
    );
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 1);

    if is_deletion_observed {
        write_connections(&path, &[]);
        let error = provider.resolve(&call()).expect_err("deleted");
        assert!(matches!(
            project_error(&error),
            ProjectConfigError::ConnectionNotInFile(_)
        ));
    }
    write_authorized_connection(&path, expires_at.as_wire_str(), Some("authorization-2"));

    // The recreated record holds the old token, so a fresh exchange is the only valid path;
    // the rotating provider then reports reuse instead of minting a token.
    let error = resolve_credential(&provider, &call(), &RotatingDriver::new(rotating.clone()))
        .expect_err("refresh-0 was already rotated");
    assert!(
        matches!(project_error(&error), ProjectConfigError::RefreshFailed(_)),
        "{error:?}"
    );
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 2);
    assert_eq!(rotating.reused_tokens.load(Ordering::SeqCst), 1);
    assert_eq!(
        file_credential(&path),
        json!({"access": "old", "refresh": "refresh-0"})
    );
    let written: Value =
        serde_json::from_slice(&std::fs::read(&path).expect("reads")).expect("JSON");
    assert_eq!(
        written["connections"][0]["authorizationId"],
        "authorization-2"
    );
}

#[test]
fn connection_recreated_after_observed_deletion_never_receives_the_held_result() {
    assert_recreated_connection_never_receives_the_held_result(true);
}

#[test]
fn connection_reauthorized_in_place_never_receives_the_held_result() {
    assert_recreated_connection_never_receives_the_held_result(false);
}

/// Delegates to a Dex Web store but pads every connection head swap past the object limit.
struct PaddingHeadStore {
    inner: Arc<DexWebObjectStore>,
}

impl ObjectStore for PaddingHeadStore {
    fn read_object(&self, key: &str, version: &str) -> Result<ProjectObject, ProjectConfigError> {
        self.inner.read_object(key, version)
    }

    fn create_object(
        &self,
        key: &str,
        contents: &[u8],
    ) -> Result<ProjectObject, ProjectConfigError> {
        self.inner.create_object(key, contents)
    }

    fn compare_and_swap_object(
        &self,
        key: &str,
        expected_etag: &str,
        contents: &[u8],
    ) -> Result<ProjectObject, ProjectConfigError> {
        let mut padded = contents.to_vec();
        if key.ends_with("/head") {
            padded.resize(MAXIMUM_OBJECT_BYTES + 1, b' ');
        }
        self.inner
            .compare_and_swap_object(key, expected_etag, &padded)
    }
}

#[test]
fn oversized_head_swap_leaves_the_file_and_head_unchanged() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    let file_before = std::fs::read(&path).expect("reads");
    let (store, _) = open_connections(&path);
    let head_before = store.read_object(&head_key(), "").expect("head");
    let padding: Arc<dyn ObjectStore> = Arc::new(PaddingHeadStore {
        inner: store.clone(),
    });
    let connections = ConnectionStore::new(padding, Scope::new("local", "preview"));
    let revision = connections.read_connection(&key()).expect("reads").revision;
    let mut credentials = JsonObject::new();
    credentials.insert("access".into(), json!("new"));
    credentials.insert("refresh".into(), json!("refresh-1"));
    let material = CredentialMaterial {
        credentials,
        expires_at: Some(in_one_hour()),
        auth_method: "local".into(),
    };
    let error = connections
        .replace_credential(&key(), revision, &material)
        .expect_err("oversized");
    assert!(matches!(error, ProjectConfigError::Invalid(_)), "{error:?}");
    assert_eq!(std::fs::read(&path).expect("reads"), file_before);
    let head_after = store.read_object(&head_key(), "").expect("head");
    assert_eq!(head_after.contents, head_before.contents);
}

#[test]
fn deleted_connection_is_not_resolved_or_refreshed() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    let project = load_dex_web_project(&path).expect("loads");
    let provider = provider(project.connections().clone());
    let rotating = RotatingProvider::new("refresh-0");
    let driver = RotatingDriver::new(rotating.clone());
    provider.resolve(&call()).expect("resolves while listed");
    write_connections(&path, &[]);
    let error = provider.resolve(&call()).expect_err("deleted");
    assert!(matches!(
        project_error(&error),
        ProjectConfigError::ConnectionNotInFile(_)
    ));
    let error = resolve_credential(&provider, &call(), &driver).expect_err("deleted");
    assert!(matches!(
        project_error(&error),
        ProjectConfigError::RefreshFailed(_)
    ));
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 0);
}

#[test]
fn duplicated_connection_is_not_resolved() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    let project = load_dex_web_project(&path).expect("loads");
    let provider = provider(project.connections().clone());
    write_connections(&path, &[("old", "refresh-0"), ("other", "refresh-9")]);
    let error = provider.resolve(&call()).expect_err("duplicated");
    assert!(matches!(
        project_error(&error),
        ProjectConfigError::ConnectionNotInFile(_)
    ));
}

#[test]
fn connection_deleted_during_exchange_is_not_published() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    let project = load_dex_web_project(&path).expect("loads");
    let provider = provider(project.connections().clone());
    let rotating = RotatingProvider::new("refresh-0");
    let deleted_path = path.clone();
    let driver = RotatingDriver {
        before_exchange: Some(Box::new(move || write_connections(&deleted_path, &[]))),
        ..RotatingDriver::new(rotating.clone())
    };
    let error = resolve_credential(&provider, &call(), &driver).expect_err("not published");
    assert!(matches!(
        project_error(&error),
        ProjectConfigError::ConnectionNotInFile(_)
    ));
    assert_eq!(rotating.exchanges.load(Ordering::SeqCst), 1);
    assert!(provider.resolve(&call()).is_err());
    let written: Value =
        serde_json::from_slice(&std::fs::read(&path).expect("reads")).expect("JSON");
    assert_eq!(written["connections"], json!([]));
}

#[test]
fn head_compare_and_swap_rejects_a_deleted_connection() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = connections_path(&directory);
    write_connections(&path, &[("old", "refresh-0")]);
    let scope = Scope::new("local", "preview");
    let store = DexWebObjectStore::open(&path, scope.clone()).expect("opens");
    let head_key = format!("{}/head", key().path(&scope).expect("path"));
    let head = store.read_object(&head_key, "").expect("reads head");
    write_connections(&path, &[]);
    let error = store
        .compare_and_swap_object(&head_key, &head.etag, &head.contents)
        .expect_err("deleted");
    assert!(matches!(error, ProjectConfigError::ConnectionNotInFile(_)));
}

fn sha256_hex(contents: &[u8]) -> String {
    Sha256::digest(contents)
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

/// Writes one Trigger inbox object exactly as Go's `versionedStore.persist` does.
fn write_inbox_object(directory: &Path, key: &str, etag: &str, contents: &[u8]) {
    let inboxes = directory.join("trigger-inboxes");
    std::fs::create_dir_all(&inboxes).expect("creates");
    std::fs::set_permissions(&inboxes, std::fs::Permissions::from_mode(0o700)).expect("chmod");
    let encoded = base64::engine::general_purpose::STANDARD.encode(contents);
    let stored =
        format!(r#"{{"key":"{key}","version":"1","etag":"{etag}","contents":"{encoded}"}}"#);
    write_private(
        &inboxes.join(format!("{}-1.json", sha256_hex(key.as_bytes()))),
        stored.as_bytes(),
    );
}

#[test]
fn trigger_inbox_objects_use_go_etags_and_accept_earlier_rust_etags() {
    let inbox_key = "projects/local/preview/trigger-inboxes/sample/main/poll/binding/head";
    let contents =
        br#"{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":[]}"#;
    let go_etag = format!("{}-1", sha256_hex(contents));
    for stored_etag in [go_etag.clone(), format!("sha256:{go_etag}")] {
        let directory = tempfile::tempdir().expect("temporary directory");
        let path = connections_path(&directory);
        write_connections(&path, &[("old", "refresh-0")]);
        write_inbox_object(directory.path(), inbox_key, &stored_etag, contents);
        let store = DexWebObjectStore::open(&path, Scope::new("local", "preview")).expect("opens");
        let loaded = store.read_object(inbox_key, "").expect("reads");
        assert_eq!(loaded.etag, stored_etag);
        assert_eq!(loaded.contents, contents);
        let replacement = br#"{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":[{}]}"#;
        let written = store
            .compare_and_swap_object(inbox_key, &stored_etag, replacement)
            .expect("swaps");
        assert_eq!(written.etag, format!("{}-2", sha256_hex(replacement)));
        let file = directory
            .path()
            .join("trigger-inboxes")
            .join(format!("{}-2.json", sha256_hex(inbox_key.as_bytes())));
        let persisted: Value =
            serde_json::from_slice(&std::fs::read(file).expect("reads")).expect("JSON");
        assert_eq!(persisted["etag"], written.etag.as_str());
        assert!(
            !directory
                .path()
                .join("trigger-inboxes")
                .join(format!("{}-1.json", sha256_hex(inbox_key.as_bytes())))
                .exists()
        );
    }
}
