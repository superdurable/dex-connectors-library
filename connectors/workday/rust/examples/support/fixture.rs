// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_workday::{Client, project_credentials};
use dex_connectors_sdk::projectconfig::load_dex_web_project;
use dex_connectors_sdk::providerhttp::*;
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
use tempfile::NamedTempFile;

pub struct WorkdayFixture {
    connection: NamedTempFile,
    provider: Arc<WorkersApi>,
}
impl WorkdayFixture {
    pub fn new(operation: &'static str) -> Result<Self, Box<dyn Error>> {
        let mut connection = NamedTempFile::new()?;
        let document = json!({"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{
            "connectorId":"workday","connectionName":"workday","modulePath":"github.com/superdurable/dex-connectors-library/connectors/workday/go","provider":"workday","configuration":{},"credentials":{"oauth_client_id":"test-id","oauth_client_secret":"test-secret","access_token":"expired","refresh_token":"test-refresh"}
        }]});
        connection.write_all(&serde_json::to_vec(&document)?)?;
        Ok(Self {
            connection,
            provider: Arc::new(WorkersApi {
                operation,
                exchanges: 0.into(),
                reads: 0.into(),
            }),
        })
    }
    pub fn client(&self) -> Result<Arc<Client>, Box<dyn Error>> {
        let project = load_dex_web_project(self.connection.path())?;
        let credentials = Arc::new(project_credentials(&project, "workday")?);
        let http =
            ProviderHttpClient::with_transport(self.provider.clone(), Duration::from_secs(15))?;
        Ok(Arc::new(Client::with_http_client(
            "https://tenant.example.com/api/common/v1/demo",
            "https://tenant.example.com/token",
            1 << 20,
            credentials,
            http,
        )?))
    }
    pub fn verify_renewal(&self) -> Result<(), Box<dyn Error>> {
        assert_eq!(self.provider.exchanges.load(Ordering::SeqCst), 1);
        assert_eq!(self.provider.reads.load(Ordering::SeqCst), 2);
        let stored: Value = serde_json::from_slice(&std::fs::read(self.connection.path())?)?;
        let connection = &stored["connections"][0];
        assert!(connection["credentialExpiresAt"].is_string());
        assert_eq!(connection["credentials"]["access_token"], "test-bearer");
        assert_eq!(
            connection["credentials"]["oauth_client_secret"],
            "test-secret"
        );
        assert_eq!(connection["credentials"]["refresh_token"], "rotated");
        Ok(())
    }
}
struct WorkersApi {
    operation: &'static str,
    exchanges: AtomicUsize,
    reads: AtomicUsize,
}
impl ProviderHttpTransport for WorkersApi {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        let (status, body) = if request.url.ends_with("/token") {
            assert_eq!(request.method, "POST");
            assert_eq!(request.url, "https://tenant.example.com/token");
            assert_eq!(timeout, Duration::from_secs(15));
            let parameters: std::collections::BTreeMap<_, _> =
                form_urlencoded::parse(&request.body).collect();
            assert_eq!(parameters.len(), 2);
            assert_eq!(parameters["grant_type"], "refresh_token");
            assert_eq!(parameters["refresh_token"], "test-refresh");
            assert!(
                request
                    .headers
                    .iter()
                    .any(|(k, v)| k.eq_ignore_ascii_case("Authorization")
                        && v == "Basic dGVzdC1pZDp0ZXN0LXNlY3JldA==")
            );
            self.exchanges.fetch_add(1, Ordering::SeqCst);
            (
                200,
                json!({"access_token":"test-bearer","token_type":"Bearer","refresh_token":"rotated","expires_in":3600}),
            )
        } else {
            assert_eq!(request.method, "GET");
            assert_eq!(
                request
                    .headers
                    .iter()
                    .find(|(key, _)| key.eq_ignore_ascii_case("Authorization"))
                    .map(|(_, value)| value.as_str()),
                Some("Bearer test-bearer")
            );
            let path = if self.operation == "GetWorker" {
                "/workers/0123456789abcdef0123456789abcdef"
            } else {
                "/workers"
            };
            assert_eq!(
                request.url,
                format!("https://tenant.example.com/api/common/v1/demo{path}")
            );
            let profile = json!({"id":"0123456789abcdef0123456789abcdef","descriptor":"Test","email":"excluded@example.org"});
            let body = if self.operation == "GetWorker" {
                profile
            } else {
                json!({"data":[profile],"total":7})
            };
            (
                if self.reads.fetch_add(1, Ordering::SeqCst) == 0 {
                    503
                } else {
                    200
                },
                body,
            )
        };
        Ok(ProviderHttpResponse::new(
            status,
            ResponseHeaders::new(vec![]),
            Box::new(std::io::Cursor::new(serde_json::to_vec(&body).unwrap())),
        ))
    }
}
