// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetBalanceInput as QueryInput;
use dex_connector_cloopen as connector;
const ORIGIN: &str = "https://app.cloopen.com:8883";
const CONNECTOR_ID: &str = "cloopen";
const OPERATION_ID: &str = "getBalance";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        account_sid: dex_connectors_sdk::SecretString::new(
            if name.ends_with("-missing-credentials") {
                ""
            } else {
                "abcdefghijklmnopqrstuvwxyz012345"
            },
        ),
        auth_token: dex_connectors_sdk::SecretString::new("012345abcdefghijklmnopqrstuvwxyz"),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|name, credentials, http, _, clock| {
        connector::GetBalanceOperation::with_clock(
            credentials,
            http,
            if name.ends_with("-oversize") {
                4
            } else {
                65536
            },
            ORIGIN,
            clock,
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
    assert!(connector::GetBalanceOperation::new(config, credentials).is_err());
}
