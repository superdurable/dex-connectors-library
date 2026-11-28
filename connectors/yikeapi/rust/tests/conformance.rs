// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_yikeapi as connector;
const CONNECTOR_ID: &str = "yikeapi";
const ORIGIN: &str = "https://apia.yikeapi.com";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let app_secret = if name == "lookupIPLocation-defect" {
        ""
    } else {
        "private-test-key"
    };
    connector::Credentials {
        app_id: dex_connectors_sdk::SecretString::new("private-test-id"),
        app_secret: dex_connectors_sdk::SecretString::new(app_secret),
    }
}

#[path = "support/ip_location.rs"]
mod ip_location;
