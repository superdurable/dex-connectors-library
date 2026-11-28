// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap, VecDeque};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use crate::connector::{Credentials, ListModelsInput, ListModelsOperation};
use crate::{CONNECTOR_ID, FIXTURE, KEY, MISSING_KEY_CASE, ORIGIN, SMALL_BODY_CASE};
use dex_connectors_sdk::providerhttp::{
    ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, ProviderHttpResponse,
    ProviderHttpTransport, ProviderTransportError, ResponseHeaders,
};
use dex_connectors_sdk::{
    Call, CallId, ConnectionRef, OperationRef, Query, QueryAttempt, SecretString,
    StaticCredentialProvider,
};
use serde_json::{Value, json};

struct RecordedProvider {
    exchanges: Mutex<VecDeque<Value>>,
    requests: Mutex<usize>,
    scenario: String,
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
        assert_eq!(
            request.url,
            format!("{}{}", ORIGIN, exchange["path"].as_str().unwrap()),
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
fn replay_go_model_catalog() {
    let fixture = std::env::var("CONNECTOR_CONFORMANCE_FIXTURE").unwrap_or_else(|_| FIXTURE.into());
    let document: Value = serde_json::from_slice(&std::fs::read(fixture).unwrap()).unwrap();
    let scenarios = document["scenarios"].as_array().unwrap();
    assert!(!scenarios.is_empty());
    for scenario in scenarios {
        let name = scenario["name"].as_str().unwrap();
        assert!(scenario.get("pending").is_none(), "{name} is pending");
        assert_eq!(scenario["operation"], "listModels", "{name}");
        let recorded = Arc::new(RecordedProvider {
            scenario: name.into(),
            exchanges: Mutex::new(scenario["exchanges"].as_array().unwrap().clone().into()),
            requests: Mutex::new(0),
        });
        let connection = ConnectionRef::new(CONNECTOR_ID, "catalog");
        let key = if name == MISSING_KEY_CASE { "" } else { KEY };
        let credentials = StaticCredentialProvider::new(HashMap::from([(
            connection.clone(),
            Credentials::from(SecretString::new(key)),
        )]));
        let limit = if name == SMALL_BODY_CASE { 16 } else { 1 << 20 };
        let http =
            ProviderHttpClient::with_transport(recorded.clone(), Duration::from_secs(15)).unwrap();
        let operation =
            ListModelsOperation::with_http_client(Arc::new(credentials), http, limit).unwrap();
        let call = Call::new(
            CallId::new(scenario["callId"].as_str().unwrap()),
            connection,
            OperationRef::new(CONNECTOR_ID, "listModels"),
        )
        .unwrap();
        let input: ListModelsInput = serde_json::from_value(scenario["input"].clone()).unwrap();
        let expected = &scenario["expected"];
        match operation.invoke(&call, &input) {
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
        assert_eq!(expected["uncertain"], false, "{name}");
        assert_eq!(
            expected["requestCount"],
            *recorded.requests.lock().unwrap(),
            "{name}"
        );
        assert!(recorded.exchanges.lock().unwrap().is_empty(), "{name}");
    }
}
