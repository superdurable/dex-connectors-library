// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_navinfo as connector;
const CONNECTOR_ID: &str = "navinfo";
const ORIGIN: &str = "https://lbs.navinfo.com";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let token = if name == "lookupIPLocation-missingCredential" {
        ""
    } else {
        "test-secret"
    };
    dex_connectors_sdk::SecretString::new(token).into()
}

#[path = "support/ip_location.rs"]
mod ip_location;
