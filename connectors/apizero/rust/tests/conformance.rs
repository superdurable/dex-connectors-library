// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetAccountInfoInput as QueryInput;
use dex_connector_apizero as connector;
const ORIGIN: &str = "https://v1.apizero.cn";
const CONNECTOR_ID: &str = "apizero";
const OPERATION_ID: &str = "getAccountInfo";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new(if name == "getAccountInfo-defect" {
        "invalid"
    } else {
        "sk_live_test-secret"
    })
    .into()
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::GetAccountInfoOperation::with_http_client(credentials, http, limit).unwrap()
    })
}
