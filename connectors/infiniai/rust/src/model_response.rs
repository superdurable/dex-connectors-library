// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// InfiniAI models in the order returned by the authorized catalog.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListModelsOutput {
    /// Projected models with their owner and creation timestamp.
    pub models: Vec<ModelInfo>,
}

/// Provider metadata for one available InfiniAI model.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelInfo {
    /// GenStudio model identifier.
    pub id: String,
    /// Provider model category.
    pub model_type: String,
    /// Maximum context in tokens.
    pub context_length: i64,
    /// Maximum output in tokens.
    pub max_output_length: i64,
}

impl ListModelsOutput {
    pub(crate) fn from_provider_models(
        rows: &[Value],
        contains_credential: impl Fn(&str) -> bool,
    ) -> Option<Self> {
        let mut models = Vec::with_capacity(rows.len());
        for row in rows {
            let id = row.get("id")?.as_str()?;
            let model_type = row.get("model_type")?.as_str()?;
            let context_length = row.get("context_length")?.as_i64()?;
            let max_output_length = row.get("max_output_length")?.as_i64()?;
            row.get("created")?.as_i64()?;
            row.get("owned_by")?.as_str()?;
            if row.get("object")?.as_str()? != "model"
                || id.is_empty()
                || id.len() > 512
                || contains_credential(id)
                || model_type.is_empty()
                || model_type.len() > 128
                || contains_credential(model_type)
                || context_length < 0
                || max_output_length < 0
            {
                return None;
            }
            models.push(ModelInfo {
                id: id.to_owned(),
                model_type: model_type.to_owned(),
                context_length,
                max_output_length,
            });
        }
        Some(Self { models })
    }
}
