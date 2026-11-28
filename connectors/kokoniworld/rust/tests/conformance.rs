// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_kokoniworld as connector;

const CONNECTOR_ID: &str = "kokoniworld";
const FIXTURE: &str = "../../../conformance/connectors/kokoniworld/scenarios.json";
const ORIGIN: &str = "https://api.kokoniworld.com";
const KEY: &str = "sk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
