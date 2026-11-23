// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Go `encoding/json.Marshal` compatible compact JSON bytes.
//!
//! Digests and storage keys shared with sdkgo hash these exact bytes: no insignificant
//! whitespace, struct fields in declaration order (serialize a struct whose field order
//! matches the Go struct), map keys sorted (use `BTreeMap`), and HTML-sensitive `<`, `>`, `&`,
//! U+2028, and U+2029 escaped as lowercase `\u` sequences.

use std::io;

use serde::Serialize;
use serde_json::ser::{CharEscape, Formatter, Serializer};

/// Encodes `value` as Go `encoding/json.Marshal` would.
///
/// # Errors
///
/// Returns a serde error when `value` cannot be serialized.
pub fn to_go_json_vec<T: Serialize + ?Sized>(value: &T) -> Result<Vec<u8>, serde_json::Error> {
    let mut output = Vec::new();
    let mut serializer = Serializer::with_formatter(&mut output, GoJsonFormatter);
    value.serialize(&mut serializer)?;
    Ok(output)
}

/// Encodes `value` as a Go `encoding/json.Marshal` string.
///
/// # Errors
///
/// Returns a serde error when `value` cannot be serialized.
pub fn to_go_json_string<T: Serialize + ?Sized>(value: &T) -> Result<String, serde_json::Error> {
    let bytes = to_go_json_vec(value)?;
    String::from_utf8(bytes).map_err(|error| {
        serde::ser::Error::custom(format!("encoded JSON is not UTF-8: {error}"))
    })
}

/// Compact formatter that adds Go's HTML-safe escapes.
#[derive(Clone, Copy, Debug, Default)]
pub struct GoJsonFormatter;

impl Formatter for GoJsonFormatter {
    fn write_string_fragment<W: ?Sized + io::Write>(
        &mut self,
        writer: &mut W,
        fragment: &str,
    ) -> io::Result<()> {
        let mut start = 0;
        for (index, character) in fragment.char_indices() {
            let escape = match character {
                '<' => "\\u003c",
                '>' => "\\u003e",
                '&' => "\\u0026",
                '\u{2028}' => "\\u2028",
                '\u{2029}' => "\\u2029",
                _ => continue,
            };
            writer.write_all(&fragment.as_bytes()[start..index])?;
            writer.write_all(escape.as_bytes())?;
            start = index + character.len_utf8();
        }
        writer.write_all(&fragment.as_bytes()[start..])
    }

    fn write_char_escape<W: ?Sized + io::Write>(
        &mut self,
        writer: &mut W,
        char_escape: CharEscape,
    ) -> io::Result<()> {
        let escaped: &[u8] = match char_escape {
            CharEscape::Quote => b"\\\"",
            CharEscape::ReverseSolidus => b"\\\\",
            CharEscape::Solidus => b"\\/",
            CharEscape::Backspace => b"\\b",
            CharEscape::FormFeed => b"\\f",
            CharEscape::LineFeed => b"\\n",
            CharEscape::CarriageReturn => b"\\r",
            CharEscape::Tab => b"\\t",
            CharEscape::AsciiControl(byte) => {
                const HEX_DIGITS: &[u8; 16] = b"0123456789abcdef";
                let bytes = [
                    b'\\',
                    b'u',
                    b'0',
                    b'0',
                    HEX_DIGITS[usize::from(byte >> 4)],
                    HEX_DIGITS[usize::from(byte & 0xF)],
                ];
                return writer.write_all(&bytes);
            }
        };
        writer.write_all(escaped)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn escapes_html_sensitive_characters_like_go() {
        let encoded = to_go_json_string("élément <&> \u{2028} \u{2029} 😀 \"\n\u{1}")
            .expect("string encodes");
        assert_eq!(
            encoded,
            "\"élément \\u003c\\u0026\\u003e \\u2028 \\u2029 😀 \\\"\\n\\u0001\""
        );
    }
}
