// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read public Hacker News feeds from Dex Query Steps.

#[rustfmt::skip]
mod generated_definitions;

use dex_connectors_sdk as sdk;
use sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, parse_retry_after,
    read_bounded_body, validate_base_url,
};
use sdk::{
    Call, Failure, FailureKind, Query, QueryAttempt, QueryDefinition, QueryStep, QueryStepConfig,
    Receipt, Rfc3339NanoTimestamp, ValidationError,
};
use serde::{Deserialize, Serialize};
use std::{
    sync::Arc,
    time::{Duration, SystemTime},
};

/// Select a public Hacker News feed and the maximum number of IDs to return.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ListStoryIDsInput {
    /// One of top, best, new, ask, or show. An empty selection uses top.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub feed: String,
    /// Maximum IDs to return, from one to 500. Zero uses fifty.
    #[serde(skip_serializing_if = "is_zero")]
    pub limit: i64,
}

fn is_zero(limit: &i64) -> bool {
    *limit == 0
}

/// Feed snapshot in provider order, observed during the successful Execute attempt.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StoryIDs {
    /// First IDs in feed order, bounded by the selected limit.
    pub ids: Vec<i64>,
    /// Whether the provider returned more IDs than the selected limit.
    pub has_more: bool,
    /// UTC timestamp captured after reading the complete response.
    pub observed_at: Rfc3339NanoTimestamp,
}

/// Query operation that makes one public Firebase GET per Execute attempt.
pub struct ListStoryIDsOperation {
    base_url: String,
    max_response_bytes: usize,
    http: ProviderHttpClient,
    clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
}

impl ListStoryIDsOperation {
    /// Use the public Firebase v0 endpoint and the manifest timeout and size limit.
    pub fn new() -> Result<Self, ValidationError> {
        Self::with_client(
            "https://hacker-news.firebaseio.com/v0",
            1 << 20,
            ProviderHttpClient::new(Duration::from_secs(15), AddressPolicy::PublicHttps)?,
            Arc::new(SystemTime::now),
        )
    }

    /// Bind a validated base URL, configured transport, positive byte limit, and observation clock.
    pub fn with_client(
        base_url: &str,
        max_response_bytes: usize,
        http: ProviderHttpClient,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        if max_response_bytes == 0 {
            return Err(ValidationError::new("response byte limit must be positive"));
        }
        Ok(Self {
            base_url: validate_base_url(base_url)?,
            max_response_bytes,
            http,
            clock,
        })
    }
}

impl Query for ListStoryIDsOperation {
    type Input = ListStoryIDsInput;
    type Output = StoryIDs;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::ListStoryIDs::definition()
    }

    fn invoke(&self, _: &Call<'_>, input: &ListStoryIDsInput) -> QueryAttempt<StoryIDs> {
        let feed = if input.feed.is_empty() {
            "top"
        } else {
            input.feed.as_str()
        };
        if !matches!(feed, "top" | "best" | "new" | "ask" | "show") {
            return failed(
                "defect",
                FailureKind::Validation,
                "feed must be top, best, new, ask, or show",
            );
        }
        let limit = if input.limit == 0 { 50 } else { input.limit };
        if !(1..=500).contains(&limit) {
            return failed(
                "defect",
                FailureKind::Validation,
                "limit must be from 1 through 500",
            );
        }
        let request =
            ProviderHttpRequest::new("GET", format!("{}/{feed}stories.json", self.base_url))
                .header("Accept", "application/json")
                .header("User-Agent", "superdurable-hacker-news-daily/0.1");
        let Ok(mut response) = self.http.send_traced(&request) else {
            return retry(FailureKind::Availability, "Hacker News request failed", 0.0);
        };
        let status = response.status();
        if matches!(status, 408 | 429 | 500..) {
            let kind = if status == 429 {
                FailureKind::RateLimit
            } else {
                FailureKind::Availability
            };
            let delay = parse_retry_after(
                response.headers().get("Retry-After").unwrap_or(""),
                (self.clock)(),
            );
            return retry(kind, "Hacker News is temporarily unavailable", delay);
        }
        if status != 200 {
            return failed(
                "providerRejected",
                FailureKind::ProviderRejection,
                "Hacker News rejected the request",
            );
        }
        let body = match read_bounded_body(&mut response, self.max_response_bytes) {
            Ok(body) => body,
            Err(ProviderHttpError::BodyTooLarge(_)) => {
                return failed(
                    "invalidResponse",
                    FailureKind::ResponseTooLarge,
                    "Hacker News response exceeded the configured limit",
                );
            }
            Err(_) => {
                return retry(
                    FailureKind::Availability,
                    "Hacker News response was interrupted",
                    0.0,
                );
            }
        };
        let Ok(mut ids) = serde_json::from_slice::<Vec<i64>>(&body) else {
            return failed(
                "invalidResponse",
                FailureKind::Protocol,
                "Hacker News returned an invalid feed",
            );
        };
        if ids.iter().any(|id| *id <= 0) {
            return failed(
                "invalidResponse",
                FailureKind::Protocol,
                "Hacker News returned an invalid item ID",
            );
        }
        let has_more = ids.len() > limit as usize;
        ids.truncate(limit as usize);
        let output = StoryIDs {
            ids,
            has_more,
            observed_at: Rfc3339NanoTimestamp::from_system_time_utc((self.clock)()),
        };
        QueryAttempt::branch("listed", output, Receipt::default(), None)
    }
}

/// Build the feed Query Step with the application's logical Hacker News connection.
pub fn new_list_story_ids_step<I>(
    config: QueryStepConfig<I, ListStoryIDsOperation>,
) -> Result<QueryStep<I, ListStoryIDsOperation>, ValidationError> {
    if config.connection.provider != "hacker-news" {
        return Err(ValidationError::new("Hacker News provider is required"));
    }
    QueryStep::new(config)
}

fn failure(kind: FailureKind, message: &str) -> Failure {
    Failure::new(kind, "hacker-news", "listStoryIDs", message)
}
fn failed(branch: &str, kind: FailureKind, message: &str) -> QueryAttempt<StoryIDs> {
    QueryAttempt::branch(
        branch,
        StoryIDs::default(),
        Receipt::default(),
        Some(failure(kind, message)),
    )
}
fn retry(kind: FailureKind, message: &str, delay: f64) -> QueryAttempt<StoryIDs> {
    QueryAttempt::retry(failure(kind, message), delay)
}
