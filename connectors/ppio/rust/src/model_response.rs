// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// PPIO models in the order returned by the authorized catalog.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListModelsOutput {
    /// Projected models with their owner and creation timestamp.
    pub models: Vec<Model>,
}

/// Provider metadata for one available PPIO model.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Model {
    /// Provider model identifier.
    pub id: String,
    /// Creation time in Unix seconds.
    pub created_at_unix: i64,
    /// Provider organization.
    pub owned_by: String,
    /// Human-readable model name.
    pub display_name: String,
    /// Provider availability status.
    pub status: i64,
    /// Maximum context size in tokens.
    pub context_size: i64,
    /// Supported API endpoint families.
    pub endpoints: Vec<String>,
}

impl ListModelsOutput {
    pub(crate) fn from_provider_models(
        rows: &[Value],
        _contains_credential: impl Fn(&str) -> bool,
    ) -> Option<Self> {
        let mut models = Vec::with_capacity(rows.len());
        for row in rows {
            let id = row.get("id")?.as_str()?;
            let owner = row.get("owned_by")?.as_str()?;
            let display_name = row.get("display_name")?.as_str()?;
            let created_at_unix = row.get("created")?.as_i64()?;
            let status = row.get("status")?.as_i64()?;
            let context_size = row.get("context_size")?.as_i64()?;
            if row.get("object")?.as_str()? != "model"
                || id.is_empty()
                || owner != "ppio"
                || display_name.is_empty()
                || created_at_unix < 0
                || !matches!(status, 1 | 3 | 4)
                || context_size <= 0
            {
                return None;
            }
            let mut endpoints = Vec::new();
            for endpoint in row.get("endpoints")?.as_array()? {
                endpoints.push(if endpoint.is_null() {
                    String::new()
                } else {
                    endpoint.as_str()?.to_owned()
                });
            }
            models.push(Model {
                id: id.to_owned(),
                created_at_unix,
                owned_by: owner.to_owned(),
                display_name: display_name.to_owned(),
                status,
                context_size,
                endpoints,
            });
        }
        Some(Self { models })
    }
}
