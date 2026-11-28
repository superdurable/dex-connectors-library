// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use connector::ListVehicleBrandsInput as QueryInput;
use dex_connector_wapi as connector;
const ORIGIN: &str = "https://uaqy.api.storeapi.net";
const CONNECTOR_ID: &str = "wapi";
const OPERATION_ID: &str = "listVehicleBrands";
#[path = "support/http_query.rs"]
mod http_query;
fn scenario_credentials(name: &str) -> connector::Credentials {
    connector::Credentials {
        app_id: "123".into(),
        app_secret: dex_connectors_sdk::SecretString::new(if name == "listVehicleBrands-defect" {
            ""
        } else {
            "REDACTEDAPPSECRET000000000000000"
        }),
    }
}
#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, clock| {
        connector::ListVehicleBrandsOperation::with_http_client(credentials, http, limit, clock)
            .unwrap()
    });
}
