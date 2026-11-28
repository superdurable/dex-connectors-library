// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, VecDeque};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use dex_connector_sage_intacct::{Client, Credentials};
use dex_connectors_sdk::{SecretString, StaticCredentialProvider};
const ORIGIN: &str = "https://tenant.example.com";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
use dex_connectors_sdk::providerhttp::{
    ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, ProviderHttpResponse,
    ProviderHttpTransport, ProviderTransportError, ResponseHeaders,
};
use dex_connectors_sdk::{Call, CallId, ConnectionRef, OperationRef, Query, QueryAttempt};
use serde_json::{Value, json};

struct RecordedProvider {
    exchanges: Mutex<VecDeque<Value>>,
    requests: Mutex<usize>,
    scenario: String,
    origin: &'static str,
}

impl ProviderHttpTransport for RecordedProvider {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        _: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        let exchange = self
            .exchanges
            .lock()
            .unwrap()
            .pop_front()
            .expect(&self.scenario);
        assert_eq!(exchange["keyMode"], "absent", "{}", self.scenario);
        assert_eq!(exchange["method"], request.method, "{}", self.scenario);
        let (endpoint, query) = request.url.split_once('?').unwrap_or((&request.url, ""));
        assert_eq!(
            endpoint,
            format!("{}{}", self.origin, exchange["path"].as_str().unwrap()),
            "{}",
            self.scenario
        );
        let mut parameters: BTreeMap<String, Vec<String>> = BTreeMap::new();
        for (key, value) in form_urlencoded::parse(query.as_bytes()) {
            parameters
                .entry(key.into_owned())
                .or_default()
                .push(value.into_owned());
        }
        assert_eq!(
            json!(parameters),
            exchange.get("query").cloned().unwrap_or(json!({})),
            "{}",
            self.scenario
        );
        let headers: BTreeMap<_, _> = request
            .headers
            .iter()
            .map(|(key, value)| (key, vec![value]))
            .collect();
        assert_eq!(
            json!(headers),
            exchange["requestHeaders"],
            "{}",
            self.scenario
        );
        assert!(request.body.is_empty(), "{}", self.scenario);
        if exchange["transportFailure"] == "before-send" {
            return Err(ProviderHttpError::Transport(ProviderTransportError::new(
                true,
            )));
        }
        *self.requests.lock().unwrap() += 1;
        if exchange["transportFailure"] == "after-send" {
            return Err(ProviderHttpError::Transport(ProviderTransportError::new(
                false,
            )));
        }
        let headers = exchange["headers"]
            .as_object()
            .unwrap()
            .iter()
            .map(|(name, value)| (name.clone(), value.as_str().unwrap().to_owned()))
            .collect();
        Ok(ProviderHttpResponse::new(
            exchange["status"].as_u64().unwrap() as u16,
            ResponseHeaders::new(headers),
            Box::new(std::io::Cursor::new(
                exchange["body"].as_str().unwrap_or("").as_bytes().to_vec(),
            )),
        ))
    }
}

#[test]
fn replay_go_invoice_queries() {
    let fixture = std::env::var("CONNECTOR_CONFORMANCE_FIXTURE").unwrap_or_else(|_| FIXTURE.into());
    let document: Value = serde_json::from_slice(&std::fs::read(fixture).unwrap()).unwrap();
    let scenarios: Vec<_> = document["scenarios"].as_array().unwrap().iter().collect();
    assert!(!scenarios.is_empty());
    for scenario in scenarios {
        let name = scenario["name"].as_str().unwrap();
        assert!(scenario.get("pending").is_none(), "{name} is pending");

        let recorded = Arc::new(RecordedProvider {
            scenario: name.into(),
            origin: "https://tenant.example.com",
            exchanges: Mutex::new(scenario["exchanges"].as_array().unwrap().clone().into()),
            requests: Mutex::new(0),
        });
        let connection = ConnectionRef::new("sage-intacct", "sage-intacct");
        let http =
            ProviderHttpClient::with_transport(recorded.clone(), Duration::from_secs(15)).unwrap();
        let credentials = Credentials {
            oauth_client_id: "sample-client-id".into(),
            oauth_client_secret: SecretString::new("sample-client-secret"),
            access_token: SecretString::new("test-sage-intacct-token"),
            refresh_token: SecretString::new("test-refresh"),
        };
        let provider = Arc::new(StaticCredentialProvider::new(
            std::collections::HashMap::from([(connection.clone(), credentials)]),
        ));
        let client = Arc::new(
            Client::with_http_client(
                ORIGIN,
                if name.ends_with("-smallLimit") {
                    16
                } else {
                    1 << 20
                },
                provider,
                http,
            )
            .unwrap(),
        );
        let operation = scenario["operation"].as_str().unwrap();
        let call = Call::new(
            CallId::new(scenario["callId"].as_str().unwrap()),
            connection,
            OperationRef::new("sage-intacct", operation),
        )
        .unwrap();
        match operation {
            "listInvoices" => replay(client.list_invoices(), &call, scenario),
            "getInvoice" => replay(client.get_invoice(), &call, scenario),
            _ => unreachable!(),
        }
        let expected = &scenario["expected"];
        assert_eq!(expected["uncertain"], false, "{name}");
        assert_eq!(
            expected["requestCount"],
            *recorded.requests.lock().unwrap(),
            "{name}"
        );
        assert!(recorded.exchanges.lock().unwrap().is_empty(), "{name}");
    }
}

fn replay<Q: Query>(operation: Q, call: &Call<'_>, scenario: &Value)
where
    Q::Input: serde::de::DeserializeOwned,
    Q::Output: serde::Serialize,
{
    let name = scenario["name"].as_str().unwrap();
    let input = serde_json::from_value(scenario["input"].clone()).unwrap();
    let expected = &scenario["expected"];
    match operation.invoke(call, &input) {
        QueryAttempt::Branch {
            branch,
            value,
            failure,
            ..
        } => {
            assert_eq!(expected["retryAfterMs"], 0, "{name}");
            assert_eq!(expected["retry"], false, "{name}");
            assert_eq!(expected["branch"], branch, "{name}");
            assert_eq!(expected["output"], json!(value), "{name}");
            assert_eq!(
                expected["failureKind"],
                failure
                    .map(|failure| failure.kind.to_string())
                    .unwrap_or_default(),
                "{name}"
            );
        }
        QueryAttempt::Retry {
            failure,
            retry_after_seconds,
        } => {
            assert_eq!(expected["branch"], "", "{name}");
            assert_eq!(expected["retry"], true, "{name}");
            assert_eq!(
                expected["failureKind"],
                failure.kind.as_wire_str(),
                "{name}"
            );
            assert_eq!(
                expected["retryAfterMs"].as_f64().unwrap(),
                retry_after_seconds * 1000.0,
                "{name}"
            );
        }
    }
}
