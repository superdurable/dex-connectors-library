// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_flurouter as connector;

const CONNECTOR_ID: &str = "flurouter";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
const ORIGIN: &str = "https://flurouter.net";
const KEY: &str = "sk-il-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "support/model_catalog.rs"]
mod model_catalog;
