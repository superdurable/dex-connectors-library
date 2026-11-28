// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashSet;

/// Ordered model IDs visible to the connected FluRouter API key.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListModelsOutput {
    /// Unique printable model IDs, in provider order.
    #[serde(rename = "modelIDs")]
    pub model_ids: Vec<String>,
}

impl ListModelsOutput {
    pub(crate) fn from_provider_models(
        models: &[Value],
        _contains_credential: impl Fn(&str) -> bool,
    ) -> Option<Self> {
        if models.len() > 1000 {
            return None;
        }
        let mut seen = HashSet::with_capacity(models.len());
        let mut model_ids = Vec::with_capacity(models.len());
        for model in models {
            let id = model.get("id")?.as_str()?;
            if model.get("object")?.as_str()? != "model"
                || id.is_empty()
                || id.len() > 256
                || !id.bytes().all(|byte| byte.is_ascii_graphic())
                || !seen.insert(id)
            {
                return None;
            }
            model_ids.push(id.to_owned());
        }
        Some(Self { model_ids })
    }
}
