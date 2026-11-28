// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connector_ipip as connector;
const CONNECTOR_ID: &str = "ipip";
const ORIGIN: &str = "https://ipapi.ipip.net";
fn scenario_credentials(name: &str) -> connector::Credentials {
    let token = if name == "lookupIPLocation-missingCredential" {
        ""
    } else {
        "test-secret"
    };
    dex_connectors_sdk::SecretString::new(token).into()
}

use connector::{LookupIPLocationInput as QueryInput, LookupIPLocationOperation as QueryOperation};
const OPERATION_ID: &str = "lookupIPLocation";

#[path = "support/http_query.rs"]
mod http_query;
