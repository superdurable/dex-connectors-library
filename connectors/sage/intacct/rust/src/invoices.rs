// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::provider_fields::SageIntacctFields;
use serde::{Deserialize, Serialize};

/// The system-assigned key for one Accounts Receivable invoice.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GetInvoiceInput {
    /// The key returned by Sage Intacct.
    pub invoice_key: String,
}
/// Request the first invoice page.
#[derive(Clone, Default, Serialize, Deserialize)]
pub struct ListInvoicesInput {}
/// Stable invoice key and displayed document ID.
#[derive(Clone, Default, Serialize, Deserialize)]
pub struct InvoiceSummary {
    /// System-assigned invoice key.
    pub key: String,
    /// Displayed invoice ID.
    pub id: String,
}
/// One page of invoice identities, in provider order.
#[derive(Clone, Default, Serialize, Deserialize)]
pub struct InvoiceList {
    /// Invoice identities on the returned page.
    pub invoices: Vec<InvoiceSummary>,
    /// Opaque next link. Empty on the last page.
    pub next: String,
}

pub(super) fn valid_key(key: &str) -> bool {
    (1..=128).contains(&key.len())
        && key
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'-'))
}

fn invoice(fields: &SageIntacctFields) -> Option<InvoiceSummary> {
    let key: String = fields.optional("key").ok()??;
    let id: String = fields.optional("id").ok()??;
    (valid_key(&key) && !id.is_empty()).then_some(InvoiceSummary { key, id })
}

pub(super) fn read_invoice(body: &[u8], requested: &str) -> Option<InvoiceSummary> {
    let envelope: SageIntacctFields = serde_json::from_slice(body).ok()?;
    invoice(&envelope.optional("ia::result").ok()??).filter(|invoice| invoice.key == requested)
}

pub(super) fn read_page(body: &[u8]) -> Option<InvoiceList> {
    let page: SageIntacctFields = serde_json::from_slice(body).ok()?;
    let rows: Vec<SageIntacctFields> = page.optional("ia::result").ok()??;
    let metadata = page
        .optional::<SageIntacctFields>("ia::meta")
        .ok()?
        .unwrap_or_default();
    Some(InvoiceList {
        invoices: rows.iter().map(invoice).collect::<Option<_>>()?,
        next: metadata.optional("next").ok()?.unwrap_or_default(),
    })
}
