// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListApplicationsInput as QueryInput;
use dex_connector_tingyun as connector;
const ORIGIN: &str = "https://wukong2.tingyun.com";
const CONNECTOR_ID: &str = "tingyun";
const OPERATION_ID: &str = "listApplications";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listApplications-defect" {
        ""
    } else {
        "access-secret"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListApplicationsOperation::with_base_url(credentials, http, limit, ORIGIN)
            .unwrap()
    });
}
#[test]
fn rejects_untrusted_origin() {
    use std::sync::Arc;
    let config = connector::Config {
        api_host: "https://example.org/prefix".into(),
        ..Default::default()
    };
    let credentials = Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
        Default::default(),
    ));
    assert!(connector::ListApplicationsOperation::new(config, credentials).is_err());
}
