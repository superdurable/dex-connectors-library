// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// StepFun models in the order returned by the authorized catalog.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListModelsOutput {
    /// Projected models with their owner and creation timestamp.
    pub models: Vec<Model>,
}

/// Provider metadata for one available StepFun model.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Model {
    /// Model identifier accepted by the inference API.
    pub id: String,
    /// Model creation time, expressed in Unix seconds.
    pub created_at_unix: i64,
    /// Name of the model's owning organization.
    pub owned_by: String,
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
            let created_at_unix = row.get("created")?.as_i64()?;
            if row.get("object")?.as_str()? != "model"
                || id.is_empty()
                || owner.is_empty()
                || created_at_unix < 0
            {
                return None;
            }
            models.push(Model {
                id: id.to_owned(),
                owned_by: owner.to_owned(),
                created_at_unix,
            });
        }
        Some(Self { models })
    }
}
