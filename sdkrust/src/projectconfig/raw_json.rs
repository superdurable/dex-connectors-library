// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! JSON kept as exact source text, as Go keeps `json.RawMessage`.

use std::collections::BTreeMap;
use std::fmt;

use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::Value;
use serde_json::value::RawValue;

use super::errors::{ProjectConfigError, ProjectConfigResult};
use super::models::{JsonObject, go_canonical_json_bytes};

/// Members of one JSON object kept raw, as Go's `map[string]json.RawMessage` holds them.
///
/// Keys are sorted, the order Go's `json.Marshal` writes a map in.
pub(crate) type RawJsonMembers = BTreeMap<String, Box<RawValue>>;

/// JSON object settings kept as exact text, as Go keeps a `json.RawMessage`.
///
/// The text is the Go `json.Marshal` form of the source: insignificant whitespace removed and
/// `<`, `>`, `&`, U+2028, and U+2029 escaped, every number lexeme and other string escape
/// unchanged. Rewriting a document therefore keeps values such as `1e400` or
/// `12345678901234567890123` byte for byte.
///
/// Typed access has two forms. [`RawJsonObject::decode`] decodes the exact text into an
/// application type, which reports any value that type cannot hold. [`RawJsonObject::to_json_object`]
/// returns a [`JsonObject`], which holds only integers within the `i64` or `u64` range and
/// numbers within the finite `f64` range; it reports any other number as an error instead of
/// approximating it.
#[derive(Clone)]
pub struct RawJsonObject {
    go_compacted: String,
}

impl RawJsonObject {
    /// Accepts the text of one JSON object, including numbers outside the `f64` range.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] unless `text` is one valid JSON object.
    pub fn from_json_text(text: &str) -> ProjectConfigResult<Self> {
        let not_object = || ProjectConfigError::invalid("JSON settings must be a JSON object");
        let raw: &RawValue = serde_json::from_str(text).map_err(|_| not_object())?;
        if !raw.get().starts_with('{') {
            return Err(not_object());
        }
        Self::from_valid_json(raw.get())
    }

    /// Stores `object` as Go's `json.Marshal` encodes it.
    pub fn from_json_object(object: &JsonObject) -> Self {
        let encoded = go_canonical_json_bytes(&Value::Object(object.clone()))
            .ok()
            .and_then(|bytes| String::from_utf8(bytes).ok());
        Self {
            // A serde_json object always encodes; the fallback is unreachable.
            go_compacted: encoded.unwrap_or_else(|| "{}".to_string()),
        }
    }

    pub(crate) fn from_raw_value(raw: &RawValue) -> ProjectConfigResult<Self> {
        Self::from_json_text(raw.get())
    }

    fn from_valid_json(text: &str) -> ProjectConfigResult<Self> {
        Ok(Self {
            go_compacted: crate::go_json::compact_go_raw_message(text),
        })
    }

    /// Returns the Go `json.Marshal` text of the settings.
    pub fn as_json_text(&self) -> &str {
        &self.go_compacted
    }

    /// Decodes the exact text into `Settings`.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] with the serde error when the text does not
    /// fit `Settings`, for example `1e400` for an `f64` field.
    pub fn decode<Settings: DeserializeOwned>(&self) -> ProjectConfigResult<Settings> {
        serde_json::from_str(self.as_json_text()).map_err(|error| {
            ProjectConfigError::Invalid(format!("JSON settings do not decode: {error}"))
        })
    }

    /// Returns the settings as a [`JsonObject`] without approximating any number.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an integer outside the `i64` and `u64`
    /// ranges or a number outside the finite `f64` range; use [`RawJsonObject::decode`] or
    /// [`RawJsonObject::as_json_text`] for those.
    pub fn to_json_object(&self) -> ProjectConfigResult<JsonObject> {
        if has_integer_beyond_64_bits(self.as_json_text()) {
            return Err(ProjectConfigError::invalid(
                "JSON settings hold an integer outside the 64-bit range a JsonObject keeps exactly",
            ));
        }
        match serde_json::from_str(self.as_json_text()) {
            Ok(Value::Object(object)) => Ok(object),
            _ => Err(ProjectConfigError::invalid(
                "JSON settings hold a number outside the finite f64 range a JsonObject keeps",
            )),
        }
    }
}

/// Reports an integer lexeme that neither `i64` nor `u64` holds; `text` is valid JSON.
fn has_integer_beyond_64_bits(text: &str) -> bool {
    let bytes = text.as_bytes();
    let mut index = 0;
    while index < bytes.len() {
        match bytes[index] {
            b'"' => {
                index += 1;
                while index < bytes.len() && bytes[index] != b'"' {
                    index += if bytes[index] == b'\\' { 2 } else { 1 };
                }
                index += 1;
            }
            b'-' | b'0'..=b'9' => {
                let start = index;
                while index < bytes.len() && b"+-.eE0123456789".contains(&bytes[index]) {
                    index += 1;
                }
                let lexeme = &text[start..index];
                let is_integer = !lexeme.contains(['.', 'e', 'E']);
                if is_integer && lexeme.parse::<i64>().is_err() && lexeme.parse::<u64>().is_err() {
                    return true;
                }
            }
            _ => index += 1,
        }
    }
    false
}

impl fmt::Debug for RawJsonObject {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_tuple("RawJsonObject")
            .field(&self.as_json_text())
            .finish()
    }
}

impl PartialEq for RawJsonObject {
    /// Equal Go encodings are equal settings, as Go compares re-encoded documents.
    fn eq(&self, other: &Self) -> bool {
        self.as_json_text() == other.as_json_text()
    }
}

impl Eq for RawJsonObject {}

impl Serialize for RawJsonObject {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        RawValue::from_string(self.go_compacted.clone())
            .map_err(serde::ser::Error::custom)?
            .serialize(serializer)
    }
}

/// Decodes `text` as one JSON object with raw members.
pub(crate) fn decode_raw_members(text: &str, label: &str) -> ProjectConfigResult<RawJsonMembers> {
    serde_json::from_str(text)
        .map_err(|_| ProjectConfigError::invalid(format!("{label} must be a JSON object")))
}

/// Decodes the raw array member `name` into raw objects; an absent member is `None`.
pub(crate) fn decode_raw_object_array(
    members: &RawJsonMembers,
    name: &str,
    label: &str,
) -> ProjectConfigResult<Option<Vec<RawJsonMembers>>> {
    let Some(raw) = members.get(name) else {
        return Ok(None);
    };
    let items: Vec<Box<RawValue>> = serde_json::from_str(raw.get())
        .map_err(|_| ProjectConfigError::invalid(format!("{name} must be an array")))?;
    items
        .iter()
        .enumerate()
        .map(|(index, item)| decode_raw_members(item.get(), &format!("{label} {index}")))
        .collect::<ProjectConfigResult<_>>()
        .map(Some)
}

/// Decodes one raw member as a [`Value`]; numbers outside the `f64` range are invalid.
pub(crate) fn raw_member_value(
    members: &RawJsonMembers,
    name: &str,
) -> ProjectConfigResult<Option<Value>> {
    members
        .get(name)
        .map(|raw| {
            serde_json::from_str(raw.get()).map_err(|_| {
                ProjectConfigError::invalid(format!(
                    "{name} holds a number outside the finite f64 range"
                ))
            })
        })
        .transpose()
}

/// Splits `members` into typed members, with an object `configuration` as `{}`, and its text.
///
/// `kept_fields` drops other members, as Go's typed decode does.
pub(crate) fn split_raw_configuration(
    members: &RawJsonMembers,
    kept_fields: Option<&[&str]>,
) -> ProjectConfigResult<(JsonObject, Option<RawJsonObject>)> {
    let mut typed = JsonObject::new();
    let mut configuration = None;
    for (name, raw) in members {
        if kept_fields.is_some_and(|fields| !fields.contains(&name.as_str())) {
            continue;
        }
        if name == "configuration" && raw.get().starts_with('{') {
            configuration = Some(RawJsonObject::from_raw_value(raw)?);
            typed.insert(name.clone(), Value::Object(JsonObject::new()));
            continue;
        }
        if let Some(value) = raw_member_value(members, name)? {
            typed.insert(name.clone(), value);
        }
    }
    Ok((typed, configuration))
}
