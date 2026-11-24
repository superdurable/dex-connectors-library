// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Durable project credential refresh admission and recovery tests.

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Barrier, Mutex};
use std::time::{Duration, SystemTime};

use dex_connectors_sdk::projectconfig::{
    BrokerHttpClient, BrokerHttpResponse, Configuration, ConfigurationStore,
    ConnectionConfiguration, ConnectionKey, ConnectionStore, CredentialBrokerConfiguration,
    CredentialDecoder, CredentialEncoder, CredentialMaterial, InMemoryObjectStore, JsonObject,
    LoadedProject, ObjectStore, ProjectConfigError, ProjectConfigResult, ProjectCredentialProvider,
    RefreshingProjectCredentialProvider, Scope, go_canonical_json_bytes, load_dex_web_project,
    load_project_from_object_store, wire,
};
use dex_connectors_sdk::{
    Call, CallId, ConnectionRef, CredentialError, CredentialProvider, CredentialRefreshDriver,
    CredentialRefreshResult, CredentialRefreshState, OperationRef, ReauthorizationRequiredError,
    Rfc3339NanoTimestamp, is_reauthorization_required, resolve_credential,
    resolve_credential_after_rejection,
};
use serde_json::{Value, json};

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
        let field = |name: &str| {
            members
                .get(name)
                .and_then(Value::as_str)
                .map(str::to_string)
                .ok_or_else(|| "credential is incomplete".to_string())
        };
        Ok(BTreeMap::from([
            ("access".to_string(), field("access")?),
            ("refresh".to_string(), field("refresh")?),
        ]))
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

fn at_offset(seconds: i64) -> Rfc3339NanoTimestamp {
    let magnitude = Duration::from_secs(seconds.unsigned_abs());
    let instant = if seconds >= 0 {
        SystemTime::now() + magnitude
    } else {
        SystemTime::now() - magnitude
    };
    Rfc3339NanoTimestamp::from_system_time_utc(instant)
}

fn material(access: &str, expires_in_seconds: i64) -> CredentialMaterial {
    CredentialMaterial {
        credentials: json!({"access": access, "refresh": "kept"})
            .as_object()
            .expect("object")
            .clone(),
        expires_at: Some(at_offset(expires_in_seconds)),
        auth_method: "oauth2".into(),
    }
}

enum DriverOutcome {
    Refreshed,
    Retryable,
    Reauthorization,
}

/// Counts provider exchanges and optionally holds one until released.
struct RefreshDriver {
    calls: AtomicUsize,
    started: Option<Arc<Barrier>>,
    release: Option<Arc<Barrier>>,
    outcome: DriverOutcome,
}

impl RefreshDriver {
    fn new(outcome: DriverOutcome) -> Self {
        Self {
            calls: AtomicUsize::new(0),
            started: None,
            release: None,
            outcome,
        }
    }
}

impl CredentialRefreshDriver<Credential> for RefreshDriver {
    fn is_refresh_required(&self, state: &CredentialRefreshState<'_, Credential>) -> bool {
        state
            .expires_at
            .as_ref()
            .is_some_and(|expires_at| expires_at.unix_nanoseconds() <= state.now.unix_nanoseconds())
    }

    fn refresh(
        &self,
        state: &CredentialRefreshState<'_, Credential>,
    ) -> Result<CredentialRefreshResult<Credential>, CredentialError> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        if let Some(started) = &self.started {
            started.wait();
        }
        if let Some(release) = &self.release {
            release.wait();
        }
        match self.outcome {
            DriverOutcome::Refreshed => Ok(CredentialRefreshResult {
                credentials: BTreeMap::from([
                    ("access".to_string(), "new".to_string()),
                    ("refresh".to_string(), state.credentials["refresh"].clone()),
                ]),
                expires_at: at_offset(3600),
            }),
            DriverOutcome::Retryable => Err("inert provider detail".into()),
            DriverOutcome::Reauthorization => {
                Err(Box::new(ReauthorizationRequiredError::new(None)))
            }
        }
    }
}

fn provider(store: ConnectionStore) -> RefreshingProjectCredentialProvider<Credential> {
    RefreshingProjectCredentialProvider::new(store, key(), decode(), encode(), None)
        .expect("provider")
}

#[test]
fn refresh_admission_across_replicas() {
    let objects: Arc<dyn ObjectStore> = Arc::new(InMemoryObjectStore::new());
    let scope = Scope::new("test-project", "preview");
    let first_store = ConnectionStore::new(objects.clone(), scope.clone());
    let second_store = ConnectionStore::new(objects, scope);
    first_store
        .replace_credential(&key(), 0, &material("old", -60))
        .expect("seeds");
    let first = provider(first_store.clone());
    let second = provider(second_store);
    let started = Arc::new(Barrier::new(2));
    let release = Arc::new(Barrier::new(2));
    let driver = RefreshDriver {
        started: Some(started.clone()),
        release: Some(release.clone()),
        ..RefreshDriver::new(DriverOutcome::Refreshed)
    };
    let resolved = Mutex::new(Vec::new());
    std::thread::scope(|scope| {
        scope.spawn(|| {
            let value = resolve_credential(&first, &call(), &driver).expect("first resolves");
            resolved.lock().expect("lock").push(value);
        });
        started.wait();
        scope.spawn(|| {
            let value = resolve_credential(&second, &call(), &driver).expect("second resolves");
            resolved.lock().expect("lock").push(value);
        });
        release.wait();
    });
    let resolved = resolved.into_inner().expect("lock");
    assert_eq!(resolved.len(), 2);
    assert!(
        resolved
            .iter()
            .all(|value| value["access"] == "new" && value["refresh"] == "kept")
    );
    assert_eq!(driver.calls.load(Ordering::SeqCst), 1);
    assert_eq!(
        first_store.read_connection(&key()).expect("reads").status,
        "READY"
    );
    let (stored, _) = first_store.read_credential_material(&key()).expect("reads");
    assert_eq!(stored.credentials["refresh"], "kept");
}

fn authorize_record(store: &ConnectionStore, deadline_offset_seconds: i64) -> String {
    let path = key().path(store.scope()).expect("path");
    let result_key = format!("{path}/exchange-results/authorization-1");
    let mutation = json!({
        "kind": "AUTHORIZING", "invocationId": "invocation-1", "id": "authorization-1",
        "fence": 1, "baseRevision": 0,
        "deadline": at_offset(deadline_offset_seconds).as_wire_str(), "resultKey": result_key,
    });
    let record = wire::connection_record(
        store.scope(),
        &key(),
        1,
        1,
        "AUTHORIZING",
        None,
        "oauth2",
        None,
        Some(&mutation),
    )
    .expect("record");
    store
        .objects()
        .create_object(
            &format!("{path}/head"),
            &go_canonical_json_bytes(&record).expect("encodes"),
        )
        .expect("creates");
    result_key
}

fn is_project_error(error: &CredentialError, expected: fn(&ProjectConfigError) -> bool) -> bool {
    error
        .downcast_ref::<ProjectConfigError>()
        .is_some_and(expected)
}

#[test]
fn expired_authorization_recovers_result_or_fences_reauthorization() {
    let scope = Scope::new("test-project", "preview");
    let pending = ConnectionStore::new(Arc::new(InMemoryObjectStore::new()), scope.clone());
    authorize_record(&pending, 60);
    let error = resolve_credential(
        &provider(pending),
        &call(),
        &RefreshDriver::new(DriverOutcome::Refreshed),
    )
    .expect_err("pending");
    assert!(is_project_error(&error, |error| matches!(
        error,
        ProjectConfigError::ExchangePending
    )));

    let expired = ConnectionStore::new(Arc::new(InMemoryObjectStore::new()), scope.clone());
    authorize_record(&expired, -60);
    let error = resolve_credential(
        &provider(expired.clone()),
        &call(),
        &RefreshDriver::new(DriverOutcome::Refreshed),
    )
    .expect_err("expired");
    assert!(is_reauthorization_required(&*error));
    assert_eq!(
        expired.read_connection(&key()).expect("reads").status,
        "REAUTHORIZATION_REQUIRED"
    );

    let completed = ConnectionStore::new(Arc::new(InMemoryObjectStore::new()), scope.clone());
    let result_key = authorize_record(&completed, -60);
    let envelope = wire::credential_envelope(
        &scope,
        &key(),
        1,
        json!({"access": "authorized", "refresh": "kept"})
            .as_object()
            .expect("object"),
        Some(&at_offset(3600)),
        "oauth2",
        "authorization-1",
    );
    completed
        .objects()
        .create_object(
            &result_key,
            &go_canonical_json_bytes(&envelope).expect("encodes"),
        )
        .expect("creates");
    let resolved = resolve_credential(
        &provider(completed.clone()),
        &call(),
        &RefreshDriver::new(DriverOutcome::Refreshed),
    )
    .expect("recovers");
    assert_eq!(resolved["access"], "authorized");
    assert_eq!(
        completed.read_connection(&key()).expect("reads").status,
        "READY"
    );
}

#[test]
fn refresh_failure_restores_prior_and_terminal_reauth_fences() {
    let store = ConnectionStore::new(
        Arc::new(InMemoryObjectStore::new()),
        Scope::new("test-project", "preview"),
    );
    store
        .replace_credential(&key(), 0, &material("old", -60))
        .expect("seeds");
    let provider = provider(store.clone());
    let error = resolve_credential(
        &provider,
        &call(),
        &RefreshDriver::new(DriverOutcome::Retryable),
    )
    .expect_err("retryable");
    assert!(is_project_error(&error, |error| matches!(
        error,
        ProjectConfigError::RefreshFailed(_)
    )));
    assert!(!error.to_string().contains("inert provider detail"));
    assert_eq!(
        store.read_connection(&key()).expect("reads").status,
        "READY"
    );
    let terminal = RefreshDriver::new(DriverOutcome::Reauthorization);
    let error = resolve_credential(&provider, &call(), &terminal).expect_err("terminal");
    assert!(is_reauthorization_required(&*error));
    assert_eq!(
        store.read_connection(&key()).expect("reads").status,
        "REAUTHORIZATION_REQUIRED"
    );
    let error = resolve_credential(&provider, &call(), &terminal).expect_err("still terminal");
    assert!(is_reauthorization_required(&*error));
    assert_eq!(terminal.calls.load(Ordering::SeqCst), 1);
}

#[test]
fn rejection_requires_authoritative_expiry() {
    let store = ConnectionStore::new(
        Arc::new(InMemoryObjectStore::new()),
        Scope::new("test-project", "preview"),
    );
    store
        .replace_credential(&key(), 0, &material("valid", 3600))
        .expect("seeds");
    let driver = RefreshDriver::new(DriverOutcome::Refreshed);
    let error = resolve_credential_after_rejection(&provider(store), &call(), &driver)
        .expect_err("refused");
    assert!(is_project_error(&error, |error| {
        matches!(error, ProjectConfigError::RejectedCredentialNotRefreshed(_))
    }));
    assert_eq!(driver.calls.load(Ordering::SeqCst), 0);
}

/// Fails the test if a shared broker credential reaches the provider refresh path.
struct ForbiddenRefreshDriver;

impl CredentialRefreshDriver<Credential> for ForbiddenRefreshDriver {
    fn is_refresh_required(&self, _state: &CredentialRefreshState<'_, Credential>) -> bool {
        panic!("shared credential consulted refresh driver")
    }

    fn refresh(
        &self,
        _state: &CredentialRefreshState<'_, Credential>,
    ) -> Result<CredentialRefreshResult<Credential>, CredentialError> {
        panic!("shared credential dispatched refresh")
    }
}

fn decode_shared() -> CredentialDecoder<Credential> {
    Arc::new(|members: &JsonObject| {
        let access = members
            .get("access")
            .and_then(Value::as_str)
            .ok_or_else(|| "shared credential is incomplete".to_string())?;
        Ok(BTreeMap::from([("access".to_string(), access.to_string())]))
    })
}

type BrokerResponder = dyn Fn(&Value) -> BrokerHttpResponse + Send + Sync;

struct FakeBrokerHttp {
    requests: Mutex<Vec<Value>>,
    respond: Box<BrokerResponder>,
}

impl BrokerHttpClient for FakeBrokerHttp {
    fn post_json(
        &self,
        _url: &str,
        _token: &str,
        body: &[u8],
        _timeout: Duration,
    ) -> ProjectConfigResult<BrokerHttpResponse> {
        let request: Value = serde_json::from_slice(body).expect("request JSON");
        let mut requests = self.requests.lock().expect("lock");
        requests.push(request.clone());
        let numbered = json!({"count": requests.len(), "request": request});
        Ok((self.respond)(&numbered))
    }
}

fn load_shared_project(http: Arc<FakeBrokerHttp>) -> (ConnectionStore, LoadedProject) {
    let scope = Scope::new("test-project", "preview");
    let objects: Arc<dyn ObjectStore> = Arc::new(InMemoryObjectStore::new());
    let configurations = ConfigurationStore::new(objects.clone(), scope.clone()).expect("store");
    let token_reference = configurations
        .create_credential_broker_token("scope-token")
        .expect("token");
    let mut shared =
        ConnectionConfiguration::new("sample", "shared", "example", "sample", JsonObject::new());
    shared.shared_connection_id = "shared-7".into();
    let mut configuration = Configuration::new(
        scope.clone(),
        1,
        vec![
            ConnectionConfiguration::new("sample", "main", "example", "sample", JsonObject::new()),
            shared,
        ],
    );
    configuration.credential_broker = Some(CredentialBrokerConfiguration {
        endpoint: "https://broker.example/read".into(),
        token_secret_ref: token_reference,
    });
    let (written, _) = configurations
        .write_configuration(0, &configuration)
        .expect("writes");
    let snapshot = configurations
        .freeze_configuration(written.revision)
        .expect("freezes");
    let project =
        load_project_from_object_store(objects, scope, snapshot, Some(http), None).expect("loads");
    (project.connections().clone(), project)
}

fn shared_call() -> Call<'static> {
    Call::new(
        CallId::new("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
        ConnectionRef::new("sample", "shared"),
        OperationRef::new("sample", "createItem"),
    )
    .expect("call")
}

#[test]
fn shared_provider_routes_broker_without_refresh_or_stored_head() {
    let http = Arc::new(FakeBrokerHttp {
        requests: Mutex::new(Vec::new()),
        respond: Box::new(|numbered: &Value| {
            let count = numbered["count"].as_u64().expect("count");
            BrokerHttpResponse {
                status: 200,
                body: serde_json::to_vec(&json!({
                    "status": "READY", "credential": {"access": format!("shared-{count}")},
                    "revision": count, "expiresAt": at_offset(3600).as_wire_str(),
                }))
                .expect("JSON"),
            }
        }),
    });
    let (store, _project) = load_shared_project(http.clone());
    store
        .replace_credential(
            &key(),
            0,
            &CredentialMaterial {
                credentials: json!({"access": "ordinary", "refresh": "kept"})
                    .as_object()
                    .expect("object")
                    .clone(),
                expires_at: None,
                auth_method: "oauth2".into(),
            },
        )
        .expect("seeds");
    let ordinary =
        ProjectCredentialProvider::new(store.clone(), key(), decode(), None).expect("provider");
    assert_eq!(
        ordinary.resolve(&call()).expect("resolves")["access"],
        "ordinary"
    );
    let shared_key = ConnectionKey::new("sample", "shared");
    let refreshing = RefreshingProjectCredentialProvider::new(
        store.clone(),
        shared_key.clone(),
        decode(),
        encode(),
        Some(decode_shared()),
    )
    .expect("provider");
    let driver = ForbiddenRefreshDriver;
    assert_eq!(
        resolve_credential(&refreshing, &shared_call(), &driver).expect("resolves")["access"],
        "shared-1"
    );
    assert_eq!(
        resolve_credential_after_rejection(&refreshing, &shared_call(), &driver)
            .expect("refreshes")["access"],
        "shared-2"
    );
    assert_eq!(
        *http.requests.lock().expect("lock"),
        vec![
            json!({"sharedConnectionId": "shared-7"}),
            json!({"sharedConnectionId": "shared-7", "rejectedRevision": 1}),
        ]
    );
    let shared_decoding = ProjectCredentialProvider::new(
        store.clone(),
        shared_key.clone(),
        decode(),
        Some(decode_shared()),
    )
    .expect("provider");
    assert_eq!(
        shared_decoding.resolve(&shared_call()).expect("resolves")["access"],
        "shared-2"
    );
    let error = ProjectCredentialProvider::new(store, shared_key, decode(), None)
        .expect("provider")
        .resolve(&shared_call())
        .expect_err("decoding fails");
    assert_eq!(
        error.to_string(),
        "shared project credential decoding failed"
    );
}

#[test]
fn shared_provider_propagates_broker_status() {
    for (status, is_expected) in [
        (
            "REAUTHORIZATION_REQUIRED",
            (|error: &CredentialError| is_reauthorization_required(&**error))
                as fn(&CredentialError) -> bool,
        ),
        ("CONNECTION_NOT_SELECTED", |error: &CredentialError| {
            is_project_error(error, |error| {
                matches!(error, ProjectConfigError::ConnectionNotSelected)
            })
        }),
    ] {
        let http = Arc::new(FakeBrokerHttp {
            requests: Mutex::new(Vec::new()),
            respond: Box::new(move |_: &Value| BrokerHttpResponse {
                status: 200,
                body: serde_json::to_vec(&json!({"status": status})).expect("JSON"),
            }),
        });
        let (store, _project) = load_shared_project(http);
        let provider = RefreshingProjectCredentialProvider::new(
            store,
            ConnectionKey::new("sample", "shared"),
            decode(),
            encode(),
            Some(decode_shared()),
        )
        .expect("provider");
        let error = resolve_credential(&provider, &shared_call(), &ForbiddenRefreshDriver)
            .expect_err(status);
        assert!(is_expected(&error), "{status}: {error}");
    }
}

#[cfg(unix)]
#[test]
fn local_dex_web_newer_authorization_wins_refresh() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = directory.path().join("connections.json");
    write_local_connections(&path, "old", -60);
    let project = load_dex_web_project(&path).expect("loads");
    let provider = provider(project.connections().clone());
    let started = Arc::new(Barrier::new(2));
    let release = Arc::new(Barrier::new(2));
    let driver = RefreshDriver {
        started: Some(started.clone()),
        release: Some(release.clone()),
        ..RefreshDriver::new(DriverOutcome::Refreshed)
    };
    let outcome = std::thread::scope(|scope| {
        let pending = scope.spawn(|| resolve_credential(&provider, &call(), &driver).map(|_| ()));
        started.wait();
        write_local_connections(&path, "dex-web-new", 3600);
        release.wait();
        pending.join().expect("thread")
    });
    let error = outcome.expect_err("superseded");
    assert!(is_project_error(&error, |error| matches!(
        error,
        ProjectConfigError::ObjectConflict(_)
    )));
    let written: Value =
        serde_json::from_slice(&std::fs::read(&path).expect("reads")).expect("JSON");
    assert_eq!(
        written["connections"][0]["credentials"]["access"],
        "dex-web-new"
    );
    let (current, _) = project
        .connections()
        .read_credential_material(&key())
        .expect("reads");
    assert_eq!(current.credentials["access"], "dex-web-new");
}

#[cfg(unix)]
fn write_local_connections(path: &std::path::Path, access: &str, expires_in_seconds: i64) {
    use std::os::unix::fs::PermissionsExt;
    let document = json!({
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [{
            "connectorId": "sample", "connectionName": "main", "modulePath": "example",
            "provider": "sample", "configuration": {},
            "credentials": {"access": access, "refresh": "kept"},
            "credentialExpiresAt": at_offset(expires_in_seconds).as_wire_str(),
        }],
        "triggerBindings": [],
    });
    std::fs::write(path, serde_json::to_vec(&document).expect("JSON")).expect("writes");
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600)).expect("chmod");
}

#[cfg(unix)]
#[test]
fn local_dex_web_refresh_writes_back_and_restart_reads_it() {
    use std::os::unix::fs::PermissionsExt;
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = directory.path().join("connections.json");
    write_local_connections(&path, "old", -60);
    let project = load_dex_web_project(&path).expect("loads");
    let provider = provider(project.connections().clone());
    let resolved = resolve_credential(
        &provider,
        &call(),
        &RefreshDriver::new(DriverOutcome::Refreshed),
    )
    .expect("refreshes");
    assert_eq!(resolved["access"], "new");
    let written: Value =
        serde_json::from_slice(&std::fs::read(&path).expect("reads")).expect("JSON");
    assert_eq!(
        written["connections"][0]["credentials"],
        json!({"access": "new", "refresh": "kept"})
    );
    let mode = std::fs::metadata(&path)
        .expect("metadata")
        .permissions()
        .mode();
    assert_eq!(mode & 0o777, 0o600);
    let restarted = load_dex_web_project(&path).expect("reloads");
    let (stored, _) = restarted
        .connections()
        .read_credential_material(&key())
        .expect("reads");
    assert_eq!(stored.credentials["access"], "new");
}
