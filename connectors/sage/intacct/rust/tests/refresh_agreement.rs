// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_sage_intacct::Credentials;
use dex_connectors_sdk::oauthtoken::{ManifestCredential, ManifestCredentialRefreshDriver};
use dex_connectors_sdk::providerhttp::*;
use dex_connectors_sdk::{
    CredentialRefreshDriver, CredentialRefreshState, Rfc3339NanoTimestamp, SecretString,
    is_reauthorization_required,
};
use serde_json::{Value, json};
use std::{collections::BTreeMap, sync::Arc, time::Duration};

struct Exchange(Value);
impl ProviderHttpTransport for Exchange {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        let scenario = &self.0;
        assert_eq!(request.method, "POST");
        assert_eq!(request.url, scenario["endpoint"]);
        assert_eq!(timeout, Duration::from_secs(15));
        let params: BTreeMap<_, _> = form_urlencoded::parse(&request.body).collect();
        assert_eq!(json!(params), scenario["parameters"]);
        Ok(ProviderHttpResponse::new(
            200,
            ResponseHeaders::new(vec![]),
            Box::new(std::io::Cursor::new(
                scenario["response"].to_string().into_bytes(),
            )),
        ))
    }
}
#[test]
fn replay_manifest_grants() {
    let scenarios: Vec<Value> =
        serde_json::from_str(include_str!("fixtures/refresh-agreement-cases.json")).unwrap();
    assert!(!scenarios.is_empty());
    for scenario in scenarios {
        let now = Rfc3339NanoTimestamp::from_wire("2026-01-01T00:00:00Z").unwrap();
        let fixed_now = now.clone();
        let driver = ManifestCredentialRefreshDriver::new_with_clock(
            "SageIntacct",
            &serde_json::from_str(include_str!("../src/generated_refresh_authorization.json"))
                .unwrap(),
            ProviderHttpClient::with_transport(
                Arc::new(Exchange(scenario.clone())),
                Duration::from_secs(15),
            )
            .unwrap(),
            BTreeMap::new(),
            Box::new(move || fixed_now.clone()),
        )
        .unwrap();
        let fields = &scenario["credentials"];
        let credentials = Credentials {
            oauth_client_id: fields["oauth_client_id"].as_str().unwrap().into(),
            oauth_client_secret: SecretString::new(fields["oauth_client_secret"].as_str().unwrap()),
            refresh_token: SecretString::new(fields["refresh_token"].as_str().unwrap()),
            access_token: SecretString::new(fields["access_token"].as_str().unwrap()),
        };
        for decision in scenario["refreshRequiredCases"].as_array().unwrap() {
            let mut prior = credentials.clone();
            if let Some(changes) = decision["credentials"].as_object() {
                for (name, value) in changes {
                    prior = prior.with_credential_value(name, value.as_str().unwrap().to_owned());
                }
            }
            let expires_at = match decision["expiresAt"].as_str().unwrap() {
                "fresh" => Some(Rfc3339NanoTimestamp::from_wire("2026-01-01T01:00:00Z").unwrap()),
                "expired" => Some(Rfc3339NanoTimestamp::from_wire("2025-12-31T23:59:59Z").unwrap()),
                "missing" => None,
                _ => panic!("unknown expiry"),
            };
            assert_eq!(
                driver.is_refresh_required(&CredentialRefreshState {
                    credentials: &prior,
                    expires_at,
                    now: now.clone()
                }),
                decision["expected"]
            );
        }
        let result = driver.refresh(&CredentialRefreshState {
            credentials: &credentials,
            expires_at: None,
            now: now.clone(),
        });
        match scenario.get("expectedError").and_then(Value::as_str) {
            None => {
                let result = result.unwrap();
                for name in Credentials::credential_field_names() {
                    assert_eq!(
                        result.credentials.credential_value(name),
                        scenario["updates"]
                            .get(name)
                            .unwrap_or(&fields[name])
                            .as_str()
                            .unwrap()
                    );
                }
                assert_eq!(
                    result.expires_at.unix_nanoseconds() - now.unix_nanoseconds(),
                    i128::from(scenario["response"]["expires_in"].as_i64().unwrap())
                        * 1_000_000_000
                );
            }
            Some(kind @ ("reauthorization" | "retryable")) => assert_eq!(
                is_reauthorization_required(&*result.unwrap_err()),
                kind == "reauthorization"
            ),
            _ => panic!("unknown refresh classification"),
        }
    }
}
