// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListDepartmentsInput as QueryInput;
use dex_connector_hesi as connector;
const ORIGIN: &str = "https://app.ekuaibao.com";
const CONNECTOR_ID: &str = "hesi";
const OPERATION_ID: &str = "listDepartments";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(_: &str) -> connector::Credentials {
    connector::Credentials {
        app_key: dex_connectors_sdk::SecretString::new("test-app-key"),
        app_security: dex_connectors_sdk::SecretString::new("test-app-security"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, keys, http, _, _| {
        connector::ListDepartmentsOperation::with_base_url(keys, http, 2 << 20, ORIGIN).unwrap()
    });
}
#[test]
fn rejects_untrusted_origin() {
    use std::sync::Arc;
    let config = connector::Config {
        base_url: "https://example.org/prefix".into(),
        ..Default::default()
    };
    let credentials = Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
        Default::default(),
    ));
    assert!(connector::ListDepartmentsOperation::new(config, credentials).is_err());
}
