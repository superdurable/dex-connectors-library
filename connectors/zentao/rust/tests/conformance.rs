// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListProductsInput as QueryInput;
use dex_connector_zentao as connector;
const ORIGIN: &str = "https://zentao.example.org/zentao";
const CONNECTOR_ID: &str = "zentao";
const OPERATION_ID: &str = "listProducts";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(if name == "listProducts-defect" {
        ""
    } else {
        "secretToken"
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListProductsOperation::with_base_url(credentials, http, limit, ORIGIN).unwrap()
    });
}
#[test]
fn requires_installation_root() {
    for url in [
        "",
        "https://example.org/?q=invalid",
        "https://example.org/zentao/api.php",
        "https://example.org/zentao/api.php/v1",
    ] {
        let config = connector::Config {
            base_url: url.into(),
            ..Default::default()
        };
        let credentials = std::sync::Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
            Default::default(),
        ));
        assert!(connector::ListProductsOperation::new(config, credentials).is_err());
    }
}
