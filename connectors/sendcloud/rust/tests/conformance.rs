// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetAccountBalanceInput as QueryInput;
use dex_connector_sendcloud as connector;
const ORIGIN: &str = "https://api.sendcloud.net";
const CONNECTOR_ID: &str = "sendcloud";
const OPERATION_ID: &str = "getAccountBalance";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        api_user: dex_connectors_sdk::SecretString::new(if name.ends_with("-defect") {
            ""
        } else {
            "test-api-user"
        }),
        api_key: dex_connectors_sdk::SecretString::new("test-api-key"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|name, credentials, http, _, _clock| {
        connector::GetAccountBalanceOperation::with_http_client(
            credentials,
            http,
            if name.ends_with("-oversized") {
                8
            } else {
                262144
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
