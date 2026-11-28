// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_modelbest as connector;

const CONNECTOR_ID: &str = "modelbest";
const FIXTURE: &str = "../../../conformance/connectors/modelbest/scenarios.json";
const ORIGIN: &str = "https://api.modelbest.cn";
const KEY: &str = "test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
