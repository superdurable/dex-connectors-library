// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_alapi as connector;
const CONNECTOR_ID: &str = "alapi";
const ORIGIN: &str = "https://v3.alapi.cn";
fn scenario_credentials(_: &str) -> connector::Credentials {
    dex_connectors_sdk::SecretString::new("test-token").into()
}

#[path = "support/ip_location.rs"]
mod ip_location;
