// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetAccountBalanceInput as QueryInput;
use dex_connector_dieyuyun as connector;
const ORIGIN: &str = "https://open.dieyuyun.com";
const CONNECTOR_ID: &str = "dieyuyun";
const OPERATION_ID: &str = "getAccountBalance";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new(if name == "getAccountBalance-missing-key" {
        ""
    } else {
        "private-test-key"
    })
    .into()
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|name, credentials, http, limit, _clock| {
        connector::GetAccountBalanceOperation::with_http_client(
            credentials,
            http,
            if name.ends_with("-oversize") {
                8
            } else {
                limit
            },
        )
        .unwrap()
    })
}

#[test]
fn rejects_base_url_with_path() {
    use std::sync::Arc;
    let config = connector::Config {
        base_url: "https://example.org/prefix".into(),
        ..Default::default()
    };
    let credentials = Arc::new(dex_connectors_sdk::StaticCredentialProvider::new(
        Default::default(),
    ));
    assert!(connector::GetAccountBalanceOperation::new(config, credentials).is_err());
}
