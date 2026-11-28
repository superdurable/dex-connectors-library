// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListWarehousesInput as QueryInput;
use dex_connector_yanwen as connector;
const ORIGIN: &str = "https://open.yw56.com.cn";
const CONNECTOR_ID: &str = "yanwen";
const OPERATION_ID: &str = "listWarehouses";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        user_id: "customer-1".into(),
        api_token: dex_connectors_sdk::SecretString::new(if name.ends_with("-defect") {
            ""
        } else {
            "REDACTED_API_TOKEN"
        }),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|name, credentials, http, limit, clock| {
        let config = connector::Config {
            base_url: ORIGIN.into(),
            max_response_bytes: if name.ends_with("-oversized") {
                8
            } else {
                limit
            },
            ..Default::default()
        };
        connector::ListWarehousesOperation::with_transport(config, credentials, http, clock)
            .unwrap()
    });
}
#[test]
fn accepts_proxy_base_url_with_path() {
    use std::sync::Arc;
    let config = connector::Config {
        base_url: "https://example.org/prefix".into(),
        ..Default::default()
    };
    let credentials = Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
        Default::default(),
    ));
    assert!(connector::ListWarehousesOperation::new(config, credentials).is_ok());
}
