// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_personio::{Client, project_credentials};
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

pub struct PersonioFixture {
    connection: NamedTempFile,
    provider: Arc<PersonsApi>,
}
impl PersonioFixture {
    pub fn new(operation: &'static str) -> Result<Self, Box<dyn Error>> {
        let mut connection = NamedTempFile::new()?;
        let document = json!({"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{
            "connectorId":"personio","connectionName":"personio","modulePath":"github.com/superdurable/dex-connectors-library/connectors/personio/go","provider":"personio","configuration":{},"credentials":{"client_id":"test-id","client_secret":"test-secret"}
        }]});
        connection.write_all(&serde_json::to_vec(&document)?)?;
        Ok(Self {
            connection,
            provider: Arc::new(PersonsApi {
                operation,
                exchanges: 0.into(),
                reads: 0.into(),
            }),
        })
    }
    pub fn client(&self) -> Result<Arc<Client>, Box<dyn Error>> {
        let project = load_dex_web_project(self.connection.path())?;
        let credentials = Arc::new(project_credentials(&project, "personio")?);
        let http =
            ProviderHttpClient::with_transport(self.provider.clone(), Duration::from_secs(15))?;
        Ok(Arc::new(Client::with_http_client(
            "https://api.personio.de/v2",
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
        assert_eq!(connection["credentials"]["client_secret"], "test-secret");
        Ok(())
    }
}
struct PersonsApi {
    operation: &'static str,
    exchanges: AtomicUsize,
    reads: AtomicUsize,
}
impl ProviderHttpTransport for PersonsApi {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        timeout: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        let (status, body) = if request.url.ends_with("/auth/token") {
            assert_eq!(request.method, "POST");
            assert_eq!(request.url, "https://api.personio.de/v2/auth/token");
            assert_eq!(timeout, Duration::from_secs(8));
            let parameters: std::collections::BTreeMap<_, _> =
                form_urlencoded::parse(&request.body).collect();
            for (key, value) in [
                ("client_id", "test-id"),
                ("client_secret", "test-secret"),
                ("grant_type", "client_credentials"),
                ("scope", "personio:persons:read"),
            ] {
                assert_eq!(parameters[key], value);
            }
            self.exchanges.fetch_add(1, Ordering::SeqCst);
            (
                200,
                json!({"access_token":"test-bearer","token_type":"Bearer","scope":"personio:persons:read","expires_in":3600}),
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
            let path = if self.operation == "GetPerson" {
                "/persons/123"
            } else {
                "/persons"
            };
            assert_eq!(request.url, format!("https://api.personio.de/v2{path}"));
            let profile = json!({"id":"123","first_name":"Test","email":"excluded@example.org"});
            let body = if self.operation == "GetPerson" {
                profile
            } else {
                json!({"_data":[profile],"_meta":{"links":{"next":{"href":"/v2/persons?cursor=page2"}}}})
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
