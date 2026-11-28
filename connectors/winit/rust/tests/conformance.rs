// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListWarehousesInput as QueryInput;
use dex_connector_winit as connector;
const ORIGIN: &str = "https://openapi.winit.com.cn";
const CONNECTOR_ID: &str = "winit";
const OPERATION_ID: &str = "listWarehouses";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        app_key: dex_connectors_sdk::SecretString::new("REDACTED_APP_KEY"),
        token: dex_connectors_sdk::SecretString::new(if name.ends_with("-defect") {
            ""
        } else {
            "REDACTED_TOKEN"
        }),
        client_id: dex_connectors_sdk::SecretString::new("REDACTED_CLIENT_ID"),
        client_secret: dex_connectors_sdk::SecretString::new("REDACTED_CLIENT_SECRET"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, clock| {
        let config = connector::Config {
            base_url: ORIGIN.into(),
            platform: "OWNERERP".into(),
            max_response_bytes: limit,
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
        platform: "OWNERERP".into(),
        ..Default::default()
    };
    let credentials = Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
        Default::default(),
    ));
    assert!(connector::ListWarehousesOperation::new(config, credentials).is_ok());
}
