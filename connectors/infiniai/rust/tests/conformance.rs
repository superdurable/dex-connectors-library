// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_infiniai as connector;

const CONNECTOR_ID: &str = "infiniai";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
const ORIGIN: &str = "https://cloud.infini-ai.com";
const KEY: &str = "sk-secret-api-key";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "support/model_catalog.rs"]
mod model_catalog;
