// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListProjectsInput as QueryInput;
use dex_connector_worktile as connector;
const ORIGIN: &str = "https://dev.worktile.com";
const CONNECTOR_ID: &str = "worktile";
const OPERATION_ID: &str = "listProjects";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        client_id: dex_connectors_sdk::SecretString::new(if name == "listProjects-defect" {
            ""
        } else {
            "REDACTED_WORKTILE_CLIENT_ID"
        }),
        client_secret: dex_connectors_sdk::SecretString::new("REDACTED_WORKTILE_CLIENT_SECRET"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListProjectsOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
