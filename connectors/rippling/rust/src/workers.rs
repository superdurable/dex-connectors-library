// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::provider_fields::RipplingFields;
use serde::{Deserialize, Serialize};

/// Select one worker by its Rippling ID.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GetWorkerInput {
    /// One to 64 ASCII letters, digits, hyphens or underscores.
    pub worker_id: String,
}
/// Select one page of workers visible to the token owner.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ListWorkersInput {
    /// Page size up to 100. Zero uses the provider default.
    pub limit: i64,
    /// Opaque cursor from an earlier page, up to 2048 bytes. Empty selects the first page.
    pub cursor: String,
}
impl ListWorkersInput {
    pub(super) fn query_suffix(&self) -> Option<String> {
        if !(0..=100).contains(&self.limit) || self.cursor.len() > 2048 {
            return None;
        }
        let mut parameters = form_urlencoded::Serializer::new(String::new());
        if !self.cursor.is_empty() {
            parameters.append_pair("cursor", &self.cursor);
        }
        if self.limit > 0 {
            parameters.append_pair("limit", &self.limit.to_string());
        }
        let query = parameters.finish();
        Some(if query.is_empty() {
            query
        } else {
            format!("?{query}")
        })
    }
}
/// Worker identity, job title and employment status.
#[derive(Clone, Default, Serialize, Deserialize)]
pub struct WorkerSummary {
    /// Stable worker identifier.
    pub id: String,
    /// Job title or an empty string if unavailable.
    pub title: String,
    /// Employment status or an empty string if unavailable.
    pub status: String,
}
impl WorkerSummary {
    pub(super) fn contains_credential(&self, token: &str) -> bool {
        self.id.contains(token) || self.title.contains(token) || self.status.contains(token)
    }
}
/// One directory page in provider order.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkerPage {
    /// Workers visible to the token owner.
    pub workers: Vec<WorkerSummary>,
    /// Cursor to request the next page, empty on the last page.
    pub next_cursor: String,
}
pub(super) fn valid_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 64
        && id
            .bytes()
            .all(|ch| ch.is_ascii_alphanumeric() || ch == b'-' || ch == b'_')
}
fn worker(fields: &RipplingFields) -> Option<WorkerSummary> {
    let id: String = fields.optional("id").ok()??;
    let title = fields.optional("title").ok()?.unwrap_or_default();
    let status = fields.optional("status").ok()?.unwrap_or_default();
    valid_id(&id).then_some(WorkerSummary { id, title, status })
}
pub(super) fn read_profile(body: &[u8], requested: &str) -> Option<WorkerSummary> {
    worker(&serde_json::from_slice(body).ok()?).filter(|summary| summary.id == requested)
}
pub(super) fn read_page(body: &[u8]) -> Option<WorkerPage> {
    let page: RipplingFields = serde_json::from_slice(body).ok()?;
    let rows: Vec<RipplingFields> = page.optional("results").ok()??;
    let link: String = page.optional("next_link").ok()?.unwrap_or_default();
    let next_cursor = if link.is_empty() {
        String::new()
    } else {
        next_cursor(&link)?
    };
    Some(WorkerPage {
        workers: rows.iter().map(worker).collect::<Option<_>>()?,
        next_cursor,
    })
}
fn valid_escapes(value: &str) -> bool {
    let bytes = value.as_bytes();
    bytes.iter().enumerate().all(|(i, &ch)| {
        ch != b'%'
            || bytes
                .get(i + 1..i + 3)
                .is_some_and(|pair| pair.iter().all(u8::is_ascii_hexdigit))
    })
}
fn next_cursor(link: &str) -> Option<String> {
    if link.bytes().any(|b| b < 32 || b == 127) {
        return None;
    }
    let (location, fragment) = link.split_once('#').unwrap_or((link, ""));
    if !valid_escapes(fragment) {
        return None;
    }
    let (path, query) = location.split_once('?')?;
    if !valid_escapes(path) {
        return None;
    }
    url::Url::parse("https://rest.ripplingapis.com")
        .ok()?
        .join(link)
        .ok()?;
    for pair in query
        .split('&')
        .filter(|p| !p.contains(';') && valid_escapes(p))
    {
        if let Some((_, value)) =
            form_urlencoded::parse(pair.as_bytes()).find(|(key, _)| key == "cursor")
        {
            return (!value.is_empty()).then(|| value.into_owned());
        }
    }
    None
}
