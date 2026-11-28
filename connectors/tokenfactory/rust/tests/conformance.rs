// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetBalanceInput as QueryInput;
use dex_connector_tokenfactory as connector;
const ORIGIN: &str = "https://api.tokenfactory.cn";
const CONNECTOR_ID: &str = "tokenfactory";
const OPERATION_ID: &str = "getBalance";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new(if name == "getBalance-defect" {
        ""
    } else {
        "sk-test-secret"
    })
    .into()
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, _, _clock| {
        connector::GetBalanceOperation::with_http_client(credentials, http, 65536).unwrap()
    })
}
