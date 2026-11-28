// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_shinloo as connector;

const CONNECTOR_ID: &str = "shinloo";
const FIXTURE: &str = "../../../conformance/connectors/shinloo/scenarios.json";
const ORIGIN: &str = "https://api.shinloo.com";
const KEY: &str = "sk-sl-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
