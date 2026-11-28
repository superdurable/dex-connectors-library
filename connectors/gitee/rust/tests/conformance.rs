// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetRepositoryInput as QueryInput;
use dex_connector_gitee as connector;
const ORIGIN: &str = "https://gitee.com";
const CONNECTOR_ID: &str = "gitee";
const OPERATION_ID: &str = "getRepository";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "getRepository-defect" {
        ""
    } else {
        "test-gitee-token"
    })
}
#[test]
fn replay_go_repository_queries() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::GetRepositoryOperation::with_http_client(ORIGIN, credentials, http, limit)
            .unwrap()
    });
}
