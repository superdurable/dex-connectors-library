// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_xiaomi as connector;

const CONNECTOR_ID: &str = "xiaomi";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
const ORIGIN: &str = "https://api.xiaomimimo.com";
const KEY: &str = "sk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "support/model_catalog.rs"]
mod model_catalog;
