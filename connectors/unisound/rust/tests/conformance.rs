// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListSystemVoicesInput as QueryInput;
use dex_connector_unisound as connector;
const ORIGIN: &str = "https://maas-api.unisound.com";
const CONNECTOR_ID: &str = "unisound";
const OPERATION_ID: &str = "listSystemVoices";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listSystemVoices-defect" {
        ""
    } else {
        "test-api-key"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListSystemVoicesOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
