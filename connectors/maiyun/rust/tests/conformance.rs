// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_maiyun as connector;
const CONNECTOR_ID: &str = "maiyun";
const ORIGIN: &str = "https://lts.maiyun.net";
fn scenario_credentials(_: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new("private-test-key").into()
}

#[path = "support/ip_location.rs"]
mod ip_location;
