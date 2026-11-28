// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use serde_json::value::RawValue;
use std::sync::LazyLock;

pub(super) fn amount(raw: &RawValue) -> Option<String> {
    let literal = raw.get();
    let amount = if literal.starts_with('"') {
        serde_json::from_str::<String>(literal).ok()?
    } else {
        literal.to_owned()
    };
    static NUMBER: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(r"\A-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\z")
            .expect("JSON number syntax")
    });
    NUMBER.is_match(&amount).then_some(amount)
}
