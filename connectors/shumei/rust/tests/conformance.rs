// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetBalanceInput as QueryInput;
use dex_connector_shumei as connector;
const ORIGIN: &str = "https://api-web-bj.fengkongcloud.com";
const CONNECTOR_ID: &str = "shumei";
const OPERATION_ID: &str = "getBalance";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new(if name == "getBalance-defect" {
        ""
    } else {
        "secretTestKey"
    })
    .into()
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, _, _clock| {
        connector::GetBalanceOperation::with_http_client(credentials, http, 65536).unwrap()
    })
}
