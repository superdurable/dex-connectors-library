// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_stepfun as connector;

const CONNECTOR_ID: &str = "stepfun";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
const ORIGIN: &str = "https://api.stepfun.com";
const KEY: &str = "secret-api-key";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "support/model_catalog.rs"]
mod model_catalog;
