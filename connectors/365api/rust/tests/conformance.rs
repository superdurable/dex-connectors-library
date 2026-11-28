// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_365api as connector;

const CONNECTOR_ID: &str = "hby365";
const FIXTURE: &str = "../../../conformance/connectors/365api/scenarios.json";
const ORIGIN: &str = "https://www.hby365.studio";
const KEY: &str = "test-key";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
