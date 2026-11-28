// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListWarehousesInput as QueryInput;
use dex_connector_chukou1 as connector;
const ORIGIN: &str = "https://openapi.chukou1.cn";
const CONNECTOR_ID: &str = "chukou1";
const OPERATION_ID: &str = "listWarehouses";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new(if name == "listWarehouses-missing-credentials" {
        ""
    } else {
        "secretTestToken"
    })
    .into()
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|name, credentials, http, limit, _| {
        connector::ListWarehousesOperation::with_base_url(
            credentials,
            http,
            if name.ends_with("-oversize") {
                4
            } else {
                limit
            },
            ORIGIN,
        )
        .unwrap()
    })
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
