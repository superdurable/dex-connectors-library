// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_ipdatacloud as connector;
const CONNECTOR_ID: &str = "ipdatacloud";
const ORIGIN: &str = "https://api.ipdatacloud.com";
const KEY: &str = "private-test-key";

#[path = "support/ip_location.rs"]
mod ip_location;
