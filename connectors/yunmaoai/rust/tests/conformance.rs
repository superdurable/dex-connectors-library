// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_yunmaoai as connector;

const CONNECTOR_ID: &str = "yunmaoai";
const FIXTURE: &str = "../../../conformance/connectors/yunmaoai/scenarios.json";
const ORIGIN: &str = "https://api.yunmaoai.com";
const KEY: &str = "test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
