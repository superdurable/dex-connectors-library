// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_dimleap as connector;

const CONNECTOR_ID: &str = "dimleap";
const FIXTURE: &str = "../../../conformance/connectors/dimleap/scenarios.json";
const ORIGIN: &str = "https://api.dimleap.cn";
const KEY: &str = "sk-test-secret";
const MISSING_KEY_CASE: &str = "listModels-missing-credentials";
const SMALL_BODY_CASE: &str = "listModels-oversize";

#[path = "../../../../conformance/runners/rust/model_catalog.rs"]
mod model_catalog;
