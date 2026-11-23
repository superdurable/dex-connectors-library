// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Manifest refresh contracts against fake and local provider token endpoints.

use std::collections::BTreeMap;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use dex_connectors_sdk::oauthtoken::{ManifestCredential, ManifestCredentialRefreshDriver};
use dex_connectors_sdk::providerhttp::{
    AddressPolicy, AddressRefusal, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest,
    ProviderHttpResponse, ProviderHttpTransport, ResponseHeaders,
};
use dex_connectors_sdk::{
    CredentialError, CredentialRefreshDriver, CredentialRefreshResult, CredentialRefreshState,
    ReauthorizationRequiredError, RetryError, Rfc3339NanoTimestamp, SecretString, ValidationError,
};
use serde_json::{Value as JsonValue, json};

#[derive(Clone, Debug)]
struct Credentials {
    access_token: SecretString,
    refresh_token: SecretString,
    client_id: String,
    client_secret: SecretString,
    auth_method_id: String,
    instance_url: String,
    service_account_key: SecretString,
    delegated_user: String,
    primary_email: String,
}

impl Credentials {
    fn new(access_token: &str, refresh_token: &str) -> Self {
        Self {
            access_token: SecretString::new(access_token),
            refresh_token: SecretString::new(refresh_token),
            client_id: "client-id".to_string(),
            client_secret: SecretString::new("client-secret"),
            auth_method_id: "oauth".to_string(),
            instance_url: "old-instance".to_string(),
            service_account_key: SecretString::new(""),
            delegated_user: String::new(),
            primary_email: String::new(),
        }
    }
}

impl ManifestCredential for Credentials {
    fn credential_field_names() -> &'static [&'static str] {
        &[
            "access_token",
            "refresh_token",
            "client_id",
            "client_secret",
            "instance_url",
            "service_account_key",
            "delegated_user",
            "primary_email",
        ]
    }

    fn credential_value(&self, name: &str) -> String {
        match name {
            "access_token" => self.access_token.reveal().to_string(),
            "refresh_token" => self.refresh_token.reveal().to_string(),
            "client_id" => self.client_id.clone(),
            "client_secret" => self.client_secret.reveal().to_string(),
            "instance_url" => self.instance_url.clone(),
            "service_account_key" => self.service_account_key.reveal().to_string(),
            "delegated_user" => self.delegated_user.clone(),
            "primary_email" => self.primary_email.clone(),
            _ => String::new(),
        }
    }

    fn with_credential_value(&self, name: &str, value: String) -> Self {
        let mut updated = self.clone();
        match name {
            "access_token" => updated.access_token = SecretString::new(value),
            "refresh_token" => updated.refresh_token = SecretString::new(value),
            "client_id" => updated.client_id = value,
            "client_secret" => updated.client_secret = SecretString::new(value),
            "instance_url" => updated.instance_url = value,
            "service_account_key" => updated.service_account_key = SecretString::new(value),
            "delegated_user" => updated.delegated_user = value,
            "primary_email" => updated.primary_email = value,
            _ => {}
        }
        updated
    }

    fn auth_method_id(&self) -> Option<&str> {
        Some(&self.auth_method_id)
    }
}

fn auth() -> JsonValue {
    json!({
        "defaultMethod": "oauth",
        "methods": [
            {
                "id": "oauth",
                "type": "oauth2",
                "fields": [
                    {"name": "access_token", "type": "secretString"},
                    {"name": "refresh_token", "type": "secretString"},
                    {"name": "client_id", "type": "string"},
                    {"name": "client_secret", "type": "secretString"},
                    {"name": "instance_url", "type": "url"}
                ],
                "oauth2": {
                    "tokenEndpoint": "https://oauth.example.test/token",
                    "tokenEndpointAuthMethod": "client_secret_basic",
                    "tokenRequestEncoding": "json",
                    "clientIDCredential": "client_id",
                    "clientSecretCredential": "client_secret",
                    "scopes": ["offline_access", "files.read"],
                    "credentialMappings": [
                        {"credential": "access_token", "source": "access_token"},
                        {"credential": "refresh_token", "source": "refresh_token"},
                        {"credential": "instance_url", "source": "instance_url"}
                    ],
                    "refresh": {
                        "terminalErrorCodes": ["invalid_grant", "invalid_client"],
                        "additionalParameters": {"scope": "offline_access files.read"}
                    }
                }
            },
            {"id": "static", "type": "apiKey", "fields": []}
        ]
    })
}

struct ScriptedTransport {
    response: Mutex<(u16, JsonValue)>,
    requests: Mutex<Vec<ProviderHttpRequest>>,
}

impl ProviderHttpTransport for ScriptedTransport {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        _timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        self.requests
            .lock()
            .expect("requests lock")
            .push(request.clone());
        let (status, body) = self.response.lock().expect("response lock").clone();
        Ok(ProviderHttpResponse::new(
            status,
            ResponseHeaders::default(),
            Box::new(std::io::Cursor::new(body.to_string().into_bytes())),
        ))
    }
}

fn scripted(status: u16, body: JsonValue) -> (Arc<ScriptedTransport>, ProviderHttpClient) {
    let transport = Arc::new(ScriptedTransport {
        response: Mutex::new((status, body)),
        requests: Mutex::new(Vec::new()),
    });
    let client = ProviderHttpClient::with_transport(transport.clone(), Duration::from_secs(5))
        .expect("client");
    (transport, client)
}

fn driver(
    auth: &JsonValue,
    client: ProviderHttpClient,
) -> ManifestCredentialRefreshDriver<Credentials> {
    ManifestCredentialRefreshDriver::new("Example", auth, client, BTreeMap::new())
        .expect("valid manifest")
}

fn state(credentials: &Credentials) -> CredentialRefreshState<'_, Credentials> {
    CredentialRefreshState {
        credentials,
        expires_at: None,
        now: Rfc3339NanoTimestamp::now_utc(),
    }
}

fn refresh(
    driver: &ManifestCredentialRefreshDriver<Credentials>,
    credentials: &Credentials,
) -> Result<CredentialRefreshResult<Credentials>, CredentialError> {
    driver.refresh(&state(credentials))
}

/// `expect_err` without requiring `Debug` on the credential result.
trait RefreshErrorExpectation {
    fn refresh_error(self, message: &str) -> CredentialError;
}

impl RefreshErrorExpectation for Result<CredentialRefreshResult<Credentials>, CredentialError> {
    fn refresh_error(self, message: &str) -> CredentialError {
        match self {
            Ok(_) => panic!("expected a refresh error: {message}"),
            Err(error) => error,
        }
    }
}

fn token_body() -> JsonValue {
    json!({
        "access_token": "next-access",
        "refresh_token": "next-refresh",
        "expires_in": 3600,
        "token_type": "Bearer",
        "scope": "files.read",
        "instance_url": "new-instance"
    })
}

#[test]
fn manifest_refresh_rotates_and_keeps_complete_material() {
    let (transport, client) = scripted(200, token_body());
    let driver = driver(&auth(), client);
    let credentials = Credentials::new("old-access", "old-refresh");
    let now = Rfc3339NanoTimestamp::now_utc();
    let refresh_state = CredentialRefreshState {
        credentials: &credentials,
        expires_at: Some(now.clone()),
        now: now.clone(),
    };
    assert!(driver.is_refresh_required(&refresh_state));
    let result = driver.refresh(&refresh_state).expect("refreshed");
    assert_eq!(result.credentials.access_token.reveal(), "next-access");
    assert_eq!(result.credentials.refresh_token.reveal(), "next-refresh");
    assert_eq!(result.credentials.client_secret.reveal(), "client-secret");
    assert_eq!(result.credentials.instance_url, "new-instance");
    let lifetime = result.expires_at.unix_nanoseconds() - now.unix_nanoseconds();
    assert!((59 * 60 * 1_000_000_000..61 * 60 * 1_000_000_000).contains(&lifetime));
    let requests = transport.requests.lock().expect("requests");
    assert_eq!(requests.len(), 1);
    assert_eq!(requests[0].url, "https://oauth.example.test/token");
    let header = |name: &str| {
        requests[0]
            .headers
            .iter()
            .find(|(candidate, _)| candidate.eq_ignore_ascii_case(name))
            .map(|(_, value)| value.clone())
            .unwrap_or_default()
    };
    assert_eq!(header("content-type"), "application/json");
    assert!(header("authorization").starts_with("Basic "));
    let body: JsonValue = serde_json::from_slice(&requests[0].body).expect("json body");
    assert_eq!(body["grant_type"], "refresh_token");
    assert_eq!(body["scope"], "offline_access files.read");
}

#[test]
fn manifest_refresh_preserves_unreturned_refresh_token_and_classifies_errors() {
    let (transport, client) = scripted(
        200,
        json!({"access_token": "next-access", "expires_in": 3600, "token_type": "Bearer", "scope": "files.read"}),
    );
    let driver = driver(&auth(), client);
    let credentials = Credentials::new("old", "prior");
    let result = refresh(&driver, &credentials).expect("refreshed");
    assert_eq!(result.credentials.refresh_token.reveal(), "prior");
    *transport.response.lock().expect("response") = (400, json!({"error": "invalid_grant"}));
    let terminal = refresh(&driver, &credentials).refresh_error("terminal");
    assert!(terminal.is::<ReauthorizationRequiredError>());
    *transport.response.lock().expect("response") = (400, json!({"error": "invalid_scope"}));
    let retry = refresh(&driver, &credentials).refresh_error("retry");
    assert!(retry.is::<RetryError>());
    *transport.response.lock().expect("response") = (500, json!({"error": "invalid_grant"}));
    assert!(
        refresh(&driver, &credentials)
            .refresh_error("5xx")
            .is::<RetryError>()
    );
}

#[test]
fn reported_scope_must_cover_required_scopes() {
    let (_transport, client) = scripted(
        200,
        json!({"access_token": "a", "expires_in": 3600, "token_type": "Bearer", "scope": "other"}),
    );
    let driver = driver(&auth(), client);
    let error = refresh(&driver, &Credentials::new("old", "prior")).refresh_error("wrong scope");
    assert!(error.is::<ReauthorizationRequiredError>());
}

#[test]
fn manifest_static_method_never_refreshes() {
    let (transport, client) = scripted(500, json!({}));
    let driver = driver(&auth(), client);
    let mut credentials = Credentials::new("old", "prior");
    credentials.auth_method_id = "static".to_string();
    let now = Rfc3339NanoTimestamp::now_utc();
    let expired = Rfc3339NanoTimestamp::from_wire("2000-01-01T00:00:00Z").expect("timestamp");
    let refresh_state = CredentialRefreshState {
        credentials: &credentials,
        expires_at: Some(expired),
        now,
    };
    assert!(!driver.is_refresh_required(&refresh_state));
    assert!(
        driver
            .refresh(&refresh_state)
            .refresh_error("no grant")
            .is::<ReauthorizationRequiredError>()
    );
    assert!(transport.requests.lock().expect("requests").is_empty());
}

#[test]
fn manifest_template_uses_go_path_segment_escaping() {
    let mut auth = auth();
    auth["methods"][0]["oauth2"]["tokenEndpoint"] =
        json!({"template": "https://oauth.example.test/{client_id}/token"});
    let (transport, client) = scripted(
        200,
        json!({"access_token": "next", "expires_in": 3600, "token_type": "Bearer"}),
    );
    let driver = driver(&auth, client);
    let mut credentials = Credentials::new("old", "prior");
    credentials.client_id = "a:b@c&d=e+f$/g;h,i?j".to_string();
    refresh(&driver, &credentials).expect("refreshed");
    assert_eq!(
        transport.requests.lock().expect("requests")[0].url,
        "https://oauth.example.test/a:b@c&d=e+f$%2Fg%3Bh%2Ci%3Fj/token"
    );
}

#[test]
fn invalid_declarations_fail_at_construction() {
    let (_transport, client) = scripted(200, json!({}));
    let mut invalid_timeout = auth();
    invalid_timeout["methods"][0]["oauth2"]["refresh"]["tokenExchangeTimeout"] = json!("-1s");
    let error = ManifestCredentialRefreshDriver::<Credentials>::new(
        "Example",
        &invalid_timeout,
        client.clone(),
        BTreeMap::new(),
    )
    .err()
    .expect("negative timeout");
    assert!(error.message().contains("tokenExchangeTimeout"));
    for duration in ["1h1s", "3000000000ms", "5000000000ms"] {
        let mut oversized = auth();
        oversized["methods"][0]["oauth2"]["refresh"]["tokenExchangeTimeout"] = json!(duration);
        assert!(
            ManifestCredentialRefreshDriver::<Credentials>::new(
                "Example",
                &oversized,
                client.clone(),
                BTreeMap::new()
            )
            .is_err(),
            "{duration}"
        );
    }
    let mut stored = auth();
    stored["methods"][0]["oauth2"]["refresh"]["storedMissingExpiry"] = json!("sometimes");
    assert!(
        ManifestCredentialRefreshDriver::<Credentials>::new(
            "Example",
            &stored,
            client.clone(),
            BTreeMap::new()
        )
        .is_err()
    );
    let mut sets = auth();
    sets["methods"][0]["oauth2"]["refresh"]["acceptedScopeSets"] = json!([[]]);
    let rejected: Result<_, ValidationError> = ManifestCredentialRefreshDriver::<Credentials>::new(
        "Example",
        &sets,
        client,
        BTreeMap::new(),
    );
    assert!(rejected.is_err());
}

#[test]
fn private_network_token_endpoint_uses_declared_exchange_deadline() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind loopback");
    let port = listener.local_addr().expect("address").port();
    thread::spawn(move || {
        for mut stream in listener.incoming().take(2).flatten() {
            let mut buffer = [0_u8; 4096];
            let _ignored = stream.read(&mut buffer);
            thread::sleep(Duration::from_millis(300));
            let body = r#"{"access_token":"new","expires_in":3600,"token_type":"Bearer","scope":"files.read"}"#;
            let _ignored = write!(
                stream,
                "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
        }
    });
    let mut auth = auth();
    let oauth = &mut auth["methods"][0]["oauth2"];
    oauth["tokenEndpoint"] = json!(format!("http://127.0.0.1:{port}/token"));
    oauth["tokenEndpointNetworkPolicy"] = json!("privateNetworkHTTP");
    oauth["refresh"]["tokenExchangeTimeout"] = json!({"fromSetting": "requestTimeout"});
    let client = ProviderHttpClient::new(Duration::from_secs(5), AddressPolicy::PublicHttps)
        .expect("client");
    let settings = BTreeMap::from([("requestTimeout".to_string(), json!(50_000_000))]);
    let slow = ManifestCredentialRefreshDriver::<Credentials>::new(
        "Example",
        &auth,
        client.clone(),
        settings,
    )
    .expect("driver");
    let error = refresh(&slow, &Credentials::new("old", "prior")).refresh_error("deadline");
    assert!(error.is::<RetryError>());
    let settings = BTreeMap::from([("requestTimeout".to_string(), json!("2s"))]);
    let patient =
        ManifestCredentialRefreshDriver::<Credentials>::new("Example", &auth, client, settings)
            .expect("driver");
    let result = refresh(&patient, &Credentials::new("old", "prior")).expect("refreshed");
    assert_eq!(result.credentials.access_token.reveal(), "new");
}

#[test]
fn private_network_policy_refuses_public_http_without_connecting() {
    let mut auth = auth();
    let oauth = &mut auth["methods"][0]["oauth2"];
    oauth["tokenEndpoint"] = json!("http://8.8.8.8/token");
    oauth["tokenEndpointNetworkPolicy"] = json!("privateNetworkHTTP");
    let client = ProviderHttpClient::new(Duration::from_secs(5), AddressPolicy::PublicHttps)
        .expect("client");
    let driver = driver(&auth, client);
    let error = refresh(&driver, &Credentials::new("old", "prior")).refresh_error("refused");
    assert!(error.is::<AddressRefusal>());
}

fn workspace_auth() -> JsonValue {
    json!({
        "refreshable": true,
        "defaultMethod": "google-oauth",
        "methods": [{
            "id": "workspace-domain-delegation",
            "type": "serviceAccount",
            "fields": [
                {"name": "service_account_key", "type": "secretString", "required": true},
                {"name": "delegated_user", "type": "string", "required": true},
                {"name": "access_token", "type": "secretString", "required": false},
                {"name": "primary_email", "type": "string", "required": false}
            ],
            "serviceAccount": {"jwtBearer": {
                "keyCredential": "service_account_key",
                "privateKey": {"fromKey": "private_key"},
                "issuer": {"fromKey": "client_email"},
                "subject": {"fromCredential": "delegated_user"},
                "audience": {"fromKey": "token_uri"},
                "tokenEndpoint": {"fromKey": "token_uri"},
                "scopes": ["https://www.googleapis.com/auth/gmail.readonly"],
                "lifetime": "1h",
                "subjectValidation": "goMailAddress",
                "credentialMappings": [{"credential": "access_token", "source": "access_token"}],
                "credentialCopies": [{"credential": "primary_email", "fromCredential": "delegated_user"}],
                "terminalErrorCodes": ["invalid_grant", "invalid_client", "unauthorized_client"],
                "missingExpiry": "reject",
                "storedMissingExpiry": "refresh"
            }}
        }]
    })
}

#[test]
fn manifest_jwt_bearer_resolves_key_sources_and_copies_identity() {
    let (transport, client) = scripted(
        200,
        json!({"access_token": "delegated-access", "expires_in": 3600, "token_type": "Bearer", "scope": "https://www.googleapis.com/auth/gmail.readonly"}),
    );
    let fixed_now = Rfc3339NanoTimestamp::from_wire("2026-10-29T00:00:00Z").expect("timestamp");
    let clock_value = fixed_now.clone();
    let driver = ManifestCredentialRefreshDriver::<Credentials>::new_with_clock(
        "Google",
        &workspace_auth(),
        client,
        BTreeMap::new(),
        Box::new(move || clock_value.clone()),
    )
    .expect("driver");
    let key = json!({
        "client_email": "robot@project.iam.example.test",
        "private_key": include_str!("fixtures/inert_test_rsa_pkcs8.pem"),
        "token_uri": "https://oauth2.example.test/token"
    });
    let mut credentials = Credentials::new("", "");
    credentials.auth_method_id = "workspace-domain-delegation".to_string();
    credentials.service_account_key = SecretString::new(key.to_string());
    credentials.delegated_user = "user@example.test".to_string();
    let refresh_state = CredentialRefreshState {
        credentials: &credentials,
        expires_at: None,
        now: fixed_now.clone(),
    };
    assert!(driver.is_refresh_required(&refresh_state));
    let result = driver.refresh(&refresh_state).expect("delegated token");
    assert_eq!(result.credentials.access_token.reveal(), "delegated-access");
    assert_eq!(result.credentials.primary_email, "user@example.test");
    assert_eq!(result.expires_at.as_wire_str(), "2026-10-29T01:00:00Z");
    let request = transport.requests.lock().expect("requests")[0].clone();
    assert_eq!(request.url, "https://oauth2.example.test/token");
    let body = String::from_utf8(request.body).expect("form");
    let assertion = body
        .split('&')
        .find_map(|pair| pair.strip_prefix("assertion="))
        .expect("assertion parameter");
    let claims_segment = assertion.split('.').nth(1).expect("claims");
    let claims: JsonValue =
        serde_json::from_slice(&URL_SAFE_NO_PAD.decode(claims_segment).expect("base64"))
            .expect("claims json");
    assert_eq!(claims["sub"], "user@example.test");
    assert_eq!(claims["iss"], "robot@project.iam.example.test");
    assert_eq!(claims["aud"], "https://oauth2.example.test/token");
    assert_eq!(claims["iat"], 1_793_232_000);
    assert!(body.contains("grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer"));
    let mut invalid_subject = credentials.clone();
    invalid_subject.delegated_user = "not an address".to_string();
    assert!(
        refresh(&driver, &invalid_subject)
            .refresh_error("invalid subject")
            .is::<ReauthorizationRequiredError>()
    );
    let mut invalid_key = credentials;
    invalid_key.service_account_key = SecretString::new("not json");
    assert!(
        refresh(&driver, &invalid_key)
            .refresh_error("invalid key")
            .is::<ReauthorizationRequiredError>()
    );
}

#[test]
fn scope_array_graph_prefix_url_origin_and_expiry_cap() {
    let mut auth = auth();
    let refresh_declaration = &mut auth["methods"][0]["oauth2"]["refresh"];
    refresh_declaration["requiredScopes"] = json!(["files.read"]);
    refresh_declaration["responseScopeField"] = json!("scopesArray");
    refresh_declaration["scopeResourcePrefix"] = json!("https://graph.microsoft.com/");
    refresh_declaration["caseInsensitiveScopes"] = json!(true);
    refresh_declaration["scopeResourcePrefixCaseInsensitive"] = json!(true);
    refresh_declaration["expiryCap"] = json!("1h");
    refresh_declaration["responseURLValidation"] = json!({
        "credential": "instance_url",
        "allowedHosts": ["example.test"],
        "requireResponse": true,
        "refreshWhenInvalid": true
    });
    let body = json!({
        "access_token": "next",
        "token_type": "Bearer",
        "expires_in": 7200,
        "scopes": ["HTTPS://GRAPH.MICROSOFT.COM/FILES.READ"],
        "instance_url": "https://example.test/"
    });
    let (transport, client) = scripted(200, body.clone());
    let driver = driver(&auth, client.clone());
    let credentials = Credentials::new("old", "prior");
    let later = Rfc3339NanoTimestamp::from_system_time_utc(
        std::time::SystemTime::now() + Duration::from_secs(3600),
    );
    assert!(driver.is_refresh_required(&CredentialRefreshState {
        credentials: &credentials,
        expires_at: Some(later),
        now: Rfc3339NanoTimestamp::now_utc(),
    }));
    let before = Rfc3339NanoTimestamp::now_utc().unix_nanoseconds();
    let result = refresh(&driver, &credentials).expect("refreshed");
    assert_eq!(result.credentials.instance_url, "https://example.test");
    assert!(result.expires_at.unix_nanoseconds() < before + 3_601_000_000_000);
    let respond = |body: JsonValue| *transport.response.lock().expect("response") = (200, body);
    let with = |changes: JsonValue| {
        let mut changed = body.clone();
        for (name, value) in changes.as_object().expect("object") {
            if value.is_null() && name != "scopes" && name != "instance_url" {
                changed.as_object_mut().expect("object").remove(name);
            } else {
                changed[name] = value.clone();
            }
        }
        changed
    };
    for scopes in [json!(["other"]), json!(["files.read,other"]), json!([])] {
        respond(with(json!({"scopes": scopes})));
        assert!(
            refresh(&driver, &credentials)
                .refresh_error("wrong scopes")
                .is::<ReauthorizationRequiredError>()
        );
    }
    let mut without_array = body.clone();
    without_array
        .as_object_mut()
        .expect("object")
        .remove("scopes");
    without_array["scope"] = json!("other");
    respond(without_array);
    refresh(&driver, &credentials).expect("absent scope array is unchecked");
    respond(with(json!({"scopes": null})));
    refresh(&driver, &credentials).expect("null scope array is unchecked");
    respond(with(
        json!({"scopes": ["files.read"], "instance_url": "https://foreign.test"}),
    ));
    assert!(
        refresh(&driver, &credentials)
            .refresh_error("foreign origin")
            .is::<RetryError>()
    );
    let mut case_sensitive_auth = auth.clone();
    case_sensitive_auth["methods"][0]["oauth2"]["refresh"]["scopeResourcePrefixCaseInsensitive"] =
        json!(false);
    let case_sensitive = self::driver(&case_sensitive_auth, client);
    respond(body.clone());
    assert!(
        refresh(&case_sensitive, &credentials)
            .refresh_error("prefix case")
            .is::<ReauthorizationRequiredError>()
    );
    let mut prior = credentials.clone();
    prior.instance_url = "https://example.test/".to_string();
    respond(with(json!({"instance_url": null})));
    assert!(
        refresh(&driver, &prior)
            .refresh_error("null origin")
            .is::<RetryError>()
    );
    let mut without_origin = body;
    without_origin
        .as_object_mut()
        .expect("object")
        .remove("instance_url");
    respond(without_origin);
    assert!(
        refresh(&driver, &prior)
            .refresh_error("required origin")
            .is::<RetryError>()
    );
}

#[test]
fn response_url_rules_and_blank_refresh_fields() {
    let mut auth = auth();
    let refresh_declaration = &mut auth["methods"][0]["oauth2"]["refresh"];
    refresh_declaration["refreshWhenCredentialBlank"] = json!("instance_url");
    refresh_declaration["responseURLValidation"] = json!({
        "credential": "instance_url", "allowedHosts": ["example.test"],
        "allowedHostSuffix": "example.test", "allowLoopback": true,
        "forbidPort": true, "defaultURL": "https://example.test", "refreshWhenInvalid": true
    });
    let (transport, client) = scripted(
        200,
        json!({"access_token": "next", "token_type": "Bearer", "expires_in": 3600, "scope": "files.read"}),
    );
    let driver = driver(&auth, client.clone());
    let mut blank = Credentials::new("old", "prior");
    blank.instance_url = String::new();
    let later = Rfc3339NanoTimestamp::from_system_time_utc(
        std::time::SystemTime::now() + Duration::from_secs(3600),
    );
    assert!(driver.is_refresh_required(&CredentialRefreshState {
        credentials: &blank,
        expires_at: Some(later),
        now: Rfc3339NanoTimestamp::now_utc(),
    }));
    assert_eq!(
        refresh(&driver, &blank)
            .expect("default")
            .credentials
            .instance_url,
        "https://example.test"
    );
    for origin in ["http://[::1]", "http://127.0.0.2"] {
        let mut credentials = blank.clone();
        credentials.instance_url = origin.to_string();
        assert_eq!(
            refresh(&driver, &credentials)
                .expect("loopback")
                .credentials
                .instance_url,
            origin
        );
    }
    for origin in [
        "https://example.test:8443",
        "https://example.test:443",
        "https://badexample.test",
        "http://127.1",
        "http://2130706433",
    ] {
        let mut credentials = blank.clone();
        credentials.instance_url = origin.to_string();
        assert!(
            refresh(&driver, &credentials)
                .refresh_error(origin)
                .is::<RetryError>(),
            "{origin}"
        );
    }
    assert_eq!(transport.requests.lock().expect("requests").len(), 8);
    for bad_rule in [
        json!({"credential": "instance_url", "unexpected": true}),
        json!({"allowAnyHTTPS": true}),
    ] {
        let mut bad = auth.clone();
        bad["methods"][0]["oauth2"]["refresh"]["responseURLValidation"] = bad_rule;
        assert!(
            ManifestCredentialRefreshDriver::<Credentials>::new(
                "Example",
                &bad,
                client.clone(),
                BTreeMap::new()
            )
            .is_err()
        );
    }
    let mut missing = auth;
    missing["methods"][0]["oauth2"]["refresh"]["responseURLValidation"] = json!({});
    missing["methods"][0]["oauth2"]["refresh"]["refreshWhenCredentialBlank"] = json!("missing");
    assert!(
        ManifestCredentialRefreshDriver::<Credentials>::new(
            "Example",
            &missing,
            client,
            BTreeMap::new()
        )
        .is_err()
    );
}
