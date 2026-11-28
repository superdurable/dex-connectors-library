// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_alapi as connector;
const CONNECTOR_ID: &str = "alapi";
const ORIGIN: &str = "https://v3.alapi.cn";
const KEY: &str = "test-token";

#[path = "support/ip_location.rs"]
mod ip_location;
