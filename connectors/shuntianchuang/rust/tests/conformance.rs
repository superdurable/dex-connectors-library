// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_shuntianchuang as connector;

const CONNECTOR_ID: &str = "shuntianchuang";
const FIXTURE: &str = "../../../conformance/connectors/shuntianchuang/scenarios.json";
const ORIGIN: &str = "https://api-model.shuntianchuang.cn";
const KEY: &str = "sk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
