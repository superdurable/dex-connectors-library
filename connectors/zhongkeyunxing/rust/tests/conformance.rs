// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_zhongkeyunxing as connector;

const CONNECTOR_ID: &str = "zhongkeyunxing";
const FIXTURE: &str = "../../../conformance/connectors/zhongkeyunxing/scenarios.json";
const ORIGIN: &str = "https://www.zhongkeyunx.com";
const KEY: &str = "sk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-defect";
const SMALL_BODY_CASE: &str = "listModels-oversized";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
