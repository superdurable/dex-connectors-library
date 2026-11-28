// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListJobFunctionsInput as QueryInput;
use dex_connector_moka as connector;
const ORIGIN: &str = "https://api.mokahr.com";
const CONNECTOR_ID: &str = "moka";
const OPERATION_ID: &str = "listJobFunctions";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listJobFunctions-defect" {
        ""
    } else {
        "secret-api-key"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListJobFunctionsOperation::with_http_client(
            connector::Config {
                org_id: "orgid-test".into(),
                max_response_bytes: limit,
                ..Default::default()
            },
            credentials,
            http,
        )
        .unwrap()
    });
}
#[test]
fn rejects_invalid_settings() {
    for (org_id, mode) in [("org/path", "social"), ("org", "internal")] {
        let config = connector::Config {
            org_id: org_id.into(),
            mode: mode.into(),
            ..Default::default()
        };
        let credentials = std::sync::Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
            Default::default(),
        ));
        assert!(connector::ListJobFunctionsOperation::new(config, credentials).is_err());
    }
}
