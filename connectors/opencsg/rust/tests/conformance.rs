// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListGatewayModelsInput as QueryInput;
use dex_connector_opencsg as connector;
const ORIGIN: &str = "https://ai.space.opencsg.com";
const CONNECTOR_ID: &str = "opencsg";
const OPERATION_ID: &str = "listGatewayModels";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials::new(match name {
        "listGatewayModels-defect" => String::new(),
        "listGatewayModels-longKey" => "x".repeat(1024),
        "listGatewayModels-nonAsciiKey" => "密钥".into(),
        _ => "test-secret".into(),
    })
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, _| {
        connector::ListGatewayModelsOperation::with_http_client(credentials, http, limit).unwrap()
    });
}
