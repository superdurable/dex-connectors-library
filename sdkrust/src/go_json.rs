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
    String::from_utf8(bytes)
        .map_err(|error| serde::ser::Error::custom(format!("encoded JSON is not UTF-8: {error}")))
}

/// Compacts valid raw JSON as Go's `json.Marshal` does for a nested `json.RawMessage`.
///
/// Go's `appendCompact` with HTML escaping removes insignificant whitespace, escapes `<`,
/// `>`, `&`, U+2028, and U+2029, and copies every other byte, so number lexemes and string
/// escape sequences such as a Unicode escape or `\/` are preserved. `raw` must already be valid JSON.
pub(crate) fn compact_go_raw_message(raw: &str) -> String {
    let mut compacted = String::with_capacity(raw.len());
    let mut is_in_string = false;
    let mut is_escaped = false;
    for character in raw.chars() {
        match character {
            '<' => compacted.push_str("\\u003c"),
            '>' => compacted.push_str("\\u003e"),
            '&' => compacted.push_str("\\u0026"),
            '\u{2028}' => compacted.push_str("\\u2028"),
            '\u{2029}' => compacted.push_str("\\u2029"),
            ' ' | '\t' | '\n' | '\r' if !is_in_string => {}
            _ => compacted.push(character),
        }
        if is_escaped {
            is_escaped = false;
        } else if is_in_string && character == '\\' {
            is_escaped = true;
        } else if character == '"' {
            is_in_string = !is_in_string;
        }
    }
    compacted
}

/// Indents compact JSON as Go's `json.Indent(dst, compact, "", indent)` does.
///
/// Go's `json.MarshalIndent` is `json.Marshal` followed by this step: empty objects and
/// arrays stay `{}` and `[]`, and a colon is followed by one space. `compact` must be valid
/// JSON without insignificant whitespace.
pub(crate) fn indent_go_json(compact: &str, indent: &str) -> String {
    let mut indented = String::with_capacity(compact.len() * 2);
    let mut depth = 0usize;
    let mut needs_indent = false;
    let mut is_in_string = false;
    let mut is_escaped = false;
    let push_newline = |output: &mut String, depth: usize| {
        output.push('\n');
        for _ in 0..depth {
            output.push_str(indent);
        }
    };
    for character in compact.chars() {
        if is_in_string {
            indented.push(character);
            if is_escaped {
                is_escaped = false;
            } else if character == '\\' {
                is_escaped = true;
            } else if character == '"' {
                is_in_string = false;
            }
            continue;
        }
        if needs_indent && character != ']' && character != '}' {
            needs_indent = false;
            depth += 1;
            push_newline(&mut indented, depth);
        }
        match character {
            '"' => {
                is_in_string = true;
                indented.push(character);
            }
            '{' | '[' => {
                needs_indent = true;
                indented.push(character);
            }
            ',' => {
                indented.push(character);
                push_newline(&mut indented, depth);
            }
            ':' => indented.push_str(": "),
            '}' | ']' => {
                if needs_indent {
                    needs_indent = false;
                } else {
                    depth = depth.saturating_sub(1);
                    push_newline(&mut indented, depth);
                }
                indented.push(character);
            }
            _ => indented.push(character),
        }
    }
    indented
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

    #[test]
    fn compacts_raw_messages_like_go_append_compact() {
        // Expected bytes follow Go's encoding/json appendCompact with escape=true.
        let raw = "{ \"a b\" : [ 1.50 , -0.0e-0, 12345678901234567890123 ],\n\t\"s\":\"x \\\" <&> \\u00e9 \\/ \u{2028}\" }";
        assert_eq!(
            compact_go_raw_message(raw),
            "{\"a b\":[1.50,-0.0e-0,12345678901234567890123],\"s\":\"x \\\" \\u003c\\u0026\\u003e \\u00e9 \\/ \\u2028\"}"
        );
    }

    #[test]
    fn indents_like_go_json_indent() {
        // Expected bytes from Go 1.26 json.Indent(&out, compact, "", "  "), run under /tmp.
        let compact = "{\"a\":[],\"b\":{},\"c\":[1,{\"d\":\"x,:{[\\\"\"}],\"e\":1e400}";
        assert_eq!(
            indent_go_json(compact, "  "),
            "{\n  \"a\": [],\n  \"b\": {},\n  \"c\": [\n    1,\n    {\n      \"d\": \"x,:{[\\\"\"\n    }\n  ],\n  \"e\": 1e400\n}"
        );
    }
}
