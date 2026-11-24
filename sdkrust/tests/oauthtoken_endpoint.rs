// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! OAuth token endpoint, refresh timing, and JWT-bearer signing.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use dex_connectors_sdk::oauthtoken::{
    ClientAuthenticationMethod, ClientCredentials, JwtBearerAssertion, MissingExpiryRule,
    TokenEndpoint, TokenExchangeError, forced_refresh_floor, has_all_scopes, has_exact_scopes,
    is_refresh_required, parse_rsa_private_key_pem, proportional_refresh_skew,
    sign_jwt_bearer_assertion,
};
use dex_connectors_sdk::providerhttp::{
    ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, ProviderHttpResponse,
    ProviderHttpTransport, ResponseHeaders,
};
use dex_connectors_sdk::{Rfc3339NanoTimestamp, SecretString};

/// Scripted transport that records requests, like `httpx.MockTransport`.
struct ScriptedTransport {
    response: Mutex<(u16, Vec<u8>)>,
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
            Box::new(std::io::Cursor::new(body)),
        ))
    }
}

fn scripted(status: u16, body: &str) -> (Arc<ScriptedTransport>, ProviderHttpClient) {
    let transport = Arc::new(ScriptedTransport {
        response: Mutex::new((status, body.as_bytes().to_vec())),
        requests: Mutex::new(Vec::new()),
    });
    let client = ProviderHttpClient::with_transport(transport.clone(), Duration::from_secs(5))
        .expect("client");
    (transport, client)
}

fn timestamp(wire: &str) -> Rfc3339NanoTimestamp {
    Rfc3339NanoTimestamp::from_wire(wire).expect("timestamp")
}

#[test]
fn refresh_timing_and_scope_rules() {
    let now = timestamp("2026-10-29T00:00:00Z");
    let expiry = timestamp("2026-10-29T00:01:00Z");
    assert_eq!(
        proportional_refresh_skew(Some(&now), Some(&expiry)),
        Duration::from_secs(15)
    );
    assert_eq!(
        forced_refresh_floor(Some(&now), Some(&expiry)),
        Duration::from_secs(15)
    );
    assert!(is_refresh_required(
        true,
        Some(&expiry),
        &timestamp("2026-10-29T00:00:46Z"),
        MissingExpiryRule::RefreshWhenExpiryMissing,
        Some(&now),
    ));
    assert!(!is_refresh_required(
        true,
        Some(&expiry),
        &now,
        MissingExpiryRule::RefreshWhenExpiryMissing,
        Some(&now),
    ));
    assert!(is_refresh_required(
        true,
        None,
        &now,
        MissingExpiryRule::RefreshWhenExpiryMissing,
        None
    ));
    assert!(!is_refresh_required(
        true,
        None,
        &now,
        MissingExpiryRule::KeepWhenExpiryMissing,
        None
    ));
    assert!(is_refresh_required(
        false,
        None,
        &now,
        MissingExpiryRule::KeepWhenExpiryMissing,
        None
    ));
    assert!(has_all_scopes("read,write extra", &["read", "write"]));
    assert!(!has_exact_scopes("read,write extra", &["read", "write"]));
    assert!(has_exact_scopes("read,write", &["write", "read"]));
}

#[test]
fn refresh_exchange_and_failure_classification() {
    let (transport, client) = scripted(
        200,
        r#"{"access_token":"INERT_ACCESS","expires_in":3600,"token_type":"Bearer"}"#,
    );
    let mut endpoint = TokenEndpoint::new("Example", "https://oauth.example/token", client);
    endpoint.terminal_error_codes = vec!["invalid_grant".to_string()];
    let credentials = ClientCredentials::new(
        "client",
        SecretString::new("INERT_CLIENT_SECRET"),
        ClientAuthenticationMethod::ClientSecretPost,
    );
    let result = endpoint
        .exchange_refresh_token(&credentials, &SecretString::new("INERT_REFRESH"))
        .expect("token");
    assert_eq!(result.access_token.reveal(), "INERT_ACCESS");
    assert_eq!(
        result
            .next_refresh_token(&SecretString::new("prior"))
            .reveal(),
        "prior"
    );
    assert_eq!(result.expires_in, Duration::from_secs(3600));
    assert!(!format!("{result:?}").contains("INERT_ACCESS"));
    let first = transport.requests.lock().expect("requests")[0].clone();
    assert_eq!(
        String::from_utf8(first.body).expect("form"),
        "client_id=client&client_secret=INERT_CLIENT_SECRET&grant_type=refresh_token&refresh_token=INERT_REFRESH"
    );
    *transport.response.lock().expect("response") = (
        400,
        br#"{"error":"invalid_grant","error_description":"INERT_SENSITIVE_DETAIL"}"#.to_vec(),
    );
    let terminal = endpoint
        .exchange_refresh_token(&credentials, &SecretString::new("INERT_REFRESH"))
        .expect_err("terminal grant");
    assert!(matches!(
        terminal,
        TokenExchangeError::ReauthorizationRequired(_)
    ));
    assert!(!terminal.to_string().contains("INERT_SENSITIVE_DETAIL"));
    *transport.response.lock().expect("response") = (503, br#"{"error":"busy"}"#.to_vec());
    let unavailable = endpoint
        .exchange_refresh_token(&credentials, &SecretString::new("INERT_REFRESH"))
        .expect_err("unavailable");
    assert!(unavailable.to_string().contains("HTTP 503"));
    assert!(matches!(
        endpoint.exchange_refresh_token(&credentials, &SecretString::new("")),
        Err(TokenExchangeError::ReauthorizationRequired(_))
    ));
    assert_eq!(transport.requests.lock().expect("requests").len(), 3);
}

#[test]
fn basic_authentication_and_json_body() {
    let (transport, client) = scripted(
        200,
        r#"{"access_token":"a","expires_in":60,"token_type":"bearer","refresh_token":"rotated","scope":"x"}"#,
    );
    let mut endpoint = TokenEndpoint::new("Example", "https://oauth.example/token", client);
    endpoint.uses_json_request_body = true;
    let credentials = ClientCredentials::new(
        "client",
        SecretString::new("secret"),
        ClientAuthenticationMethod::ClientSecretBasic,
    );
    let token = endpoint
        .exchange_refresh_token(&credentials, &SecretString::new("refresh"))
        .expect("token");
    assert_eq!(
        token
            .next_refresh_token(&SecretString::new("prior"))
            .reveal(),
        "rotated"
    );
    let request = transport.requests.lock().expect("requests")[0].clone();
    let authorization = request
        .headers
        .iter()
        .find(|(name, _)| name == "Authorization")
        .map(|(_, value)| value.clone());
    assert_eq!(authorization.as_deref(), Some("Basic Y2xpZW50OnNlY3JldA=="));
    let body: serde_json::Value = serde_json::from_slice(&request.body).expect("json body");
    assert_eq!(body["grant_type"], "refresh_token");
    assert!(body.get("client_secret").is_none());
}

#[test]
fn token_endpoint_rejects_unsafe_configuration() {
    let (_transport, client) = scripted(302, "");
    let credentials = ClientCredentials::new(
        "client",
        SecretString::new("secret"),
        ClientAuthenticationMethod::ClientSecretBasic,
    );
    let refresh = SecretString::new("refresh");
    let exchange = |endpoint: &TokenEndpoint| {
        endpoint
            .exchange_refresh_token(&credentials, &refresh)
            .expect_err("exchange fails")
            .to_string()
    };
    let insecure = TokenEndpoint::new("Example", "http://oauth.example/token", client.clone());
    assert!(exchange(&insecure).contains("URL"));
    let uppercase = TokenEndpoint::new("Example", "HTTPS://oauth.example/token", client.clone());
    assert!(exchange(&uppercase).contains("HTTP 302"));
    let mut uppercase_plain =
        TokenEndpoint::new("Example", "HTTP://oauth.example/token", client.clone());
    uppercase_plain.allows_plain_http = true;
    assert!(exchange(&uppercase_plain).contains("HTTP 302"));
    let leading_space =
        TokenEndpoint::new("Example", " https://oauth.example/token", client.clone());
    assert!(exchange(&leading_space).contains("URL"));
    let mut override_grant =
        TokenEndpoint::new("Example", "https://oauth.example/token", client.clone());
    override_grant
        .additional_request_parameters
        .insert("grant_type".to_string(), "other".to_string());
    assert!(exchange(&override_grant).contains("grant"));
    let mut retained = TokenEndpoint::new("Example", "https://oauth.example/token", client.clone());
    retained.retained_response_fields = vec!["access_token".to_string()];
    assert!(exchange(&retained).contains("retain"));
    let safe = TokenEndpoint::new("Example", "https://oauth.example/token", client);
    assert!(exchange(&safe).contains("HTTP 302"));
}

#[test]
fn token_response_validation_rejects_missing_fields() {
    let credentials = ClientCredentials::new(
        "client",
        SecretString::new("secret"),
        ClientAuthenticationMethod::ClientSecretPost,
    );
    for body in [
        r#"{"expires_in":60,"token_type":"Bearer"}"#,
        r#"{"access_token":"a","token_type":"Bearer"}"#,
        r#"{"access_token":"a","expires_in":1.5,"token_type":"Bearer"}"#,
        r#"{"access_token":"a","expires_in":60}"#,
        r#"{"access_token":"a","expires_in":60,"token_type":"mac"}"#,
        r#"{"access_token":"a","expires_in":60,"token_type":"Bearer","scope":1}"#,
        "[]",
    ] {
        let (_transport, client) = scripted(200, body);
        let endpoint = TokenEndpoint::new("Example", "https://oauth.example/token", client);
        let error = endpoint
            .exchange_client_credentials(&credentials, &["scope".to_string()])
            .expect_err(body);
        assert!(
            matches!(error, TokenExchangeError::InvalidResponse(_)),
            "{body}"
        );
    }
    let (_transport, client) = scripted(200, r#"{"access_token":"a"}"#);
    let mut lenient = TokenEndpoint::new("Example", "https://oauth.example/token", client);
    lenient.accepts_missing_expires_in = true;
    lenient.accepts_missing_token_type = true;
    let token = lenient
        .exchange_client_credentials(&credentials, &[])
        .expect("lenient token");
    assert_eq!(token.expires_in, Duration::ZERO);
}

#[test]
fn jwt_bearer_assertion_signing() {
    for pem in [
        include_str!("fixtures/inert_test_rsa_pkcs1.pem"),
        include_str!("fixtures/inert_test_rsa_pkcs8.pem"),
    ] {
        let key = parse_rsa_private_key_pem(pem).expect("test key");
        assert!(!format!("{key:?}").contains("MII"));
        let assertion = JwtBearerAssertion {
            issuer: "service@example.test".to_string(),
            audience: "https://oauth.example/token".to_string(),
            issued_at: timestamp("2026-10-29T00:00:00Z"),
            lifetime: Duration::from_secs(3600),
            subject: "user@example.test".to_string(),
            scope: "scope".to_string(),
        };
        let signed = sign_jwt_bearer_assertion(&assertion, &key).expect("signed");
        let segments: Vec<&str> = signed.reveal().split('.').collect();
        assert_eq!(segments.len(), 3);
        let claims: serde_json::Value =
            serde_json::from_slice(&URL_SAFE_NO_PAD.decode(segments[1]).expect("claims"))
                .expect("claims json");
        assert_eq!(claims["sub"], "user@example.test");
        assert_eq!(claims["scope"], "scope");
        assert_eq!(
            claims["exp"].as_i64(),
            claims["iat"].as_i64().map(|iat| iat + 3600)
        );
        assert!(!signed.to_string().contains("service@example.test"));
    }
    assert!(parse_rsa_private_key_pem("not a key").is_err());
}
