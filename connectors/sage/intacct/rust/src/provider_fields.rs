// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde::Deserialize;
use serde::de::{MapAccess, Visitor};
use serde_json::value::RawValue;
use std::collections::BTreeMap;

/// Field lookup follows the provider's Go JSON contract.
#[derive(Default)]
pub(super) struct SageIntacctFields(BTreeMap<String, Box<RawValue>>);
impl<'de> Deserialize<'de> for SageIntacctFields {
    fn deserialize<D: serde::Deserializer<'de>>(decoder: D) -> Result<Self, D::Error> {
        struct FieldsVisitor;
        impl<'de> Visitor<'de> for FieldsVisitor {
            type Value = SageIntacctFields;
            fn expecting(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
                f.write_str("a SageIntacct response object")
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
                let mut fields = BTreeMap::new();
                while let Some((name, value)) = map.next_entry::<String, Box<RawValue>>()? {
                    fields.insert(name.to_ascii_lowercase(), value);
                }
                Ok(SageIntacctFields(fields))
            }
        }
        decoder.deserialize_map(FieldsVisitor)
    }
}
impl SageIntacctFields {
    pub(super) fn optional<T: serde::de::DeserializeOwned>(
        &self,
        name: &str,
    ) -> Result<Option<T>, serde_json::Error> {
        match self.0.get(name) {
            Some(raw) => serde_json::from_str(raw.get()),
            None => Ok(None),
        }
    }
}
