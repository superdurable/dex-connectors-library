// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetAccountStatusInput as QueryInput;
use dex_connector_istero as connector;
const ORIGIN: &str = "https://api.istero.com";
const CONNECTOR_ID: &str = "istero";
const OPERATION_ID: &str = "getAccountStatus";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        isid: if name.ends_with("-invalidISID") {
            "060066023"
        } else {
            "60066023"
        }
        .into(),
        api_key: dex_connectors_sdk::SecretString::new(if name.ends_with("-defect") {
            ""
        } else {
            "private-test-token"
        }),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::GetAccountStatusOperation::with_http_client(credentials, http, limit).unwrap()
    })
}
