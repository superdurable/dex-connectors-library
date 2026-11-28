// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::GetAccountBalanceInput as QueryInput;
use dex_connector_fanyigou as connector;
const ORIGIN: &str = "https://www.fanyigou.com";
const CONNECTOR_ID: &str = "fanyigou";
const OPERATION_ID: &str = "getAccountBalance";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    use dex_connectors_sdk::SecretString;
    connector::Credentials {
        app_id: SecretString::new(if name.ends_with("-defect") {
            ""
        } else {
            "test-app"
        }),
        private_key: SecretString::new("test-private-key"),
    }
}
#[test]
fn replay_go_query() {
    use sha2::{Digest, Sha256};
    let fixture = std::env::var("CONNECTOR_CONFORMANCE_FIXTURE").unwrap_or_else(|_| {
        concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json").into()
    });
    let document: serde_json::Value =
        serde_json::from_slice(&std::fs::read(fixture).unwrap()).unwrap();
    let seeds: std::collections::HashMap<_, _> = document["scenarios"]
        .as_array()
        .unwrap()
        .iter()
        .map(|scenario| {
            (
                scenario["name"].as_str().unwrap(),
                scenario["randomSeed"].as_str().unwrap_or(""),
            )
        })
        .collect();
    http_query::replay_with(|name, credentials, http, limit, _| {
        let digest = Sha256::new()
            .chain_update(seeds[name])
            .chain_update([0; 8])
            .finalize();
        let nonce: [u8; 16] = digest[..16].try_into().unwrap();
        let available = !name.ends_with("-nonceUnavailable");
        connector::GetAccountBalanceOperation::with_nonce_source(
            credentials,
            http,
            limit,
            std::sync::Arc::new(move || available.then_some(nonce)),
        )
        .unwrap()
    });
}
