// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_newayz as connector;
const CONNECTOR_ID: &str = "newayz";
const ORIGIN: &str = "https://api.newayz.com";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let token = if name == "getCurrentWeather-missingCredential" {
        ""
    } else {
        "test-newayz-access-key"
    };
    dex_connectors_sdk::SecretString::new(token).into()
}

use connector::{
    GetCurrentWeatherInput as QueryInput, GetCurrentWeatherOperation as QueryOperation,
};
const OPERATION_ID: &str = "getCurrentWeather";

#[path = "support/http_query.rs"]
mod http_query;

#[test]
fn replay_go_query() {
    http_query::replay_with(|_, credentials, http, limit, clock| {
        QueryOperation::with_http_client_and_clock(credentials, http, limit, clock).unwrap()
    });
}
