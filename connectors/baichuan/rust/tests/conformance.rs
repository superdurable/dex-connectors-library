// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListKnowledgeBasesInput as QueryInput;
use dex_connector_baichuan as connector;
const ORIGIN: &str = "https://api.baichuan-ai.com";
const CONNECTOR_ID: &str = "baichuan";
const OPERATION_ID: &str = "listKnowledgeBases";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new(if name == "listKnowledgeBases-defect" {
        ""
    } else {
        "test-baichuan-key-123"
    })
    .into()
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|name, credentials, http, limit, _clock| {
        connector::ListKnowledgeBasesOperation::with_http_client(
            credentials,
            http,
            if name.ends_with("-oversized") {
                30
            } else {
                limit
            },
        )
        .unwrap()
    })
}
