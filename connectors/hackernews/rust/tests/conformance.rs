// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, VecDeque};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use dex_connector_hackernews::{ListStoryIDsInput, ListStoryIDsOperation};
use dex_connectors_sdk::Rfc3339NanoTimestamp;
const ORIGIN: &str = "https://hacker-news.firebaseio.com/v0";
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
fn replay_go_story_ids() {
    let fixture = std::env::var("CONNECTOR_CONFORMANCE_FIXTURE").unwrap_or_else(|_| FIXTURE.into());
    let document: Value = serde_json::from_slice(&std::fs::read(fixture).unwrap()).unwrap();
    let scenarios: Vec<_> = document["scenarios"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|s| s["operation"] == "listStoryIDs")
        .collect();
    assert!(!scenarios.is_empty());
    for scenario in scenarios {
        let name = scenario["name"].as_str().unwrap();
        assert!(scenario.get("pending").is_none(), "{name} is pending");

        let recorded = Arc::new(RecordedProvider {
            scenario: name.into(),
            exchanges: Mutex::new(scenario["exchanges"].as_array().unwrap().clone().into()),
            requests: Mutex::new(0),
        });
        let connection = ConnectionRef::new("hacker-news", "hacker-news-public");
        let http =
            ProviderHttpClient::with_transport(recorded.clone(), Duration::from_secs(15)).unwrap();
        let timestamp = Rfc3339NanoTimestamp::from_wire(scenario["fixedClock"].as_str().unwrap())
            .unwrap()
            .unix_nanoseconds();
        let instant = std::time::UNIX_EPOCH + Duration::from_nanos(timestamp.try_into().unwrap());
        let operation =
            ListStoryIDsOperation::with_client(ORIGIN, 1 << 20, http, Arc::new(move || instant))
                .unwrap();
        let call = Call::new(
            CallId::new(scenario["callId"].as_str().unwrap()),
            connection,
            OperationRef::new("hacker-news-daily", "listStoryIDs"),
        )
        .unwrap();
        let input: ListStoryIDsInput = serde_json::from_value(scenario["input"].clone()).unwrap();
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
