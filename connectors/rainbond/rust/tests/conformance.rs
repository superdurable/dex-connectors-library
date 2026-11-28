// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListTeamsInput as QueryInput;
use dex_connector_rainbond as connector;
const ORIGIN: &str = "https://rainbond.example.org";
const CONNECTOR_ID: &str = "rainbond";
const OPERATION_ID: &str = "listTeams";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(_: &str) -> connector::Credentials {
    connector::Credentials::new("private-test-key")
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListTeamsOperation::with_base_url(credentials, http, limit, ORIGIN).unwrap()
    });
}
#[test]
fn rejects_untrusted_origin() {
    use std::sync::Arc;
    let config = connector::Config {
        base_url: "https://example.org/prefix".into(),
        ..Default::default()
    };
    let credentials = Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
        Default::default(),
    ));
    assert!(connector::ListTeamsOperation::new(config, credentials).is_err());
}
