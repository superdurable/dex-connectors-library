// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connectors_sdk::SecretString;
use dex_connectors_sdk::oauthtoken::ManifestCredential;
use dex_connectors_sdk::projectconfig::{
    ConnectionKey, JsonObject, LoadedProject, ProjectConfigError,
    RefreshingProjectCredentialProvider,
};
use std::sync::Arc;

/// Private Personio credential material. The access token may be empty before the first exchange.
#[derive(Clone)]
pub struct Credentials {
    /// Client ID of the custom integration.
    pub client_id: String,
    /// Client secret used only for token exchange.
    pub client_secret: SecretString,
    /// Renewable access token sent only to the configured Persons API.
    pub access_token: SecretString,
}
impl ManifestCredential for Credentials {
    fn credential_field_names() -> &'static [&'static str] {
        &["client_id", "client_secret", "access_token"]
    }
    fn credential_value(&self, name: &str) -> String {
        match name {
            "client_id" => &self.client_id,
            "client_secret" => self.client_secret.reveal(),
            "access_token" => self.access_token.reveal(),
            _ => "",
        }
        .to_owned()
    }
    fn with_credential_value(&self, name: &str, value: String) -> Self {
        let mut replacement = self.clone();
        match name {
            "client_id" => replacement.client_id = value,
            "client_secret" => replacement.client_secret = SecretString::new(value),
            "access_token" => replacement.access_token = SecretString::new(value),
            _ => {}
        }
        replacement
    }
    fn auth_method_id(&self) -> Option<&str> {
        None
    }
}

/// Bind a Dex project connection. Each query resolves current credentials and the provider coordinates token renewal.
pub fn project_credentials(
    project: &LoadedProject,
    connection_name: &str,
) -> Result<RefreshingProjectCredentialProvider<Credentials>, ProjectConfigError> {
    RefreshingProjectCredentialProvider::new(
        project.connections().clone(),
        ConnectionKey::new("personio", connection_name),
        Arc::new(|fields| decode_credentials(fields, false)),
        Arc::new(|credentials| {
            Ok(Credentials::credential_field_names()
                .iter()
                .map(|&name| (name.to_owned(), credentials.credential_value(name).into()))
                .collect())
        }),
        Some(Arc::new(|fields| decode_credentials(fields, true))),
    )
}
fn decode_credentials(fields: &JsonObject, shared: bool) -> Result<Credentials, String> {
    let mut credentials = Credentials {
        client_id: String::new(),
        client_secret: SecretString::new(""),
        access_token: SecretString::new(""),
    };
    for (name, value) in fields {
        if !Credentials::credential_field_names().contains(&name.as_str()) {
            return Err("unexpected Personio credential field".to_owned());
        }
        if !value.is_null() {
            let text = value.as_str().ok_or("invalid Personio credential field")?;
            credentials = credentials.with_credential_value(name, text.to_owned());
        }
    }
    if credentials.client_id.is_empty()
        || (!shared && credentials.client_secret.reveal().is_empty())
        || (shared && credentials.access_token.reveal().is_empty())
    {
        return Err("incomplete Personio credentials".to_owned());
    }
    Ok(credentials)
}
