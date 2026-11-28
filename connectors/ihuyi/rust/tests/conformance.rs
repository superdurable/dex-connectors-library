// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_ihuyi as connector;
const CONNECTOR_ID: &str = "ihuyi";
const ORIGIN: &str = "https://api.ihuyi.com";
fn scenario_credentials(_: &str) -> connector::Credentials {
    connector::Credentials {
        api_id: "test-account".into(),
        api_key: dex_connectors_sdk::SecretString::new("private-test-key"),
    }
}

#[path = "support/ip_location.rs"]
mod ip_location;
