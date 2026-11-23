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
/// returns a [`JsonObject`], which holds integral values within the `i64` or `u64` range
/// exactly, whatever their spelling, and other numbers as `f64`. It rounds a fractional value
/// such as `0.1` to an `f64`, and reports a non-finite number, a nonzero number that underflows
/// to zero, or a larger integral number that an `f64` would change as an error instead of
/// changing it.
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

    /// Returns the settings as a [`JsonObject`] without silently distorting any number.
    ///
    /// A number whose exact value is a nonzero integer within the `i64` or `u64` range is kept
    /// exactly as that integer, however it is spelled, so `2.5E+3` becomes `2500`,
    /// `9007199254740993.0` stays `9007199254740993`, and `1000000000000000128e0` stays
    /// `1000000000000000128`. A larger integral value is kept as an `f64` only when that
    /// `f64` and its serialized text both equal it exactly, so `1e22` is accepted and `1e23`
    /// is rejected. Any other number becomes the `f64` that `serde_json` parses: a value with
    /// a fractional part, such as `0.1` or `1.50`, is accepted and rounded to a nearby `f64`
    /// (possibly one unit in the last place from the nearest), and a zero keeps its sign. A
    /// nonzero number that would round to zero, such as `1e-400`, is rejected.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an integer lexeme outside the `i64` and
    /// `u64` ranges, a number outside the finite `f64` range, a nonzero number that underflows
    /// to zero, or a larger integral number that an `f64` or its serialized text would change;
    /// use [`RawJsonObject::decode`] or [`RawJsonObject::as_json_text`] for those.
    pub fn to_json_object(&self) -> ProjectConfigResult<JsonObject> {
        let text = self.as_json_text();
        let lexemes = number_lexemes(text);
        if lexemes
            .iter()
            .any(|(_, lexeme)| is_integer_beyond_64_bits(lexeme))
        {
            return Err(ProjectConfigError::invalid(
                "JSON settings hold an integer outside the 64-bit range a JsonObject keeps exactly",
            ));
        }
        let mut parsed_text = String::with_capacity(text.len());
        let mut copied_until = 0;
        for (start, lexeme) in lexemes {
            if !is_float_lexeme(lexeme) {
                continue;
            }
            let exact = exact_decimal_value(lexeme);
            if let ExactDecimalValue::Integral(digits) = &exact
                && let Some(integer) = integer_within_64_bits(lexeme.starts_with('-'), digits)
            {
                // Parsing in place keeps the integer exact; float parsing could round it.
                parsed_text.push_str(&text[copied_until..start]);
                parsed_text.push_str(&integer);
                copied_until = start + lexeme.len();
                continue;
            }
            if let Some(distortion) = f64_distortion(lexeme, &exact) {
                return Err(ProjectConfigError::invalid(distortion));
            }
        }
        parsed_text.push_str(&text[copied_until..]);
        let Ok(Value::Object(object)) = serde_json::from_str(&parsed_text) else {
            return Err(ProjectConfigError::invalid(
                "JSON settings hold a number outside the finite f64 range a JsonObject keeps",
            ));
        };
        Ok(object)
    }
}

/// Exact value of a float lexeme, from its decimal digits rather than any `f64`.
enum ExactDecimalValue {
    Zero,
    Fractional,
    /// Magnitude digits without leading zeros.
    Integral(String),
    /// More integer digits than any finite `f64` holds.
    BeyondF64Digits,
}

/// Returns every number lexeme outside strings with its byte offset; `text` is valid JSON.
fn number_lexemes(text: &str) -> Vec<(usize, &str)> {
    let bytes = text.as_bytes();
    let mut lexemes = Vec::new();
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
                lexemes.push((start, &text[start..index]));
            }
            _ => index += 1,
        }
    }
    lexemes
}

fn is_float_lexeme(lexeme: &str) -> bool {
    lexeme.contains(['.', 'e', 'E'])
}

fn is_integer_beyond_64_bits(lexeme: &str) -> bool {
    !is_float_lexeme(lexeme) && lexeme.parse::<i64>().is_err() && lexeme.parse::<u64>().is_err()
}

/// Returns the signed integer text when `digits` fits `i64` or `u64`.
fn integer_within_64_bits(is_negative: bool, digits: &str) -> Option<String> {
    let integer = if is_negative {
        format!("-{digits}")
    } else {
        digits.to_string()
    };
    (integer.parse::<i64>().is_ok() || integer.parse::<u64>().is_ok()).then_some(integer)
}

/// Describes how the `f64` of a float `lexeme`, or its serialized text, misstates `exact`.
///
/// `None` also covers a lexeme beyond the finite `f64` range, which the full parse reports.
fn f64_distortion(lexeme: &str, exact: &ExactDecimalValue) -> Option<&'static str> {
    const UNDERFLOW: &str =
        "JSON settings hold a nonzero number that rounds to zero as the f64 a JsonObject keeps";
    const INEXACT_INTEGRAL: &str = "JSON settings hold an integral number that the f64 a JsonObject keeps cannot represent exactly";
    let value: f64 = serde_json::from_str(lexeme).ok()?;
    match exact {
        ExactDecimalValue::Zero => None,
        _ if value == 0.0 => Some(UNDERFLOW),
        ExactDecimalValue::Fractional => None,
        ExactDecimalValue::BeyondF64Digits => Some(INEXACT_INTEGRAL),
        ExactDecimalValue::Integral(digits) => {
            let serialized = serde_json::to_string(&value).ok()?;
            let is_serialized_exact = matches!(
                exact_decimal_value(&serialized),
                ExactDecimalValue::Integral(serialized_digits) if serialized_digits == *digits
            );
            (format!("{:.0}", value.abs()) != *digits || !is_serialized_exact)
                .then_some(INEXACT_INTEGRAL)
        }
    }
}

/// Classifies a float `lexeme` by its exact decimal value.
fn exact_decimal_value(lexeme: &str) -> ExactDecimalValue {
    let (mantissa, exponent_text) = lexeme
        .trim_start_matches('-')
        .split_once(['e', 'E'])
        .map_or(
            (lexeme.trim_start_matches('-'), None),
            |(mantissa, exponent)| (mantissa, Some(exponent)),
        );
    let (integer_digits, fraction_digits) = mantissa.split_once('.').unwrap_or((mantissa, ""));
    let all_digits = format!("{integer_digits}{fraction_digits}");
    let significant = all_digits.trim_start_matches('0').trim_end_matches('0');
    if significant.is_empty() {
        return ExactDecimalValue::Zero;
    }
    let Ok(exponent) = exponent_text.map_or(Ok(0), str::parse::<i64>) else {
        return if exponent_text.is_some_and(|text| text.starts_with('-')) {
            ExactDecimalValue::Fractional
        } else {
            ExactDecimalValue::BeyondF64Digits
        };
    };
    let trailing_zeros = all_digits.len() - all_digits.trim_end_matches('0').len();
    let (Ok(trailing_zeros), Ok(fraction_length)) = (
        i64::try_from(trailing_zeros),
        i64::try_from(fraction_digits.len()),
    ) else {
        return ExactDecimalValue::BeyondF64Digits;
    };
    let decimal_scale = exponent
        .saturating_add(trailing_zeros)
        .saturating_sub(fraction_length);
    if decimal_scale < 0 {
        return ExactDecimalValue::Fractional;
    }
    // A finite f64 has at most 309 integer digits, so a longer integer cannot match.
    match usize::try_from(decimal_scale) {
        Ok(zero_count) if significant.len() + zero_count <= 309 => {
            ExactDecimalValue::Integral(format!("{significant}{}", "0".repeat(zero_count)))
        }
        _ => ExactDecimalValue::BeyondF64Digits,
    }
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
