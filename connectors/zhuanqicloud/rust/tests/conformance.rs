// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_zhuanqicloud as connector;

const CONNECTOR_ID: &str = "zhuanqicloud";
const FIXTURE: &str = "../../../conformance/connectors/zhuanqicloud/scenarios.json";
const ORIGIN: &str = "https://zhuanqicloud.com";
const KEY: &str = "sk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
