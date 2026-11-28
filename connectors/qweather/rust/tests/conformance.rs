// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_qweather as connector;
const CONNECTOR_ID: &str = "qweather";
const ORIGIN: &str = "https://abc123.xy.qweatherapi.com";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let token = if name == "getCurrentWeather-missingCredential" {
        ""
    } else {
        "private-test-key"
    };
    connector::Credentials {
        api_host: dex_connectors_sdk::SecretString::new(
            if name == "getCurrentWeather-invalidHost" {
                "spoof.qweatherapi.com.example"
            } else {
                "abc123.xy.qweatherapi.com"
            },
        ),
        api_key: dex_connectors_sdk::SecretString::new(token),
    }
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
