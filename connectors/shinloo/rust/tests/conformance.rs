// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_shinloo as connector;

const CONNECTOR_ID: &str = "shinloo";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
const ORIGIN: &str = "https://api.shinloo.com";
const KEY: &str = "sk-sl-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "support/model_catalog.rs"]
mod model_catalog;
