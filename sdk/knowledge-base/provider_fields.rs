// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

pub(super) fn decode_provider_fields<T: serde::de::DeserializeOwned>(
    body: &[u8],
) -> Result<T, serde_json::Error> {
    let mut response: serde_json::Value = serde_json::from_slice(body)?;
    lowercase_fields(&mut response);
    serde_json::from_value(response)
}

fn lowercase_fields(value: &mut serde_json::Value) {
    match value {
        serde_json::Value::Array(values) => values.iter_mut().for_each(lowercase_fields),
        serde_json::Value::Object(fields) => {
            *fields = std::mem::take(fields)
                .into_iter()
                .map(|(name, mut value)| {
                    lowercase_fields(&mut value);
                    (name.to_ascii_lowercase(), value)
                })
                .collect();
        }
        _ => (),
    }
}
