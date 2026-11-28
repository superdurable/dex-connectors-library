// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListProvincesInput as QueryInput;
use dex_connector_jisuapi as connector;
const ORIGIN: &str = "https://api.jisuapi.com";
const CONNECTOR_ID: &str = "jisuapi";
const OPERATION_ID: &str = "listProvinces";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listProvinces-defect" {
        ""
    } else {
        "test-secret"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListProvincesOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
