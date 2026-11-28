// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_yikeapi as connector;
const CONNECTOR_ID: &str = "yikeapi";
const ORIGIN: &str = "https://apia.yikeapi.com";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let app_secret = if name == "lookupIPLocation-defect" {
        ""
    } else {
        "private-test-key"
    };
    connector::Credentials {
        app_id: dex_connectors_sdk::SecretString::new("private-test-id"),
        app_secret: dex_connectors_sdk::SecretString::new(app_secret),
    }
}

use connector::{LookupIPLocationInput as QueryInput, LookupIPLocationOperation as QueryOperation};
const OPERATION_ID: &str = "lookupIPLocation";

#[path = "support/http_query.rs"]
mod http_query;
