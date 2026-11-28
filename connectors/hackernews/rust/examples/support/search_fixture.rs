// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connectors_sdk::providerhttp::*;
use std::{
    sync::atomic::{AtomicUsize, Ordering},
    time::Duration,
};

pub(super) struct Responses {
    pub(super) calls: AtomicUsize,
    suffix: &'static str,
}
impl Responses {
    pub(super) fn new(suffix: &'static str) -> Self {
        Self {
            calls: AtomicUsize::new(0),
            suffix,
        }
    }
}
impl ProviderHttpTransport for Responses {
    fn send(
        &self,
        request: &ProviderHttpRequest,
        _: Duration,
    ) -> Result<ProviderHttpResponse, ProviderHttpError> {
        assert_eq!(
            request.url,
            format!(
                "https://hn.algolia.com/api/v1/search?hitsPerPage=2&page=0&query=compiler{}",
                self.suffix
            )
        );
        let status = if self.calls.fetch_add(1, Ordering::SeqCst) == 0 {
            503
        } else {
            200
        };
        let body = br#"{"page":0,"nbPages":2,"nbHits":3,"hits":[{"objectID":"42","created_at_i":1,"_tags":["story"],"title":"Release"}]}"#;
        Ok(ProviderHttpResponse::new(
            status,
            ResponseHeaders::new(vec![]),
            Box::new(std::io::Cursor::new(body)),
        ))
    }
}
