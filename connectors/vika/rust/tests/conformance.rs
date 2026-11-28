// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListSpacesInput as QueryInput;
use dex_connector_vika as connector;
const ORIGIN: &str = "https://vika.cn";
const CONNECTOR_ID: &str = "vika";
const OPERATION_ID: &str = "listSpaces";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listSpaces-defect" {
        ""
    } else {
        "uskTestToken"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListSpacesOperation::with_base_url(credentials, http, limit, ORIGIN).unwrap()
    });
}
#[test]
fn rejects_untrusted_origin() {
    use std::sync::Arc;
    let config = connector::Config {
        base_url: "https://example.com/?query=rejected".into(),
        ..Default::default()
    };
    let credentials = Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
        Default::default(),
    ));
    assert!(connector::ListSpacesOperation::new(config, credentials).is_err());
}
