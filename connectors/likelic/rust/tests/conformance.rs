// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListRoomsInput as QueryInput;
use dex_connector_likelic as connector;
const ORIGIN: &str = "https://agent.likelic.com";
const CONNECTOR_ID: &str = "likelic";
const OPERATION_ID: &str = "listRooms";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        customer_code: "demo_company".into(),
        api_key: dex_connectors_sdk::SecretString::new(if name == "listRooms-invalidCredentials" {
            ""
        } else {
            "sk-0123456789abcdef0123456789abcdef0123456789abcdef"
        }),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListRoomsOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
