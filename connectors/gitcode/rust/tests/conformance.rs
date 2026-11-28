// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetRepositoryInput as QueryInput;
use dex_connector_gitcode as connector;
const ORIGIN: &str = "https://api.gitcode.com";
const CONNECTOR_ID: &str = "gitcode";
const OPERATION_ID: &str = "getRepository";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(_: &str) -> connector::Credentials {
    connector::Credentials::new("test-personal-access-token")
}
#[test]
fn replay_go_repository_queries() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::GetRepositoryOperation::with_http_client(ORIGIN, credentials, http, limit)
            .unwrap()
    });
}
