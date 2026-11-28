// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListClustersInput as QueryInput;
use dex_connector_zilliz as connector;
const ORIGIN: &str = "https://api.cloud.zilliz.com";
const CONNECTOR_ID: &str = "zilliz";
const OPERATION_ID: &str = "listClusters";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listClusters-defect" {
        ""
    } else {
        "test-secret"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListClustersOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
