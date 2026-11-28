// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_juhe as connector;
const CONNECTOR_ID: &str = "juhe";
const ORIGIN: &str = "https://apis.juhe.cn";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let token = if name == "getCurrentWeather-missingCredential" {
        ""
    } else {
        "test-juhe-api-key"
    };
    dex_connectors_sdk::SecretString::new(token).into()
}

use connector::{
    GetCurrentWeatherInput as QueryInput, GetCurrentWeatherOperation as QueryOperation,
};
const OPERATION_ID: &str = "getCurrentWeather";

#[path = "support/http_query.rs"]
mod http_query;
