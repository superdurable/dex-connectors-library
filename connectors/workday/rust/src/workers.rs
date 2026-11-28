// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::provider_fields::WorkdayFields;
use serde::{Deserialize, Serialize};

/// Look up a worker by its 32-character Workday ID.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GetWorkerInput {
    /// Worker WID from the tenant directory.
    pub worker_id: String,
}
/// One page of workers, optionally restricted by name prefix.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ListWorkersInput {
    /// Page size up to 100. Zero requests Workday's default.
    pub limit: i64,
    /// Zero-based index of the first worker.
    pub offset: i64,
    /// Optional prefix of at least three UTF-8 bytes after trimming.
    pub search: String,
}
impl ListWorkersInput {
    pub(super) fn query_suffix(&self) -> Option<String> {
        if !(0..=100).contains(&self.limit)
            || self.offset < 0
            || (!self.search.is_empty() && self.search.trim().len() < 3)
        {
            return None;
        }
        let mut parameters = form_urlencoded::Serializer::new(String::new());
        if self.limit > 0 {
            parameters.append_pair("limit", &self.limit.to_string());
        }
        if self.offset > 0 {
            parameters.append_pair("offset", &self.offset.to_string());
        }
        if !self.search.is_empty() {
            parameters.append_pair("search", &self.search);
        }
        let query = parameters.finish();
        Some(if query.is_empty() {
            query
        } else {
            format!("?{query}")
        })
    }
}
/// Worker identity and display name from the Common REST directory.
#[derive(Clone, Default, Serialize, Deserialize)]
pub struct WorkerSummary {
    /// Workday WID of the worker.
    pub id: String,
    /// Display name supplied by the tenant.
    pub descriptor: String,
}
/// Matching worker count and summaries in provider order.
#[derive(Clone, Default, Serialize, Deserialize)]
pub struct WorkerPage {
    /// Total across all matching pages.
    pub total: i64,
    /// Workers on this page.
    pub data: Vec<WorkerSummary>,
}
pub(super) fn valid_id(id: &str) -> bool {
    id.len() == 32 && id.bytes().all(|ch| ch.is_ascii_hexdigit())
}
fn worker(fields: &WorkdayFields) -> Option<WorkerSummary> {
    let id: String = fields.optional("id").ok()??;
    let descriptor: String = fields.optional("descriptor").ok()??;
    (valid_id(&id) && !descriptor.is_empty()).then_some(WorkerSummary { id, descriptor })
}
pub(super) fn read_profile(body: &[u8], requested: &str) -> Option<WorkerSummary> {
    worker(&serde_json::from_slice(body).ok()?).filter(|summary| summary.id == requested)
}
pub(super) fn read_page(body: &[u8]) -> Option<WorkerPage> {
    let page: WorkdayFields = serde_json::from_slice(body).ok()?;
    let total: i64 = page.optional("total").ok()?.unwrap_or_default();
    if total < 0 {
        return None;
    }
    let rows: Vec<WorkdayFields> = page.optional("data").ok()??;
    Some(WorkerPage {
        total,
        data: rows.iter().map(worker).collect::<Option<_>>()?,
    })
}
