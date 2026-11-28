// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Moxin models in the order returned by the authorized catalog.
#[derive(Clone, Default, Deserialize, Serialize)]
pub struct ModelPage {
    /// Models accessible to the connection in provider order.
    pub models: Vec<Model>,
}

/// Provider metadata for one available Moxin model.
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Model {
    /// Model identifier accepted by the inference API.
    pub id: String,
}

impl ModelPage {
    pub(crate) fn from_provider_models(
        rows: &[Value],
        contains_credential: impl Fn(&str) -> bool,
    ) -> Option<Self> {
        if rows.len() > 1000 {
            return None;
        }
        let mut models = Vec::with_capacity(rows.len());
        for row in rows {
            let id = row.get("id")?.as_str()?;
            if id.is_empty() || contains_credential(id) {
                return None;
            }
            if row.get("object")?.as_str()? != "model" {
                return None;
            }
            models.push(Model { id: id.to_owned() });
        }
        Some(Self { models })
    }
}
