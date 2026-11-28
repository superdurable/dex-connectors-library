// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_alapi as connector;
const CONNECTOR_ID: &str = "alapi";
const ORIGIN: &str = "https://v3.alapi.cn";
fn scenario_credentials(_: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new("test-token").into()
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
