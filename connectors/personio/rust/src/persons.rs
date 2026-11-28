// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::provider_fields::PersonioFields;
use serde::{Deserialize, Serialize};

/// The stable Persons API ID to look up.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GetPersonInput {
    /// One to 128 ASCII letters, digits, underscores or hyphens.
    pub person_id: String,
}
/// Optional filters for one page of Personio persons.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ListPersonsInput {
    /// Persons to return, from 1 to 50. Zero uses the provider default.
    pub limit: i64,
    /// Opaque next-page cursor. Empty starts at the first page.
    pub cursor: String,
    /// Exact first name, or empty for all.
    pub first_name: String,
    /// Exact last name, or empty for all.
    pub last_name: String,
    /// ACTIVE or INACTIVE. Empty includes both.
    pub status: String,
}
impl ListPersonsInput {
    pub(super) fn query_suffix(&self) -> Option<String> {
        if !(0..=50).contains(&self.limit)
            || self.cursor.len() > 1024
            || !matches!(self.status.as_str(), "" | "ACTIVE" | "INACTIVE")
        {
            return None;
        }
        let mut query = form_urlencoded::Serializer::new(String::new());
        for (key, value) in [
            ("cursor", &self.cursor),
            ("first_name", &self.first_name),
            ("last_name", &self.last_name),
            ("status", &self.status),
        ] {
            if !value.is_empty() {
                query.append_pair(key, value);
            }
        }
        if self.limit > 0 {
            query.append_pair("limit", &self.limit.to_string());
        }
        let query = query.finish();
        Some(if query.is_empty() {
            query
        } else {
            format!("?{query}")
        })
    }
}
/// ID and names of a person visible to this integration.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PersonProfile {
    /// Stable Persons API ID.
    pub id: String,
    /// First name, empty when omitted.
    pub first_name: String,
    /// Last name, empty when omitted.
    pub last_name: String,
}
/// A single directory page.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PersonPage {
    /// Selected profiles in provider order.
    pub persons: Vec<PersonProfile>,
    /// Opaque cursor for the next page, empty on the last page.
    pub next_cursor: String,
}
pub(super) fn valid_id(id: &str) -> bool {
    (1..=128).contains(&id.len())
        && id
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-'))
}
pub(super) fn read_profile(body: &[u8], requested_id: &str) -> Option<PersonProfile> {
    let record = serde_json::from_slice(body).ok()?;
    profile(record).filter(|p| p.id == requested_id)
}
fn profile(record: PersonioFields) -> Option<PersonProfile> {
    let id = record.optional::<String>("id").ok()??;
    valid_id(&id).then_some(PersonProfile {
        id,
        first_name: record.optional("first_name").ok()?.unwrap_or_default(),
        last_name: record.optional("last_name").ok()?.unwrap_or_default(),
    })
}
pub(super) fn read_page(body: &[u8], base_url: &str) -> Option<PersonPage> {
    let record: PersonioFields = serde_json::from_slice(body).ok()?;
    let persons = record
        .optional::<Vec<PersonioFields>>("_data")
        .ok()??
        .into_iter()
        .map(profile)
        .collect::<Option<_>>()?;
    let mut links = record;
    for key in ["_meta", "links", "next"] {
        links = links.optional(key).ok()?.unwrap_or_default();
    }
    let href = links.optional::<String>("href").ok()?.unwrap_or_default();
    let next_cursor = if href.is_empty() {
        String::new()
    } else {
        next_cursor(&href, base_url)?
    };
    Some(PersonPage {
        persons,
        next_cursor,
    })
}
fn next_cursor(href: &str, base_url: &str) -> Option<String> {
    if href.bytes().any(|b| b.is_ascii_control()) {
        return None;
    }
    let (location, query) = href.split('#').next()?.split_once('?')?;
    let path = if let Some((scheme, after_scheme)) = location.split_once("://") {
        let (base_scheme, base) = base_url.split_once("://")?;
        let (authority, path) = after_scheme.split_once('/')?;
        if !scheme.eq_ignore_ascii_case(base_scheme)
            || authority.rsplit('@').next()? != base.split('/').next()?
        {
            return None;
        }
        path
    } else if let Some(network_path) = location.strip_prefix("//") {
        network_path.split_once('/')?.1
    } else {
        location.strip_prefix('/')?
    };
    if percent_encoding::percent_decode_str(path)
        .decode_utf8()
        .ok()?
        .as_ref()
        != "v2/persons"
    {
        return None;
    }
    for pair in query.split('&') {
        if pair.contains(';')
            || pair.as_bytes().iter().enumerate().any(|(index, byte)| {
                *byte == b'%'
                    && !pair
                        .as_bytes()
                        .get(index + 1..index + 3)
                        .is_some_and(|digits| digits.iter().all(u8::is_ascii_hexdigit))
            })
        {
            continue;
        }
        if let Some((key, value)) = form_urlencoded::parse(pair.as_bytes()).next()
            && key == "cursor"
        {
            return (!value.is_empty()).then(|| value.into_owned());
        }
    }
    None
}
