// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListSpacesInput as QueryInput;
use dex_connector_huoban as connector;
const ORIGIN: &str = "https://api.huoban.com";
const CONNECTOR_ID: &str = "huoban";
const OPERATION_ID: &str = "listSpaces";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listSpaces-defect" {
        ""
    } else {
        "test-secret"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListSpacesOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
