// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_scnet as connector;

const CONNECTOR_ID: &str = "scnet";
const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/support/scenarios.json");
const ORIGIN: &str = "https://api.scnet.cn";
const KEY: &str = "private-test-key";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "support/model_catalog.rs"]
mod model_catalog;
