// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListTagsInput as QueryInput;
use dex_connector_weiban as connector;
const ORIGIN: &str = "https://open.weibanzhushou.com";
const CONNECTOR_ID: &str = "weiban";
const OPERATION_ID: &str = "listTags";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        corp_id: (if name == "listTags-defect" {
            ""
        } else {
            "corp_test"
        })
        .into(),
        secret: dex_connectors_sdk::SecretString::new("REDACTED_WEIBAN_SECRET"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListTagsOperation::with_base_url(credentials, http, limit, ORIGIN).unwrap()
    });
}
