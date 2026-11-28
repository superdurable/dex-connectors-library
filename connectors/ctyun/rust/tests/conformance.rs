// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListKnowledgeBasesInput as QueryInput;
use dex_connector_ctyun as connector;
const ORIGIN: &str = "https://kqa-global.ctapi.ctyun.cn";
const CONNECTOR_ID: &str = "ctyun";
const OPERATION_ID: &str = "listKnowledgeBases";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    use dex_connectors_sdk::SecretString;
    connector::Credentials {
        access_key: SecretString::new(if name.ends_with("-missing-credentials") {
            ""
        } else {
            "AKIDEXAMPLE"
        }),
        security_key: SecretString::new("SKEXAMPLE"),
        tenant_id: SecretString::new("tenant-example"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|name, credentials, http, limit, clock| {
        {
            let fail = name.ends_with("-id-generation-failure");
            connector::ListKnowledgeBasesOperation::with_signing_sources(
                credentials,
                http,
                if name.ends_with("-oversize") {
                    4
                } else {
                    limit
                },
                ORIGIN,
                clock,
                std::sync::Arc::new(move || {
                    if fail {
                        None
                    } else {
                        uuid::Uuid::parse_str("123e4567-e89b-42d3-a456-426614174000").ok()
                    }
                }),
            )
        }
        .unwrap()
    })
}
