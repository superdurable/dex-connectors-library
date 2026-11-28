// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_seniverse as connector;
const CONNECTOR_ID: &str = "seniverse";
const ORIGIN: &str = "https://api.seniverse.com";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let token = if name == "getCurrentWeather-missingCredential" {
        ""
    } else {
        "private-test-key"
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
