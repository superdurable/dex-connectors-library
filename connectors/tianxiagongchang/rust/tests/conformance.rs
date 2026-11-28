// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetAccountStatusInput as QueryInput;
use dex_connector_tianxiagongchang as connector;
const ORIGIN: &str = "https://open.tianxiagongchang.com";
const CONNECTOR_ID: &str = "tianxiagongchang";
const OPERATION_ID: &str = "getAccountStatus";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new(if name == "getAccountStatus-defect" {
        ""
    } else {
        "sk-tx-live-test-secret"
    })
    .into()
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::GetAccountStatusOperation::with_http_client(credentials, http, limit).unwrap()
    })
}
