// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_ihuyi as connector;
const CONNECTOR_ID: &str = "ihuyi";
const ORIGIN: &str = "https://api.ihuyi.com";
fn scenario_credentials(_: &str) -> connector::Credentials {
    connector::Credentials {
        api_id: "test-account".into(),
        api_key: dex_connectors_sdk::SecretString::new("private-test-key"),
    }
}

use connector::{LookupIPLocationInput as QueryInput, LookupIPLocationOperation as QueryOperation};
const OPERATION_ID: &str = "lookupIPLocation";

#[path = "support/http_query.rs"]
mod http_query;

#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, clock| {
        QueryOperation::with_http_client_and_clock(credentials, http, limit, clock).unwrap()
    });
}
