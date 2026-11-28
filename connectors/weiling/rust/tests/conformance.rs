// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListDepartmentsInput as QueryInput;
use dex_connector_weiling as connector;
const ORIGIN: &str = "https://openapi.weiling.cn";
const CONNECTOR_ID: &str = "weiling";
const OPERATION_ID: &str = "listDepartments";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        app_id: dex_connectors_sdk::SecretString::new(if name.ends_with("-defect") {
            ""
        } else {
            "REDACTED_APP_ID"
        }),
        app_secret: dex_connectors_sdk::SecretString::new("REDACTED_APP_SECRET"),
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
