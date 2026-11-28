// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// SiliconFlow models in the order returned by the authorized catalog.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ListModelsOutput {
    /// Models accessible to the connection in provider order.
    pub models: Vec<Model>,
}

/// Provider metadata for one available SiliconFlow model.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Model {
    /// Model identifier accepted by the inference API.
    pub id: String,
    /// Provider object discriminator, omitted when empty.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub object: String,
}

impl ListModelsOutput {
    pub(crate) fn from_provider_models(
        rows: &[Value],
        contains_credential: impl Fn(&str) -> bool,
    ) -> Option<Self> {
        if rows.len() > 10000 {
            return None;
        }
        let mut seen = std::collections::HashSet::with_capacity(rows.len());
        let mut models = Vec::with_capacity(rows.len());
        for row in rows {
            let id = row.get("id")?.as_str()?;
            if id.is_empty() || contains_credential(id) {
                return None;
            }
            if id.len() > 512 || !seen.insert(id) {
                return None;
            }
            let object = match row.get("object") {
                None | Some(Value::Null) => "",
                Some(value) => value.as_str()?,
            };
            if contains_credential(object) {
                return None;
            }
            models.push(Model {
                id: id.to_owned(),
                object: object.to_owned(),
            });
        }
        Some(Self { models })
    }
}
