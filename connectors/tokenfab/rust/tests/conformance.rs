// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_tokenfab as connector;

const CONNECTOR_ID: &str = "tokenfab";
const FIXTURE: &str = "../../../conformance/connectors/tokenfab/scenarios.json";
const ORIGIN: &str = "https://api.tokenfab.cn";
const KEY: &str = "tk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
