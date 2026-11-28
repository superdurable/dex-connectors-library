// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_workday::{Client, GetWorkerInput, project_credentials};
use dex_connectors_sdk::projectconfig::load_dex_web_project;
use dex_connectors_sdk::providerhttp::*;
use dex_connectors_sdk::{
    Call, CallId, ConnectionRef, FailureKind, OperationRef, Query, QueryAttempt,
};
use serde_json::{Value, json};
use std::{
    error::Error,
    io::Write,
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::Duration,
};

struct TokenEndpoint {
    status: u16,
    body: Value,
    requests: AtomicUsize,
}
impl ProviderHttpTransport for TokenEndpoint {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        _: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        assert_eq!(request.url, "https://tenant.example.com/token");
        self.requests.fetch_add(1, Ordering::SeqCst);
        Ok(ProviderHttpResponse::new(
            self.status,
            ResponseHeaders::new(vec![]),
            Box::new(std::io::Cursor::new(self.body.to_string().into_bytes())),
        ))
    }
}
#[test]
fn token_exchange_failures_stop_before_reading_workers() -> Result<(), Box<dyn Error>> {
    for (status, body, expected_failure) in [
        (
            400,
            json!({"error":"invalid_grant"}),
            FailureKind::Authentication,
        ),
        (
            400,
            json!({"error":"invalid_client"}),
            FailureKind::Authentication,
        ),
        (503, json!({}), FailureKind::Transport),
    ] {
        let mut connection = tempfile::NamedTempFile::new()?;
        let document = json!({"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{"connectorId":"workday","connectionName":"workday","modulePath":"github.com/superdurable/dex-connectors-library/connectors/workday/go","provider":"workday","configuration":{},"credentials":{"oauth_client_id":"fixture-id","oauth_client_secret":"fixture-secret","access_token":"expired","refresh_token":"test-refresh"}}]});
        connection.write_all(&serde_json::to_vec(&document)?)?;
        let project = load_dex_web_project(connection.path())?;
        let endpoint = Arc::new(TokenEndpoint {
            status,
            body,
            requests: 0.into(),
        });
        let http = ProviderHttpClient::with_transport(endpoint.clone(), Duration::from_secs(15))?;
        let client = Arc::new(Client::with_http_client(
            "https://tenant.example.com/api/common/v1/demo",
            "https://tenant.example.com/token",
            1 << 20,
            Arc::new(project_credentials(&project, "workday")?),
            http,
        )?);
        let call = Call::new(
            CallId::new("63166940-5976-4be8-b6ce-dc9c14446f1d"),
            ConnectionRef::new("workday", "workday"),
            OperationRef::new("workday", "getWorker"),
        )?;
        let attempt = client.get_worker().invoke(
            &call,
            &GetWorkerInput {
                worker_id: "0123456789abcdef0123456789abcdef".into(),
            },
        );
        match attempt {
            QueryAttempt::Branch {
                branch, failure, ..
            } => {
                assert_eq!(expected_failure, FailureKind::Authentication);
                assert_eq!(branch, "defect");
                assert_eq!(failure.unwrap().kind, expected_failure);
            }
            QueryAttempt::Retry { failure, .. } => {
                assert_eq!(expected_failure, FailureKind::Transport);
                assert_eq!(failure.kind, expected_failure);
            }
        }
        assert_eq!(endpoint.requests.load(Ordering::SeqCst), 1);
    }
    Ok(())
}
