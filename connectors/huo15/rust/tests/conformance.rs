// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_huo15 as connector;

const CONNECTOR_ID: &str = "huo15";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
const ORIGIN: &str = "https://fireworks-simulator-api.huo15.com";
const KEY: &str = "fsk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "support/model_catalog.rs"]
mod model_catalog;
