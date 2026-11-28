// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetAccountBalanceInput as QueryInput;
use dex_connector_textin as connector;
const ORIGIN: &str = "https://api.textin.com";
const CONNECTOR_ID: &str = "textin";
const OPERATION_ID: &str = "getAccountBalance";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        app_id: if name.ends_with("-defect") {
            String::new()
        } else {
            "test-app-id".into()
        },
        secret_code: dex_connectors_sdk::SecretString::new("secret-test-code"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, _, _clock| {
        connector::GetAccountBalanceOperation::with_http_client(credentials, http, 65536).unwrap()
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
