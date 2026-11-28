// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_maiyun as connector;
const CONNECTOR_ID: &str = "maiyun";
const ORIGIN: &str = "https://lts.maiyun.net";
const KEY: &str = "private-test-key";

#[path = "support/ip_location.rs"]
mod ip_location;
