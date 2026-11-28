// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListApplicationsInput as QueryInput;
use dex_connector_qingflow as connector;
const ORIGIN: &str = "https://api.qingflow.com";
const CONNECTOR_ID: &str = "qingflow";
const OPERATION_ID: &str = "listApplications";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listApplications-defect" {
        ""
    } else {
        "private-test-key"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListApplicationsOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
