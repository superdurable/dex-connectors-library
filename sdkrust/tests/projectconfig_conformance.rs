// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Project snapshot, credential, local Dex Web, broker, and S3 conformance tests.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime};

use dex_connectors_sdk::Rfc3339NanoTimestamp;
use dex_connectors_sdk::projectconfig::{
    ApplicationSecretRef, BrokerHttpClient, BrokerHttpResponse, BrokerReadError, Configuration,
    ConfigurationStore, ConnectionConfiguration, ConnectionKey, ConnectionStore,
    CredentialBrokerClient, CredentialBrokerConfiguration, CredentialMaterial, EnvironmentValue,
    InMemoryObjectStore, JsonObject, ObjectStore, OperationConfiguration, ProjectConfigError,
    ProjectConfigResult, RawJsonObject, S3Client, S3Error, S3GetObjectOutput, S3ObjectStore,
    S3PutObjectInput, S3PutObjectOutput, Scope, SharedConnection, SnapshotRef,
    TriggerConfiguration, decode_broker_response, digest_bytes, go_canonical_json_bytes,
    load_dex_web_project, load_from_environment_values, load_project_from_object_store,
    validate_secret_reference, wire,
};
use serde_json::{Value, json};

fn conformance_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../conformance")
}

fn fixture(name: &str) -> Value {
    let bytes = std::fs::read(conformance_dir().join(name)).expect("fixture is readable");
    serde_json::from_slice(&bytes).expect("fixture is JSON")
}

fn text<'value>(value: &'value Value, name: &str) -> &'value str {
    value[name].as_str().expect("fixture member is a string")
}

/// Compares JSON values with numbers compared numerically, as the fixtures require.
fn assert_json_value_eq(actual: &Value, expected: &Value) {
    fn normalize(value: &Value) -> Value {
        match value {
            Value::Number(number) => json!(number.as_f64()),
            Value::Array(items) => Value::Array(items.iter().map(normalize).collect()),
            Value::Object(object) => Value::Object(
                object
                    .iter()
                    .map(|(key, value)| (key.clone(), normalize(value)))
                    .collect(),
            ),
            other => other.clone(),
        }
    }
    assert_eq!(normalize(actual), normalize(expected));
}

fn object(value: &Value) -> JsonObject {
    value.as_object().expect("JSON object").clone()
}

fn in_memory() -> Arc<dyn ObjectStore> {
    Arc::new(InMemoryObjectStore::new())
}

#[test]
fn scope_snapshot_reference_conformance() {
    let storage = fixture("project-storage.json");
    let scope = Scope::from_wire(&storage["scope"]).expect("scope decodes");
    assert_eq!(
        scope.prefix().expect("prefix"),
        "projects/sample-project/preview"
    );
    let snapshot = SnapshotRef::from_wire(&storage["snapshot"]).expect("snapshot decodes");
    snapshot.validate(&scope).expect("snapshot is valid");
    assert_json_value_eq(&snapshot.to_wire(), &storage["snapshot"]);
}

#[test]
fn go_canonical_snapshot_and_credential_bytes() {
    let fixture = fixture("project-storage.json");
    let configuration =
        Configuration::from_wire(&fixture["configurationSnapshotDocument"]).expect("decodes");
    let objects = in_memory();
    let configuration_store =
        ConfigurationStore::new(objects.clone(), configuration.scope.clone()).expect("store");
    let (_, written) = configuration_store
        .write_configuration(0, &configuration)
        .expect("writes");
    assert_eq!(
        String::from_utf8(written.contents.clone()).expect("UTF-8"),
        text(&fixture, "configurationSnapshotCanonicalJSON")
    );
    assert_eq!(
        digest_bytes(&written.contents),
        fixture["snapshot"]["digest"]
    );
    assert_json_value_eq(
        &configuration_store
            .freeze_configuration(1)
            .expect("freezes")
            .to_wire(),
        &fixture["snapshot"],
    );
    assert_eq!(
        go_canonical_json_bytes(
            &SnapshotRef::from_wire(&fixture["snapshot"])
                .expect("ref")
                .to_wire()
        )
        .expect("encodes"),
        go_canonical_json_bytes(&fixture["snapshot"]).expect("encodes")
    );
    let key = ConnectionKey::new(
        text(&fixture["connectionKey"], "connectorId"),
        text(&fixture["connectionKey"], "connectionName"),
    );
    assert_eq!(
        format!("{}/head", key.path(&configuration.scope).expect("path")),
        text(&fixture, "connectionHeadKey")
    );
    let envelope_fixture = &fixture["credentialEnvelope"];
    let expires_at =
        Rfc3339NanoTimestamp::from_wire(text(envelope_fixture, "expiresAt")).expect("timestamp");
    let connections = ConnectionStore::new(objects.clone(), configuration.scope.clone());
    connections
        .replace_credential(
            &key,
            0,
            &CredentialMaterial {
                credentials: object(&envelope_fixture["credentials"]),
                expires_at: Some(expires_at.clone()),
                auth_method: text(envelope_fixture, "authMethod").into(),
            },
        )
        .expect("replaces");
    let credential_key = text(&fixture, "credentialObjectKey");
    let credential = objects
        .read_object(credential_key, "")
        .expect("credential object");
    assert_eq!(
        String::from_utf8(credential.contents.clone()).expect("UTF-8"),
        text(&fixture, "credentialEnvelopeCanonicalJSON")
    );
    assert_eq!(
        &digest_bytes(&credential.contents)[7..],
        text(&fixture["objectReference"], "digest")
    );
    assert_eq!(credential.key, text(&fixture["objectReference"], "key"));
    let (loaded, _) = connections.read_credential_material(&key).expect("loads");
    assert_eq!(
        loaded.expires_at.expect("expiry").as_wire_str(),
        text(envelope_fixture, "expiresAt")
    );
    let exchange = wire::credential_envelope(
        &configuration.scope,
        &key,
        2,
        &object(&envelope_fixture["credentials"]),
        Some(&expires_at),
        text(envelope_fixture, "authMethod"),
        "attempt-7",
    );
    assert_eq!(
        String::from_utf8(go_canonical_json_bytes(&exchange).expect("encodes")).expect("UTF-8"),
        text(&fixture, "credentialExchangeEnvelopeCanonicalJSON")
    );
    let reversed = wire::credential_envelope(
        &configuration.scope,
        &key,
        2,
        &object(
            &json!({"refreshToken": "REDACTED_REFRESH_TOKEN", "accessToken": "REDACTED_ACCESS_TOKEN"}),
        ),
        Some(&expires_at),
        "oauth2",
        "",
    );
    let reversed_bytes =
        String::from_utf8(go_canonical_json_bytes(&reversed).expect("encodes")).expect("UTF-8");
    assert!(reversed_bytes.contains(r#""refreshToken":"REDACTED_REFRESH_TOKEN","accessToken""#));
    let record_fixture = &fixture["connectionRecord"];
    let record = wire::connection_record(
        &configuration.scope,
        &key,
        record_fixture["revision"].as_u64().expect("revision"),
        record_fixture["fence"].as_u64().expect("fence"),
        text(record_fixture, "status"),
        Some(&expires_at),
        text(record_fixture, "authMethod"),
        Some(&fixture["objectReference"]),
        Some(&fixture["mutation"]),
    )
    .expect("record encodes");
    let record_bytes = go_canonical_json_bytes(&record).expect("encodes");
    assert_eq!(
        String::from_utf8(record_bytes.clone()).expect("UTF-8"),
        text(&fixture, "connectionRecordCanonicalJSON")
    );
    let cas = &fixture["objectCAS"];
    assert_eq!(
        format!(
            "{}-{}",
            &digest_bytes(&record_bytes)[7..],
            text(cas, "version")
        ),
        text(cas, "etag")
    );
    assert_eq!(text(cas, "key"), text(&fixture, "connectionHeadKey"));
    let cas_objects = InMemoryObjectStore::new();
    let initial = cas_objects
        .create_object(text(&fixture, "connectionHeadKey"), b"{}")
        .expect("creates");
    let observed = cas_objects
        .compare_and_swap_object(
            text(&fixture, "connectionHeadKey"),
            &initial.etag,
            &record_bytes,
        )
        .expect("swaps");
    assert_eq!(observed.etag, text(cas, "etag"));
    let completed = &fixture["completedConnectionRecord"];
    let completed_record = wire::connection_record(
        &configuration.scope,
        &key,
        completed["revision"].as_u64().expect("revision"),
        completed["fence"].as_u64().expect("fence"),
        text(completed, "status"),
        Some(&expires_at),
        text(completed, "authMethod"),
        Some(&completed["credential"]),
        None,
    )
    .expect("record encodes");
    assert_json_value_eq(&completed_record, completed);
    assert_eq!(
        text(&completed["credential"], "key"),
        text(&fixture, "exchangeResultKey")
    );
    let secret = ApplicationSecretRef::from_wire(Some(&fixture["applicationSecret"])).expect("ref");
    validate_secret_reference(&secret, &configuration.scope, "DEX_CREDENTIAL_BROKER_TOKEN")
        .expect("valid secret reference");
    assert_eq!(
        go_canonical_json_bytes(&secret.to_wire()).expect("encodes"),
        go_canonical_json_bytes(&fixture["applicationSecret"]).expect("encodes")
    );
    assert_eq!(
        fixture["emptyTriggerInboxDocument"],
        json!({"schemaVersion": "connectors.dex.dev/project-trigger-inbox/v1alpha1", "events": []})
    );
    let inbox_bytes = go_canonical_json_bytes(&fixture["triggerInboxDocument"]).expect("encodes");
    let inbox: Value = serde_json::from_slice(&inbox_bytes).expect("JSON");
    assert_eq!(inbox, fixture["triggerInboxDocument"]);
    assert!(text(&fixture, "triggerInboxHeadKey").starts_with(&format!(
        "{}/trigger-inboxes/sample/",
        configuration.scope.prefix().expect("prefix")
    )));
}

#[test]
fn zero_offset_credential_write_uses_go_utc_bytes() {
    let scope = Scope::new("project-1", "preview");
    let objects = in_memory();
    let store = ConnectionStore::new(objects.clone(), scope.clone());
    for (name, offset) in [("positive", "+00:00"), ("negative", "-00:00")] {
        let key = ConnectionKey::new("sample", name);
        let supplied = Rfc3339NanoTimestamp::from_wire(&format!("2026-10-29T12:30:45.120{offset}"))
            .expect("time");
        let expected = Rfc3339NanoTimestamp::from_wire("2026-10-29T12:30:45.12Z").expect("time");
        let credentials = object(&json!({"token": "REDACTED"}));
        store
            .replace_credential(
                &key,
                0,
                &CredentialMaterial {
                    credentials: credentials.clone(),
                    expires_at: Some(supplied),
                    auth_method: "oauth2".into(),
                },
            )
            .expect("replaces");
        let envelope = go_canonical_json_bytes(&wire::credential_envelope(
            &scope,
            &key,
            1,
            &credentials,
            Some(&expected),
            "oauth2",
            "",
        ))
        .expect("encodes");
        let path = key.path(&scope).expect("path");
        let credential_key = format!("{path}/credentials/{}", &digest_bytes(&envelope)[7..]);
        assert_eq!(
            objects
                .read_object(&credential_key, "")
                .expect("object")
                .contents,
            envelope
        );
        let head: Value = serde_json::from_slice(
            &objects
                .read_object(&format!("{path}/head"), "")
                .expect("head")
                .contents,
        )
        .expect("JSON");
        assert_eq!(head["expiresAt"], "2026-10-29T12:30:45.12Z");
    }
}

#[test]
fn snapshot_cas_and_exact_version() {
    let scope = Scope::new("project-1", "preview");
    let store = ConfigurationStore::new(in_memory(), scope.clone()).expect("store");
    let document = Configuration::new(
        scope,
        1,
        vec![ConnectionConfiguration::new(
            "sample",
            "main",
            "module",
            "sample",
            JsonObject::new(),
        )],
    );
    let (first, _) = store.write_configuration(0, &document).expect("writes");
    let frozen = store.freeze_configuration(first.revision).expect("freezes");
    let (second, _) = store.write_configuration(1, &document).expect("writes");
    assert_eq!(second.revision, 2);
    assert_eq!(store.read_snapshot(&frozen).expect("reads").revision, 1);
    assert!(matches!(
        store.write_configuration(1, &document),
        Err(ProjectConfigError::ObjectConflict(_))
    ));
    let wrong = SnapshotRef::new(
        frozen.key.clone(),
        frozen.version.clone(),
        format!("sha256:{}", "0".repeat(64)),
    );
    assert!(matches!(
        store.read_snapshot(&wrong),
        Err(ProjectConfigError::Invalid(_))
    ));
}

#[test]
fn connection_private_material_and_revision() {
    let objects = in_memory();
    let scope = Scope::new("project-1", "live");
    let store = ConnectionStore::new(objects, scope.clone());
    let key = ConnectionKey::new("sample", "main / west");
    let material = |api_key: &str| CredentialMaterial {
        credentials: object(&json!({"apiKey": api_key})),
        expires_at: None,
        auth_method: "apiKey".into(),
    };
    let first = store
        .replace_credential(&key, 0, &material("inert"))
        .expect("first");
    assert_eq!(first.revision, 1);
    let (loaded, safe) = store.read_credential_material(&key).expect("reads");
    assert_eq!(loaded.credentials, object(&json!({"apiKey": "inert"})));
    assert!(!format!("{loaded:?}{safe:?}").contains("inert"));
    assert!(matches!(
        store.replace_credential(&key, 0, &loaded),
        Err(ProjectConfigError::ObjectConflict(_))
    ));
    let second = store
        .replace_credential(&key, 1, &material("new"))
        .expect("second");
    assert_eq!(second.fence, 2);
    assert!(
        key.path(&scope)
            .expect("path")
            .ends_with("/bWFpbiAvIHdlc3Q")
    );
}

#[test]
fn connection_rejects_foreign_private_reference() {
    let objects = in_memory();
    let scope = Scope::new("project-1", "preview");
    let store = ConnectionStore::new(objects.clone(), scope.clone());
    let key = ConnectionKey::new("sample", "main");
    store
        .replace_credential(
            &key,
            0,
            &CredentialMaterial {
                credentials: object(&json!({"apiKey": "inert"})),
                expires_at: None,
                auth_method: "apiKey".into(),
            },
        )
        .expect("replaces");
    let head_key = format!("{}/head", key.path(&scope).expect("path"));
    let head = objects.read_object(&head_key, "").expect("head");
    let mut record: Value = serde_json::from_slice(&head.contents).expect("JSON");
    record["credential"]["key"] =
        json!("projects/other/preview/connections/sample/foreign/credentials/secret");
    objects
        .compare_and_swap_object(
            &head_key,
            &head.etag,
            &serde_json::to_vec(&record).expect("JSON"),
        )
        .expect("tampers");
    assert!(matches!(
        store.read_credential_material(&key),
        Err(ProjectConfigError::Invalid(_))
    ));
}

#[cfg(unix)]
fn set_mode(path: &Path, mode: u32) {
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode)).expect("chmod");
}

#[cfg(unix)]
#[test]
fn local_connections_file_schema_and_permissions() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let target = directory.path().join("connections.json");
    std::fs::copy(conformance_dir().join("connections.json"), &target).expect("copies");
    set_mode(&target, 0o600);
    let project = load_dex_web_project(&target).expect("loads");
    let source = fixture("connections.json");
    let source_connections = source["connections"].as_array().expect("array");
    assert_eq!(
        project.configuration().connections.len(),
        source_connections.len()
    );
    let mut loaded_names: Vec<&str> = project
        .configuration()
        .connections
        .iter()
        .map(|entry| entry.connection_name.as_str())
        .collect();
    let mut source_names: Vec<&str> = source_connections
        .iter()
        .map(|entry| text(entry, "connectionName"))
        .collect();
    loaded_names.sort_unstable();
    source_names.sort_unstable();
    assert_eq!(loaded_names, source_names);
    assert!(project.configuration().trigger_bindings.is_empty());
    assert!(source_connections.last().expect("entry")["authMethodIds"].is_null());
    let resolve = |name: &str| -> String {
        let material = project
            .resolve_credential(&ConnectionKey::new("sample", name), "")
            .unwrap_or_else(|error| panic!("{name} resolves: {error}"));
        material["apiKey"].as_str().expect("apiKey").to_string()
    };
    assert_eq!(resolve("élément <&> 😀"), "REDACTED_UNICODE_KEY");
    assert_eq!(resolve("api"), "REDACTED_API_KEY");
    let mut updated: Value =
        serde_json::from_slice(&std::fs::read(&target).expect("reads")).expect("JSON");
    updated["connections"][0]["credentials"]["apiKey"] = json!("REDACTED_NEW_KEY");
    std::fs::write(&target, serde_json::to_vec(&updated).expect("JSON")).expect("writes");
    set_mode(&target, 0o600);
    assert_eq!(resolve("api"), "REDACTED_NEW_KEY");
    set_mode(&target, 0o644);
    assert!(load_dex_web_project(&target).is_err());
}

#[cfg(unix)]
#[test]
fn local_sidecar_and_hosted_selector_conflicts() {
    let directory = tempfile::tempdir().expect("temporary directory");
    let path = directory.path().join("connections.json");
    std::fs::write(
        &path,
        br#"{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[],"triggerBindings":[]}"#,
    )
    .expect("writes");
    set_mode(&path, 0o600);
    let sidecar = directory.path().join("use-configurations.json");
    std::fs::write(
        &sidecar,
        br#"{"schemaVersion":"connectors.dex.dev/local-use-configurations/v1alpha1","operationConfigurations":[]}"#,
    )
    .expect("writes");
    set_mode(&sidecar, 0o644);
    assert!(load_dex_web_project(&path).is_err());
    let conflicting = HashMap::from([
        (
            "DEX_CONNECTOR_CONFIG_FILE".to_string(),
            path.display().to_string(),
        ),
        ("DEX_PROJECT_ID".to_string(), "other".to_string()),
    ]);
    assert!(matches!(
        load_from_environment_values(&conflicting, None),
        Err(ProjectConfigError::Invalid(_))
    ));
    set_mode(&sidecar, 0o600);
    assert!(
        load_dex_web_project(&path)
            .expect("loads")
            .configuration()
            .connections
            .is_empty()
    );
}

#[test]
fn pinned_application_secret_and_reserved_names() {
    let scope = Scope::new("project-1", "preview");
    let objects = in_memory();
    let store = ConfigurationStore::new(objects.clone(), scope.clone()).expect("store");
    let secret_value = "inert<&>\u{2028}\u{2029}secret";
    let reference = store
        .create_application_secret("WEBHOOK_SECRET", secret_value)
        .expect("creates");
    let mut document = Configuration::new(scope.clone(), 1, Vec::new());
    document.environment.insert(
        "WEBHOOK_SECRET".into(),
        EnvironmentValue::SecretRef(reference.clone()),
    );
    let (written, _) = store.write_configuration(0, &document).expect("writes");
    let snapshot = store
        .freeze_configuration(written.revision)
        .expect("freezes");
    let project =
        load_project_from_object_store(objects.clone(), scope.clone(), snapshot, None, None)
            .expect("loads");
    assert_eq!(
        project.resolve_application_environment().expect("resolves"),
        HashMap::from([("WEBHOOK_SECRET".to_string(), secret_value.to_string())])
    );
    let private = objects
        .read_object(&reference.key, &reference.version)
        .expect("reads");
    assert!(
        String::from_utf8(private.contents.clone())
            .expect("UTF-8")
            .contains("\\u003c\\u0026\\u003e\\u2028\\u2029"),
        "secret object lacks Go escapes for {:?}",
        r"  "
    );
    assert_eq!(digest_bytes(&private.contents), reference.digest);
    assert!(
        store
            .create_application_secret("DEX_CONFIG_KEY", "bad")
            .is_err()
    );
    assert!(store.read_secret_value(&reference, "OTHER_SECRET").is_err());
    let mut foreign = Configuration::new(scope, 1, Vec::new());
    foreign.environment.insert(
        "WEBHOOK_SECRET".into(),
        EnvironmentValue::SecretRef(ApplicationSecretRef::new(
            reference.key.replace("project-1", "other"),
            reference.version.clone(),
            reference.digest.clone(),
        )),
    );
    assert!(foreign.validate().is_err());
}

#[test]
fn configuration_rejects_duplicate_operation_and_public_http_broker() {
    let scope = Scope::new("project-1", "preview");
    let entry =
        ConnectionConfiguration::new("sample", "main", "module", "sample", JsonObject::new());
    let operation = OperationConfiguration::from_wire(&json!({
        "connectorId": "sample", "connectionName": "main", "operationId": "getItem",
        "flowType": "ExampleFlow", "stepType": "GetItem", "configuration": {},
    }))
    .expect("operation");
    let mut duplicated = Configuration::new(scope.clone(), 1, vec![entry.clone()]);
    duplicated.operation_configurations = vec![operation.clone(), operation];
    assert!(duplicated.validate().is_err());
    let token = ApplicationSecretRef::new(
        format!(
            "{}/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/{}",
            scope.prefix().expect("prefix"),
            "0".repeat(32)
        ),
        "version-1",
        format!("sha256:{}", "0".repeat(64)),
    );
    let mut public_broker = Configuration::new(scope, 1, vec![entry]);
    public_broker.credential_broker = Some(CredentialBrokerConfiguration {
        endpoint: "http://public.example/read".into(),
        token_secret_ref: token,
    });
    assert!(public_broker.validate().is_err());
}

type BrokerHandler = dyn Fn(&str, &str, &Value) -> BrokerHttpResponse + Send + Sync;

struct FakeBrokerHttp {
    requests: Mutex<Vec<Value>>,
    request_bodies: Mutex<Vec<Vec<u8>>>,
    handler: Box<BrokerHandler>,
}

impl FakeBrokerHttp {
    fn new(
        handler: impl Fn(&str, &str, &Value) -> BrokerHttpResponse + Send + Sync + 'static,
    ) -> Arc<Self> {
        Arc::new(Self {
            requests: Mutex::new(Vec::new()),
            request_bodies: Mutex::new(Vec::new()),
            handler: Box::new(handler),
        })
    }

    fn requests(&self) -> Vec<Value> {
        self.requests.lock().expect("lock").clone()
    }

    /// Returns the exact request body bytes in send order.
    fn request_bodies(&self) -> Vec<Vec<u8>> {
        self.request_bodies.lock().expect("lock").clone()
    }
}

impl BrokerHttpClient for FakeBrokerHttp {
    fn post_json(
        &self,
        url: &str,
        token: &str,
        body: &[u8],
        _timeout: Duration,
    ) -> ProjectConfigResult<BrokerHttpResponse> {
        let request: Value = serde_json::from_slice(body).expect("request JSON");
        self.requests.lock().expect("lock").push(request.clone());
        self.request_bodies
            .lock()
            .expect("lock")
            .push(body.to_vec());
        Ok((self.handler)(url, token, &request))
    }
}

fn ok_json(value: &Value) -> BrokerHttpResponse {
    BrokerHttpResponse {
        status: 200,
        body: serde_json::to_vec(value).expect("JSON"),
    }
}

fn status(code: u16) -> BrokerHttpResponse {
    BrokerHttpResponse {
        status: code,
        body: Vec::new(),
    }
}

fn token_reference(version: &str) -> ApplicationSecretRef {
    ApplicationSecretRef::new("token", version, format!("sha256:{}", "0".repeat(64)))
}

fn broker_with(http: Arc<dyn BrokerHttpClient>) -> CredentialBrokerClient {
    CredentialBrokerClient::new(
        &CredentialBrokerConfiguration {
            endpoint: "https://broker.example/read".into(),
            token_secret_ref: token_reference("old"),
        },
        Arc::new(|reference: &ApplicationSecretRef| {
            Ok(if reference.version == "new" {
                "new"
            } else {
                "old"
            }
            .to_string())
        }),
        Arc::new(|| Ok(token_reference("new"))),
        http,
    )
    .expect("broker")
}

#[test]
fn broker_wire_renewal_and_revision() {
    let fixture = fixture("credential-broker.json");
    let ready = fixture["ready"].clone();
    let http = FakeBrokerHttp::new(move |_, token, _| {
        if token == "old" {
            return status(401);
        }
        let mut answer = ready.clone();
        let later = Rfc3339NanoTimestamp::from_system_time_utc(
            SystemTime::now() + Duration::from_secs(3600),
        );
        answer["expiresAt"] = json!(later.as_wire_str());
        ok_json(&answer)
    });
    let broker = Arc::new(broker_with(http.clone()));
    let shared = SharedConnection::new("shared-7", broker.clone()).expect("shared");
    assert_eq!(
        shared.resolve("call-1").expect("resolves")["accessToken"],
        "REDACTED_ACCESS_TOKEN"
    );
    assert_eq!(broker.read("shared-7", 3).expect("reads").revision, 4);
    assert!(matches!(
        shared.resolve_after_rejection("call-1"),
        Err(BrokerReadError::Project(
            ProjectConfigError::RejectedCredentialNotRefreshed(_)
        ))
    ));
    let requests = http.requests();
    let bodies = http.request_bodies();
    let first_request_bytes =
        go_canonical_json_bytes(&fixture["firstRequest"]).expect("fixture encodes");
    assert_eq!(first_request_bytes, br#"{"sharedConnectionId":"shared-7"}"#);
    // The first send and the resend after the 401 carry the same Go-encoded bytes.
    assert_eq!(bodies[0], first_request_bytes);
    assert_eq!(bodies[1], first_request_bytes);
    let refresh_request_bytes =
        go_canonical_json_bytes(&fixture["refreshRequest"]).expect("fixture encodes");
    assert!(bodies.contains(&refresh_request_bytes));
    assert_eq!(requests[0], fixture["firstRequest"]);
    assert!(requests.contains(&fixture["refreshRequest"]));
    assert_eq!(
        requests.last().expect("request"),
        &json!({"sharedConnectionId": "shared-7", "rejectedRevision": 4})
    );
    let decoded = decode_broker_response(&ok_json(&fixture["ready"])).expect("decodes");
    assert!(
        decoded
            .expires_at
            .expect("expiry")
            .as_wire_str()
            .ends_with("+08:00")
    );
    assert!(matches!(
        decode_broker_response(&ok_json(&fixture["reauthorizationRequired"])),
        Err(BrokerReadError::ReauthorizationRequired)
    ));
    assert!(matches!(
        decode_broker_response(&ok_json(&fixture["connectionNotSelected"])),
        Err(BrokerReadError::Project(
            ProjectConfigError::ConnectionNotSelected
        ))
    ));
    assert_eq!(
        fixture["refreshRequest"],
        json!({"sharedConnectionId": "shared-7", "rejectedRevision": 3})
    );
}

#[test]
fn relayed_trigger_registration_sends_go_escaped_bytes() {
    let http = FakeBrokerHttp::new(|_, token, _| {
        if token == "old" {
            return status(401);
        }
        ok_json(&json!({"status": "REGISTERED"}))
    });
    let broker = broker_with(http.clone());
    broker
        .register_relayed_trigger("shared-7", "élément <&>", "/relayed-triggers")
        .expect("registers");
    // sdkgo relayedTriggerRegistrationRequest field order with Go HTML escaping.
    let expected: &[u8] =
        "{\"sharedConnectionId\":\"shared-7\",\"connectionName\":\"élément \\u003c\\u0026\\u003e\"}"
            .as_bytes();
    assert_eq!(
        http.request_bodies(),
        vec![expected.to_vec(), expected.to_vec()]
    );

    // Plain serde bytes fit under 4096, but the Go-escaped bytes sent do not.
    let oversized = "<".repeat(1000);
    assert!(matches!(
        broker.register_relayed_trigger("shared-7", &oversized, "/relayed-triggers"),
        Err(ProjectConfigError::Invalid(_))
    ));
    assert_eq!(http.request_bodies().len(), 2);
}

#[test]
fn broker_http_response_debug_omits_body_bytes() {
    let response = ok_json(&json!({
        "status": "READY",
        "credential": {"accessToken": "REDACTED_ACCESS_TOKEN"},
        "revision": 4
    }));
    let rendered = format!("{response:?}");
    assert!(!rendered.contains("REDACTED_ACCESS_TOKEN"), "{rendered}");
    assert!(!rendered.contains("82, 69, 65"), "{rendered}");
    assert!(rendered.contains("status=200"), "{rendered}");
    assert!(
        rendered.contains(&format!("body_bytes={}", response.body.len())),
        "{rendered}"
    );
}

#[test]
fn broker_status_classification_and_invalid_answers() {
    type ErrorPredicate = fn(&ProjectConfigError) -> bool;
    let cases: Vec<(BrokerHttpResponse, ErrorPredicate)> = vec![
        (status(503), |error| {
            matches!(error, ProjectConfigError::BrokerUnavailable(_))
        }),
        (status(400), |error| {
            matches!(error, ProjectConfigError::BrokerRejected(_))
        }),
        (
            ok_json(&json!({"status": "READY", "credential": {}, "revision": 0})),
            |error| matches!(error, ProjectConfigError::BrokerRejected(_)),
        ),
        (
            ok_json(&json!({"status": "UNKNOWN", "secret": "inert"})),
            |error| matches!(error, ProjectConfigError::BrokerRejected(_)),
        ),
        (
            BrokerHttpResponse {
                status: 200,
                body: vec![b'x'; (1 << 20) + 1],
            },
            |error| matches!(error, ProjectConfigError::BrokerRejected(_)),
        ),
        (status(401), |error| {
            matches!(error, ProjectConfigError::BrokerRejected(_))
        }),
    ];
    for (response, is_expected) in cases {
        let http = FakeBrokerHttp::new(move |_, _, _| response.clone());
        let broker = CredentialBrokerClient::new(
            &CredentialBrokerConfiguration {
                endpoint: "https://broker.example/read".into(),
                token_secret_ref: token_reference("v1"),
            },
            Arc::new(|_: &ApplicationSecretRef| Ok("inert".to_string())),
            Arc::new(|| Ok(token_reference("v1"))),
            http,
        )
        .expect("broker");
        match broker.read("shared-7", 0) {
            Err(BrokerReadError::Project(error)) => assert!(is_expected(&error), "{error:?}"),
            other => panic!("unexpected broker outcome: {:?}", other.map(|_| ())),
        }
    }
}

type PutObjectResult = Result<S3PutObjectOutput, S3Error>;

#[derive(Default)]
struct FakeS3 {
    versioning: Mutex<String>,
    snapshot: Mutex<Option<S3GetObjectOutput>>,
    put_inputs: Mutex<Vec<S3PutObjectInput>>,
    put_result: Mutex<Option<PutObjectResult>>,
}

impl S3Client for FakeS3 {
    fn get_bucket_versioning(&self, _bucket: &str) -> Result<String, S3Error> {
        Ok(self.versioning.lock().expect("lock").clone())
    }

    fn get_object(
        &self,
        _bucket: &str,
        _key: &str,
        _version_id: &str,
        _maximum_bytes: usize,
    ) -> Result<S3GetObjectOutput, S3Error> {
        self.snapshot.lock().expect("lock").clone().ok_or(S3Error {
            http_status: Some(404),
        })
    }

    fn put_object(&self, input: S3PutObjectInput) -> Result<S3PutObjectOutput, S3Error> {
        self.put_inputs.lock().expect("lock").push(input);
        self.put_result
            .lock()
            .expect("lock")
            .clone()
            .unwrap_or_else(|| Ok(S3PutObjectOutput::default()))
    }
}

fn string_map(value: &Value) -> HashMap<String, String> {
    value
        .as_object()
        .expect("object")
        .iter()
        .map(|(key, value)| (key.clone(), value.as_str().expect("string").to_string()))
        .collect()
}

#[test]
fn go_project_environment_selectors() {
    let environment = fixture("project-environment.json");
    let storage = fixture("project-storage.json");
    assert_eq!(
        environment["credentialBrokerSecretName"],
        "DEX_CREDENTIAL_BROKER_TOKEN"
    );
    let hosted = string_map(&environment["hosted"]);
    let fake = Arc::new(FakeS3::default());
    *fake.versioning.lock().expect("lock") = "Enabled".into();
    *fake.snapshot.lock().expect("lock") = Some(S3GetObjectOutput {
        version_id: text(&storage["snapshot"], "version").into(),
        etag: "\"fixture-etag\"".into(),
        server_side_encryption: "aws:kms".into(),
        sse_kms_key_id: hosted["DEX_PROJECT_STORAGE_KMS_KEY_ARN"].clone(),
        body: text(&storage, "configurationSnapshotCanonicalJSON")
            .as_bytes()
            .to_vec(),
    });
    let project = load_from_environment_values(&hosted, Some(fake.clone())).expect("loads hosted");
    assert_json_value_eq(
        &project.configuration().to_wire().expect("encodes"),
        &storage["configurationSnapshotDocument"],
    );
    let mut mixed = hosted.clone();
    mixed.extend(string_map(&environment["localDexWeb"]));
    assert!(matches!(
        load_from_environment_values(&mixed, None),
        Err(ProjectConfigError::Invalid(_))
    ));
    let mut local = hosted.clone();
    local.extend(string_map(&environment["localObjectStoreOverrides"]));
    let local_project =
        load_from_environment_values(&local, Some(fake.clone())).expect("loads local");
    assert_eq!(
        local_project.configuration().scope,
        project.configuration().scope
    );
    let mut public = hosted;
    public.insert("DEX_PROJECT_ALLOW_LOCAL_STORAGE".into(), "true".into());
    public.insert(
        "DEX_PROJECT_STORAGE_ENDPOINT".into(),
        "https://public.example".into(),
    );
    assert!(matches!(
        load_from_environment_values(&public, Some(fake)),
        Err(ProjectConfigError::Invalid(_))
    ));
}

const KMS_KEY: &str = "arn:aws:kms:us-west-2:123456789012:key/example-key";

#[test]
fn s3_store_conditional_arguments_and_versioning() {
    let fake = Arc::new(FakeS3::default());
    *fake.versioning.lock().expect("lock") = "Enabled".into();
    *fake.put_result.lock().expect("lock") = Some(Ok(S3PutObjectOutput {
        version_id: "v1".into(),
        etag: "\"etag\"".into(),
    }));
    let store = S3ObjectStore::create(fake.clone(), "bucket", "", KMS_KEY, false).expect("store");
    store
        .create_object("projects/p1/live/configuration/head", b"{}")
        .expect("creates");
    store
        .compare_and_swap_object(
            "projects/p1/live/configuration/head",
            "\"etag\"",
            br#"{"x":1}"#,
        )
        .expect("swaps");
    let inputs = fake.put_inputs.lock().expect("lock").clone();
    assert_eq!(inputs[0].if_none_match, "*");
    assert_eq!(inputs[0].sse_kms_key_id, KMS_KEY);
    assert_eq!(inputs[1].if_match, "\"etag\"");
}

#[test]
fn s3_store_failure_classification_and_exact_read() {
    let fake = Arc::new(FakeS3::default());
    *fake.versioning.lock().expect("lock") = "Suspended".into();
    assert!(S3ObjectStore::create(fake.clone(), "bucket", "", "", true).is_err());
    *fake.versioning.lock().expect("lock") = "Enabled".into();
    let store = S3ObjectStore::create(fake.clone(), "bucket", "", KMS_KEY, false).expect("store");
    assert!(matches!(
        store.create_object("projects/p1/live/head", b"{}"),
        Err(ProjectConfigError::OutcomeUnknown(_))
    ));
    *fake.put_result.lock().expect("lock") = Some(Err(S3Error {
        http_status: Some(412),
    }));
    assert!(matches!(
        store.create_object("projects/p1/live/head", b"{}"),
        Err(ProjectConfigError::ObjectConflict(_))
    ));
    *fake.snapshot.lock().expect("lock") = Some(S3GetObjectOutput {
        version_id: "v1".into(),
        etag: "\"etag\"".into(),
        server_side_encryption: "AES256".into(),
        sse_kms_key_id: String::new(),
        body: b"{}".to_vec(),
    });
    assert!(matches!(
        store.read_object("projects/p1/live/head", "v1"),
        Err(ProjectConfigError::Invalid(_))
    ));
}

/// Go-written document with shuffled outer fields, spacing, and nested number lexemes.
const SHUFFLED_GO_CONFIGURATION: &str = r#"{"triggerBindings":[{"configuration":{"limit":1.50,"account":12345678901234567890123,"ratio":2.5E+3,"note":"a<b & c>d é \/"},"bindingName":"nightly","triggerName":"poll","connectionName":"main","connectorId":"sample"}],
 "revision":1,
 "operationConfigurations":[{"stepType":"Send","configuration":{ "limit" : 1.50 , "tiny" : -0.0e-0 , "list" : [ 1.0 , 2E2 ] },"flowType":"Order","operationId":"createItem","connectionName":"main","connectorId":"sample"}],
 "scope":{"kind":"preview","projectId":"project-1"},
 "connections":[{"configuration":{"region":"eu","maxRetries":10,"threshold":1e2},"provider":"sample","modulePath":"example.com/sample","connectionName":"main","connectorId":"sample"}],
 "schemaVersion":"connectors.dex.dev/project-configuration/v1alpha2"}
"#;

#[test]
fn rewritten_configuration_matches_go_raw_message_bytes_and_snapshot_digest() {
    // Derived by running sdkgo (Go 1.26.5, GOWORK=off) on SHUFFLED_GO_CONFIGURATION:
    // CreateObject, ReadConfiguration, WriteConfiguration(1), FreezeConfiguration(2).
    let expected = r#"{"schemaVersion":"connectors.dex.dev/project-configuration/v1alpha2","scope":{"projectId":"project-1","kind":"preview"},"revision":2,"connections":[{"connectorId":"sample","connectionName":"main","modulePath":"example.com/sample","provider":"sample","configuration":{"region":"eu","maxRetries":10,"threshold":1e2}}],"triggerBindings":[{"connectorId":"sample","connectionName":"main","triggerName":"poll","bindingName":"nightly","configuration":{"limit":1.50,"account":12345678901234567890123,"ratio":2.5E+3,"note":"a%LT%b %AMP% c%GT%d é \/"}}],"operationConfigurations":[{"connectorId":"sample","connectionName":"main","operationId":"createItem","flowType":"Order","stepType":"Send","configuration":{"limit":1.50,"tiny":-0.0e-0,"list":[1.0,2E2]}}]}"#;
    // Go writes `<`, `&`, and `>` as backslash-u escapes; spelled out to keep the source ASCII.
    let backslash = char::from(0x5c);
    let expected = expected
        .replace("%LT%", &format!("{backslash}u003c"))
        .replace("%AMP%", &format!("{backslash}u0026"))
        .replace("%GT%", &format!("{backslash}u003e"));
    let scope = Scope::new("project-1", "preview");
    let objects: Arc<dyn ObjectStore> = Arc::new(InMemoryObjectStore::new());
    objects
        .create_object(
            "projects/project-1/preview/configuration/head",
            SHUFFLED_GO_CONFIGURATION.as_bytes(),
        )
        .expect("creates");
    let configurations = ConfigurationStore::new(objects, scope).expect("store");
    let (document, _) = configurations.read_configuration().expect("reads");
    let (_, written) = configurations
        .write_configuration(1, &document)
        .expect("writes");
    assert_eq!(
        String::from_utf8(written.contents).expect("UTF-8"),
        expected
    );
    let snapshot = configurations.freeze_configuration(2).expect("freezes");
    assert_eq!(
        snapshot.digest,
        "sha256:2c9c5fc6a416ceef4982cc614a9e4447f2479b32309163e9cb8ba924279d8226"
    );
}

#[test]
fn changed_nested_settings_are_encoded_from_the_new_value() {
    let scope = Scope::new("project-1", "preview");
    let objects: Arc<dyn ObjectStore> = Arc::new(InMemoryObjectStore::new());
    objects
        .create_object(
            "projects/project-1/preview/configuration/head",
            SHUFFLED_GO_CONFIGURATION.as_bytes(),
        )
        .expect("creates");
    let configurations = ConfigurationStore::new(objects, scope).expect("store");
    let (mut document, _) = configurations.read_configuration().expect("reads");
    document.trigger_bindings[0].configuration =
        RawJsonObject::from_json_object(&object(&json!({"limit": 2})));
    let (_, written) = configurations
        .write_configuration(1, &document)
        .expect("writes");
    let written = String::from_utf8(written.contents).expect("UTF-8");
    assert!(written.contains(r#""bindingName":"nightly","configuration":{"limit":2}}"#));
    assert!(written.contains(r#""configuration":{"limit":1.50,"tiny":-0.0e-0,"list":[1.0,2E2]}"#));
}

#[test]
fn configuration_rejects_unknown_trigger_binding_fields_as_go_strict_decoding_does() {
    let binding = json!({
        "connectorId": "sample", "connectionName": "main", "triggerName": "poll",
        "bindingName": "nightly", "configuration": {}, "extra": true,
    });
    assert!(TriggerConfiguration::from_wire(&binding).is_err());
    let mut document = json!({
        "schemaVersion": "connectors.dex.dev/project-configuration/v1alpha2",
        "scope": {"projectId": "project-1", "kind": "preview"},
        "revision": 1,
        "connections": [{
            "connectorId": "sample", "connectionName": "main", "modulePath": "module",
            "provider": "sample", "configuration": {},
        }],
        "triggerBindings": [binding],
        "operationConfigurations": [],
    });
    assert!(Configuration::from_wire(&document).is_err());
    document["triggerBindings"][0]
        .as_object_mut()
        .expect("object")
        .remove("extra");
    assert!(Configuration::from_wire(&document).is_ok());
}

/// Settings Go keeps as `json.RawMessage`; `1e400` and the big integers exceed a `Value`.
const RAW_HOSTED_CONFIGURATION: &str = r#"{"schemaVersion":"connectors.dex.dev/project-configuration/v1alpha2","scope":{"projectId":"project-1","kind":"preview"},"revision":1,"connections":[{"connectorId":"sample","connectionName":"main","modulePath":"example.com/sample","provider":"sample","configuration":{ "huge": 1e400, "account": 12345678901234567890123, "note": "x" }},{"connectorId":"sample","connectionName":"other","modulePath":"example.com/sample","provider":"sample","configuration":{"ratio":2.5E+3,"tiny":-0.0e-0}}],"triggerBindings":[{"connectorId":"sample","connectionName":"main","triggerName":"poll","bindingName":"nightly","configuration":{"limit":1.50,"big":1e400}}],"operationConfigurations":[{"connectorId":"sample","connectionName":"main","operationId":"createItem","flowType":"Order","stepType":"Send","configuration":{"limit":1.50,"big":12345678901234567890123}},{"connectorId":"sample","connectionName":"other","operationId":"getItem","flowType":"Order","stepType":"Get","configuration":{"huge":-1E+400}}]}"#;

#[test]
fn swapped_and_edited_entries_keep_untouched_nested_bytes_like_go() {
    let scope = Scope::new("project-1", "preview");
    let objects: Arc<dyn ObjectStore> = Arc::new(InMemoryObjectStore::new());
    objects
        .create_object(
            "projects/project-1/preview/configuration/head",
            RAW_HOSTED_CONFIGURATION.as_bytes(),
        )
        .expect("creates");
    let configurations = ConfigurationStore::new(objects, scope).expect("store");
    let (mut document, _) = configurations.read_configuration().expect("reads 1e400");
    document.connections.swap(0, 1);
    document.operation_configurations.swap(0, 1);
    document.connections[0].module_path = "example.com/edited".into();
    let (_, written) = configurations
        .write_configuration(1, &document)
        .expect("writes");
    // Derived by running sdkgo (Go 1.26.5, GOWORK=off, under /tmp) on RAW_HOSTED_CONFIGURATION:
    // CreateObject, ReadConfiguration, the same swaps and edit, WriteConfiguration(1),
    // FreezeConfiguration(2).
    let expected = r#"{"schemaVersion":"connectors.dex.dev/project-configuration/v1alpha2","scope":{"projectId":"project-1","kind":"preview"},"revision":2,"connections":[{"connectorId":"sample","connectionName":"other","modulePath":"example.com/edited","provider":"sample","configuration":{"ratio":2.5E+3,"tiny":-0.0e-0}},{"connectorId":"sample","connectionName":"main","modulePath":"example.com/sample","provider":"sample","configuration":{"huge":1e400,"account":12345678901234567890123,"note":"x"}}],"triggerBindings":[{"connectorId":"sample","connectionName":"main","triggerName":"poll","bindingName":"nightly","configuration":{"limit":1.50,"big":1e400}}],"operationConfigurations":[{"connectorId":"sample","connectionName":"other","operationId":"getItem","flowType":"Order","stepType":"Get","configuration":{"huge":-1E+400}},{"connectorId":"sample","connectionName":"main","operationId":"createItem","flowType":"Order","stepType":"Send","configuration":{"limit":1.50,"big":12345678901234567890123}}]}"#;
    assert_eq!(
        String::from_utf8(written.contents).expect("UTF-8"),
        expected
    );
    let snapshot = configurations.freeze_configuration(2).expect("freezes");
    assert_eq!(
        snapshot.digest,
        "sha256:022afaed8e8567b6a1f15378b5a8d8896cc9301408d0ebde70dac2f88c1441fc"
    );
}

#[derive(serde::Deserialize)]
struct ExactSettings {
    account: u128,
}

#[derive(Debug, serde::Deserialize)]
struct FloatSettings {
    #[expect(dead_code, reason = "decoding into f64 is the behavior under test")]
    huge: f64,
}

#[test]
fn raw_settings_give_exact_typed_access_or_a_clear_range_error() {
    let settings =
        RawJsonObject::from_json_text(r#"{ "huge": 1e400, "account": 12345678901234567890123 }"#)
            .expect("keeps 1e400");
    assert_eq!(
        settings.as_json_text(),
        r#"{"huge":1e400,"account":12345678901234567890123}"#
    );
    let exact: ExactSettings = settings.decode().expect("decodes exactly");
    assert_eq!(exact.account, 12_345_678_901_234_567_890_123);
    let error = settings
        .decode::<FloatSettings>()
        .expect_err("1e400 is not an f64");
    assert!(error.to_string().contains("do not decode"), "{error}");
    let error = settings.to_json_object().expect_err("not exact in a Value");
    assert!(error.to_string().contains("64-bit"), "{error}");
    let float_only = RawJsonObject::from_json_text(r#"{"huge":1e400}"#).expect("keeps");
    let error = float_only.to_json_object().expect_err("not finite");
    assert!(error.to_string().contains("f64"), "{error}");
    let ordinary = RawJsonObject::from_json_text(r#"{"limit":1.50,"count":-9}"#).expect("keeps");
    assert_eq!(ordinary.to_json_object().expect("fits")["count"], json!(-9));
    assert!(RawJsonObject::from_json_text("[1]").is_err());
    assert!(RawJsonObject::from_json_text("{").is_err());
}

#[test]
fn raw_settings_reject_numbers_an_f64_would_distort() {
    for (text, expected) in [
        (r#"{"tiny":1e-400}"#, "rounds to zero"),
        (r#"{"big":9007199254740993.0}"#, "cannot represent exactly"),
        (r#"{"big":1e23}"#, "cannot represent exactly"),
        (r#"{"nested":{"tiny":-0.5e-330}}"#, "rounds to zero"),
    ] {
        let settings = RawJsonObject::from_json_text(text).expect("keeps the exact text");
        let error = settings.to_json_object().expect_err(text);
        assert!(error.to_string().contains(expected), "{text}: {error}");
    }
    // Rust prints the exact integer an f64 holds, which the integral check compares against.
    assert_eq!(format!("{:.0}", 1e23_f64), "99999999999999991611392");

    let ordinary = RawJsonObject::from_json_text(
        r#"{"ratio":0.1,"limit":1.50,"scaled":2.5E+3,"exact":9007199254740992.0,"power":1e22,"zero":-0.0e-0,"small":5e-324}"#,
    )
    .expect("keeps");
    let object = ordinary.to_json_object().expect("fits");
    assert_eq!(object["ratio"], json!(0.1));
    assert_eq!(object["limit"], json!(1.5));
    assert_eq!(object["scaled"], json!(2500.0));
    assert_eq!(object["exact"], json!(9_007_199_254_740_992.0));
    assert_eq!(object["power"], json!(1e22));

    let mut connection = ConnectionConfiguration::new(
        "sample",
        "main",
        "example.com/sample",
        "sample",
        JsonObject::new(),
    );
    connection.configuration = RawJsonObject::from_json_text(r#"{"tiny":1e-400}"#).expect("keeps");
    let error = connection
        .to_wire()
        .expect_err("to_wire keeps the rejection");
    assert!(error.to_string().contains("rounds to zero"), "{error}");
}

#[cfg(unix)]
#[test]
fn dex_web_load_keeps_nested_bytes_across_swaps_like_go() {
    // Each digest derived by running sdkgo LoadDexWebConnectionsFile (Go 1.26.5, GOWORK=off,
    // under /tmp) on the same files; the second swaps connections and edits `other`'s modulePath.
    let original = r#"{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{"connectorId":"sample","connectionName":"main","modulePath":"example.com/sample","provider":"sample","authMethodId":"oauth2","configuration":{"huge": 1e400, "account": 12345678901234567890123},"credentials":{"access":"old","refresh":"r0"},"credentialExpiresAt":"2030-01-01T00:00:00Z","dexWebOnly":{"keep": 1.50}},{"connectorId":"sample","connectionName":"other","modulePath":"example.com/sample","provider":"sample","authMethodId":"apiKey","configuration":{"ratio":2.5E+3, "tiny":-0.0e-0},"credentials":{"apiKey":"k"}}],"triggerBindings":[{"connectorId":"sample","connectionName":"main","triggerName":"poll","bindingName":"nightly","configuration":{"limit":1.50,"big":1e400}}]}"#;
    let swapped = r#"{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{"connectorId":"sample","connectionName":"other","modulePath":"example.com/edited","provider":"sample","authMethodId":"apiKey","configuration":{"ratio":2.5E+3, "tiny":-0.0e-0},"credentials":{"apiKey":"k"}},{"connectorId":"sample","connectionName":"main","modulePath":"example.com/sample","provider":"sample","authMethodId":"oauth2","configuration":{"huge": 1e400, "account": 12345678901234567890123},"credentials":{"access":"old","refresh":"r0"},"credentialExpiresAt":"2030-01-01T00:00:00Z","dexWebOnly":{"keep": 1.50}}],"triggerBindings":[{"connectorId":"sample","connectionName":"main","triggerName":"poll","bindingName":"nightly","configuration":{"limit":1.50,"big":1e400}}]}"#;
    let uses = r#"{"schemaVersion":"connectors.dex.dev/local-use-configurations/v1alpha1","operationConfigurations":[{"connectorId":"sample","connectionName":"main","operationId":"createItem","flowType":"Order","stepType":"Send","configuration":{"limit":1.50,"big":12345678901234567890123}}]}"#;
    let cases = [
        (
            original,
            "sha256:1203059a245597c309a961d33086283be01279eb98abd4e3edf013b1c127f28e",
        ),
        (
            swapped,
            "sha256:a1fbeb02926aa26f41721c2ad552ab0e57ff3c6ceb386a75e61fdf0bdc50064e",
        ),
    ];
    for (connections, digest) in cases {
        let directory = tempfile::tempdir().expect("temporary directory");
        let path = directory.path().join("connections.json");
        std::fs::write(&path, connections).expect("writes");
        set_mode(&path, 0o600);
        let uses_path = directory.path().join("use-configurations.json");
        std::fs::write(&uses_path, uses).expect("writes");
        set_mode(&uses_path, 0o600);
        let project = load_dex_web_project(&path).expect("loads 1e400");
        assert_eq!(project.snapshot().digest, digest);
        let main = project
            .connection_configuration(&ConnectionKey::new("sample", "main"))
            .expect("main");
        assert_eq!(
            main.configuration.as_json_text(),
            r#"{"huge":1e400,"account":12345678901234567890123}"#
        );
        assert_eq!(
            project.configuration().trigger_bindings[0]
                .configuration
                .as_json_text(),
            r#"{"limit":1.50,"big":1e400}"#
        );
        assert_eq!(
            project.configuration().operation_configurations[0]
                .configuration
                .as_json_text(),
            r#"{"limit":1.50,"big":12345678901234567890123}"#
        );
    }
}
