// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::{PublicApiClient, failed, generated_definitions};
use dex_connectors_sdk::providerhttp::ProviderHttpClient;
use dex_connectors_sdk::{
    Call, FailureKind, Query, QueryAttempt, QueryDefinition, QueryStep, QueryStepConfig, Receipt,
    Rfc3339NanoTimestamp, ValidationError,
};
use html5gum::{DefaultEmitter, Token, Tokenizer};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use std::{
    collections::BTreeMap,
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

/// Maximum Unicode code points in a search hit's plain text.
pub const MAX_STORY_TEXT_CHARACTERS: usize = 2000;
const SEARCH_ORIGIN: &str = "https://hn.algolia.com/api/v1";
const STORY_TAGS: [&str; 4] = ["story", "show_hn", "ask_hn", "front_page"];

/// Filter one page of public Hacker News results.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct SearchStoriesInput {
    /// Keywords up to 512 UTF-8 bytes. Blank matches every indexed item allowed by the filters.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub query: String,
    /// Relevance or date. Blank selects relevance.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub sort_by: String,
    /// Required story tags. The story tag always applies.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub tags: Vec<String>,
    /// Strict lower creation bound. The zero time omits the filter.
    #[serde(skip_serializing_if = "Rfc3339NanoTimestamp::is_zero")]
    pub created_after: Rfc3339NanoTimestamp,
    /// Strict upper creation bound. The zero time omits the filter.
    #[serde(skip_serializing_if = "Rfc3339NanoTimestamp::is_zero")]
    pub created_before: Rfc3339NanoTimestamp,
    /// Minimum score. Zero omits the filter.
    #[serde(skip_serializing_if = "crate::is_zero")]
    pub min_points: i64,
    /// Zero-based result page within the first 1,000 hits.
    #[serde(skip_serializing_if = "crate::is_zero")]
    pub page: i64,
    /// Requested page size from one to 100. Zero selects 20.
    #[serde(skip_serializing_if = "crate::is_zero")]
    pub hits_per_page: i64,
}

/// One public Hacker News search result.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct StorySearchHit {
    /// Positive Hacker News item ID.
    pub id: i64,
    /// Untrusted indexed title.
    pub title: String,
    /// External source URL, when indexed.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub url: String,
    /// Canonical Hacker News discussion permalink.
    pub discussion_url: String,
    /// Public author username.
    pub author: String,
    /// UTC creation time at second precision.
    pub created_at: Rfc3339NanoTimestamp,
    /// Indexed story score.
    pub points: i64,
    /// Indexed comment count.
    pub comment_count: i64,
    /// Recognized story tags in canonical order.
    pub tags: Vec<String>,
    /// Plain submission text with paragraph breaks.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub story_text: String,
    /// Whether the original plain text exceeded the character limit.
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub is_story_text_truncated: bool,
}

/// One result page from Hacker News Search.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct StorySearchPage {
    /// Indexed hits in requested order.
    pub stories: Vec<StorySearchHit>,
    /// Zero-based result page within the first 1,000 hits.
    pub page: i64,
    /// Number of readable pages.
    pub pages: i64,
    /// Estimated total matches.
    pub total_hits: i64,
    /// Requested page size from one to 100. Zero selects 20.
    pub hits_per_page: i64,
    /// Next page number, or zero after the last page.
    pub next_page: i64,
}

/// Searches one bounded page of indexed Hacker News story results.
pub struct SearchStoriesOperation(PublicApiClient);

impl SearchStoriesOperation {
    /// Use the public Search API and manifest request limits.
    pub fn new() -> Result<Self, ValidationError> {
        Ok(Self(PublicApiClient::new(SEARCH_ORIGIN)?))
    }

    /// Bind a Search endpoint, bounded transport and clock for Retry-After.
    pub fn with_client(
        base_url: &str,
        max_response_bytes: usize,
        http: ProviderHttpClient,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        Ok(Self(PublicApiClient::with_client(
            base_url,
            max_response_bytes,
            http,
            clock,
        )?))
    }
}

/// Build a search Step for the application's named Hacker News connection.
pub fn new_search_stories_step<I>(
    config: QueryStepConfig<I, SearchStoriesOperation>,
) -> Result<QueryStep<I, SearchStoriesOperation>, ValidationError> {
    if config.connection.provider != "hacker-news" {
        return Err(ValidationError::new("Hacker News provider is required"));
    }
    QueryStep::new(config)
}

impl Query for SearchStoriesOperation {
    type Input = SearchStoriesInput;
    type Output = StorySearchPage;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::SearchStories::definition()
    }
    fn invoke(&self, _: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let request = match input.request() {
            Ok(request) => request,
            Err(message) => {
                return failed("searchStories", "defect", FailureKind::Validation, message);
            }
        };
        self.0.read_json("searchStories", &request.path, |body| {
            match request.read_page(body, story_hit) {
                Ok(page) => QueryAttempt::branch(
                    "searched",
                    StorySearchPage {
                        stories: page.hits,
                        page: request.page,
                        pages: page.pages,
                        total_hits: page.total,
                        hits_per_page: request.size,
                        next_page: page.next,
                    },
                    Receipt::default(),
                    None,
                ),
                Err(message) => failed(
                    "searchStories",
                    "invalidResponse",
                    FailureKind::Protocol,
                    message,
                ),
            }
        })
    }
}

/// Filter one page of public Hacker News results.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct SearchItemsInput {
    /// Keywords up to 512 UTF-8 bytes. Blank matches every indexed item allowed by the filters.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub query: String,
    /// Relevance or date. Blank selects relevance.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub sort_by: String,
    /// Story, comment, poll, or pollopt. Blank searches all four.
    #[serde(rename = "type", skip_serializing_if = "String::is_empty")]
    pub item_type: String,
    /// Strict lower creation bound. The zero time omits the filter.
    #[serde(skip_serializing_if = "Rfc3339NanoTimestamp::is_zero")]
    pub created_after: Rfc3339NanoTimestamp,
    /// Zero-based result page within the first 1,000 hits.
    #[serde(skip_serializing_if = "crate::is_zero")]
    pub page: i64,
    /// Requested page size from one to 100. Zero selects 20.
    #[serde(skip_serializing_if = "crate::is_zero")]
    pub hits_per_page: i64,
}

/// One public Hacker News search result.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ItemSearchHit {
    /// Positive Hacker News item ID.
    pub id: i64,
    /// Untrusted indexed title.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub title: String,
    /// External source URL, when indexed.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub url: String,
    /// Canonical Hacker News discussion permalink.
    pub discussion_url: String,
    /// Public author username.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub author: String,
    /// UTC creation time at second precision.
    pub created_at: Rfc3339NanoTimestamp,
    /// First indexed item type present in the provider tags.
    #[serde(rename = "type")]
    pub item_type: String,
    /// Bounded plain submission or comment text.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub text: String,
    /// Direct parent item ID when indexed.
    #[serde(skip_serializing_if = "crate::is_zero")]
    pub parent_id: i64,
    /// Associated story ID when indexed.
    #[serde(skip_serializing_if = "crate::is_zero")]
    pub story_id: i64,
}

/// One result page from Hacker News Search.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ItemSearchPage {
    /// Indexed hits in requested order.
    pub items: Vec<ItemSearchHit>,
    /// Zero-based result page within the first 1,000 hits.
    pub page: i64,
    /// Number of readable pages.
    pub pages: i64,
    /// Estimated total matches.
    pub total_hits: i64,
    /// Requested page size from one to 100. Zero selects 20.
    pub hits_per_page: i64,
    /// Next page number, or zero after the last page.
    pub next_page: i64,
}

/// Searches one bounded page of indexed Hacker News item results.
pub struct SearchItemsOperation(PublicApiClient);

impl SearchItemsOperation {
    /// Use the public Search API and manifest request limits.
    pub fn new() -> Result<Self, ValidationError> {
        Ok(Self(PublicApiClient::new(SEARCH_ORIGIN)?))
    }

    /// Bind a Search endpoint, bounded transport and clock for Retry-After.
    pub fn with_client(
        base_url: &str,
        max_response_bytes: usize,
        http: ProviderHttpClient,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        Ok(Self(PublicApiClient::with_client(
            base_url,
            max_response_bytes,
            http,
            clock,
        )?))
    }
}

/// Build a search Step for the application's named Hacker News connection.
pub fn new_search_items_step<I>(
    config: QueryStepConfig<I, SearchItemsOperation>,
) -> Result<QueryStep<I, SearchItemsOperation>, ValidationError> {
    if config.connection.provider != "hacker-news" {
        return Err(ValidationError::new("Hacker News provider is required"));
    }
    QueryStep::new(config)
}

impl Query for SearchItemsOperation {
    type Input = SearchItemsInput;
    type Output = ItemSearchPage;
    fn definition(&self) -> QueryDefinition {
        generated_definitions::SearchItems::definition()
    }
    fn invoke(&self, _: &Call<'_>, input: &Self::Input) -> QueryAttempt<Self::Output> {
        let request = match input.request() {
            Ok(request) => request,
            Err(message) => {
                return failed("searchItems", "defect", FailureKind::Validation, message);
            }
        };
        self.0.read_json("searchItems", &request.path, |body| {
            match request.read_page(body, |fields| item_hit(fields, &input.item_type)) {
                Ok(page) => QueryAttempt::branch(
                    "searched",
                    ItemSearchPage {
                        items: page.hits,
                        page: request.page,
                        pages: page.pages,
                        total_hits: page.total,
                        hits_per_page: request.size,
                        next_page: page.next,
                    },
                    Receipt::default(),
                    None,
                ),
                Err(message) => failed(
                    "searchItems",
                    "invalidResponse",
                    FailureKind::Protocol,
                    message,
                ),
            }
        })
    }
}

struct SearchRequest {
    path: String,
    page: i64,
    size: i64,
}

impl SearchStoriesInput {
    fn request(&self) -> Result<SearchRequest, &'static str> {
        let mut tags = vec!["story"];
        for tag in &self.tags {
            if !STORY_TAGS.contains(&tag.as_str()) {
                return Err("tags must be story, show_hn, ask_hn, or front_page");
            }
            if !tags.contains(&tag.as_str()) {
                tags.push(tag);
            }
        }
        if self.min_points < 0 {
            return Err("minPoints cannot be negative");
        }
        let after = self.created_after.unix_nanoseconds();
        let before = self.created_before.unix_nanoseconds();
        if !self.created_before.is_zero() && !self.created_after.is_zero() && before <= after {
            return Err("createdBefore must be later than createdAfter");
        }
        let mut filters = after_filter(&self.created_after);
        if !self.created_before.is_zero() {
            let ceiling = -(-before).div_euclid(1_000_000_000);
            filters.push(format!("created_at_i<{ceiling}"));
        }
        if self.min_points > 0 {
            filters.push(format!("points>={}", self.min_points));
        }
        SearchRequest::new(
            &self.query,
            &self.sort_by,
            tags.join(","),
            filters,
            self.page,
            self.hits_per_page,
        )
    }
}

impl SearchItemsInput {
    fn request(&self) -> Result<SearchRequest, &'static str> {
        if !self.item_type.is_empty() && !is_indexed_type(&self.item_type) {
            return Err("Hacker News item type is invalid");
        }
        SearchRequest::new(
            &self.query,
            &self.sort_by,
            self.item_type.clone(),
            after_filter(&self.created_after),
            self.page,
            self.hits_per_page,
        )
    }
}

fn after_filter(time: &Rfc3339NanoTimestamp) -> Vec<String> {
    if time.is_zero() {
        return Vec::new();
    }
    vec![format!(
        "created_at_i>{}",
        time.unix_nanoseconds().div_euclid(1_000_000_000)
    )]
}

fn is_indexed_type(tag: &str) -> bool {
    matches!(tag, "story" | "comment" | "poll" | "pollopt")
}

struct IndexedPage<H> {
    hits: Vec<H>,
    pages: i64,
    total: i64,
    next: i64,
}

impl SearchRequest {
    fn new(
        query: &str,
        sort: &str,
        tags: String,
        filters: Vec<String>,
        page: i64,
        size: i64,
    ) -> Result<Self, &'static str> {
        if query.len() > 512 {
            return Err("query must be at most 512 bytes");
        }
        let endpoint = match sort {
            "" | "relevance" => "/search",
            "date" => "/search_by_date",
            _ => return Err("sortBy must be relevance or date"),
        };
        let size = if size == 0 { 20 } else { size };
        if !(1..=100).contains(&size) || !(0..=999 / size).contains(&page) {
            return Err("search page or page size is outside the readable range");
        }
        let mut parameters = BTreeMap::from([
            ("page", page.to_string()),
            ("hitsPerPage", size.to_string()),
        ]);
        if !query.is_empty() {
            parameters.insert("query", query.to_owned());
        }
        if !tags.is_empty() {
            parameters.insert("tags", tags);
        }
        if !filters.is_empty() {
            parameters.insert("numericFilters", filters.join(","));
        }
        let encoded = parameters
            .into_iter()
            .map(|(key, value)| format!("{key}={}", escape_query_value(&value)))
            .collect::<Vec<_>>()
            .join("&");
        Ok(Self {
            path: format!("{endpoint}?{encoded}"),
            page,
            size,
        })
    }

    fn read_page<H>(
        &self,
        body: &[u8],
        read_hit: impl Fn(SearchFields) -> Result<H, &'static str>,
    ) -> Result<IndexedPage<H>, &'static str> {
        let mut page = SearchFields::read(serde_json::from_slice(body).map_err(|_| MALFORMED)?)?;
        let Value::Array(hits) = page.0.remove("hits").ok_or(MALFORMED)? else {
            return Err(MALFORMED);
        };
        let pages = page.required_integer("nbpages")?;
        let total = page.required_integer("nbhits")?;
        if page.required_integer("page")? != self.page
            || pages < 0
            || total < 0
            || hits.len() > self.size as usize
        {
            return Err("Hacker News Search returned inconsistent paging");
        }
        let hits = hits
            .into_iter()
            .map(|hit| read_hit(SearchFields::read(hit)?))
            .collect::<Result<Vec<_>, _>>()?;
        Ok(IndexedPage {
            hits,
            pages,
            total,
            next: if self.page + 1 < pages {
                self.page + 1
            } else {
                0
            },
        })
    }
}

fn escape_query_value(value: &str) -> String {
    const HEX: &[u8] = b"0123456789ABCDEF";
    let mut output = String::new();
    for byte in value.bytes() {
        if byte.is_ascii_alphanumeric() || b"-_.~".contains(&byte) {
            output.push(char::from(byte));
        } else if byte == b' ' {
            output.push('+');
        } else {
            output.push('%');
            output.push(char::from(HEX[usize::from(byte >> 4)]));
            output.push(char::from(HEX[usize::from(byte & 15)]));
        }
    }
    output
}

const MALFORMED: &str = "Hacker News Search returned a malformed result page";

struct SearchFields(Map<String, Value>);
impl SearchFields {
    fn read(document: Value) -> Result<Self, &'static str> {
        let Value::Object(fields) = document else {
            return Err(MALFORMED);
        };
        Ok(Self(
            fields
                .into_iter()
                .map(|(key, value)| (key.to_ascii_lowercase(), value))
                .collect(),
        ))
    }

    fn string(&self, name: &str) -> Result<&str, &'static str> {
        match self.0.get(name) {
            None | Some(Value::Null) => Ok(""),
            Some(field) => field.as_str().ok_or(MALFORMED),
        }
    }
    fn integer(&self, name: &str) -> Result<i64, &'static str> {
        match self.0.get(name) {
            None | Some(Value::Null) => Ok(0),
            Some(field) => field.as_i64().ok_or(MALFORMED),
        }
    }
    fn required_integer(&self, name: &str) -> Result<i64, &'static str> {
        self.0.get(name).and_then(Value::as_i64).ok_or(MALFORMED)
    }
    fn tags(&self) -> Result<Vec<&str>, &'static str> {
        match self.0.get("_tags") {
            None | Some(Value::Null) => Ok(Vec::new()),
            Some(Value::Array(tags)) => tags
                .iter()
                .map(|tag| {
                    if tag.is_null() {
                        Ok("")
                    } else {
                        tag.as_str().ok_or(MALFORMED)
                    }
                })
                .collect(),
            _ => Err(MALFORMED),
        }
    }
    fn id(&self) -> Result<i64, &'static str> {
        self.string("objectid")?
            .parse::<i64>()
            .ok()
            .filter(|id| *id > 0)
            .ok_or(MALFORMED)
    }
    fn created_at(&self) -> Result<Rfc3339NanoTimestamp, &'static str> {
        let seconds = self.required_integer("created_at_i")?;
        if !(1..=253402300799).contains(&seconds) {
            return Err(MALFORMED);
        }
        Ok(Rfc3339NanoTimestamp::from_system_time_utc(
            UNIX_EPOCH + Duration::from_secs(seconds as u64),
        ))
    }
}

fn story_hit(fields: SearchFields) -> Result<StorySearchHit, &'static str> {
    let id = fields.id()?;
    let tags = fields.tags()?;
    let (story_text, is_story_text_truncated) = indexed_text(fields.string("story_text")?);
    Ok(StorySearchHit {
        id,
        title: fields.string("title")?.into(),
        url: fields.string("url")?.into(),
        author: fields.string("author")?.into(),
        discussion_url: format!("https://news.ycombinator.com/item?id={id}"),
        created_at: fields.created_at()?,
        points: fields.integer("points")?,
        comment_count: fields.integer("num_comments")?,
        tags: STORY_TAGS
            .into_iter()
            .filter(|tag| tags.contains(tag))
            .map(str::to_owned)
            .collect(),
        story_text,
        is_story_text_truncated,
    })
}

fn item_hit(fields: SearchFields, requested_type: &str) -> Result<ItemSearchHit, &'static str> {
    let id = fields.id()?;
    let item_type = fields
        .tags()?
        .into_iter()
        .find(|tag| is_indexed_type(tag))
        .ok_or(MALFORMED)?;
    if !requested_type.is_empty() && requested_type != item_type {
        return Err("Hacker News Search returned an unexpected item type");
    }
    let title = fields.string("title")?;
    let story_title = fields.string("story_title")?;
    let comment = fields.string("comment_text")?;
    let story = fields.string("story_text")?;
    Ok(ItemSearchHit {
        id,
        item_type: item_type.into(),
        title: if title.is_empty() { story_title } else { title }.into(),
        url: fields.string("url")?.into(),
        author: fields.string("author")?.into(),
        discussion_url: format!("https://news.ycombinator.com/item?id={id}"),
        created_at: fields.created_at()?,
        parent_id: fields.integer("parent_id")?,
        story_id: fields.integer("story_id")?,
        text: indexed_text(if item_type == "comment" {
            comment
        } else {
            story
        })
        .0,
    })
}

fn indexed_text(html: &str) -> (String, bool) {
    let mut emitter = DefaultEmitter::<()>::default();
    emitter.naively_switch_states(true);
    let mut paragraphs = Vec::new();
    let mut current = String::new();
    for token in Tokenizer::new_with_emitter(html, emitter) {
        let tag = match token.expect("reading an in-memory string is infallible") {
            Token::String(value) => {
                current.push_str(&String::from_utf8_lossy(&value));
                continue;
            }
            Token::StartTag(tag) => tag.name,
            Token::EndTag(tag) => tag.name,
            _ => continue,
        };
        if matches!(
            tag.as_slice(),
            b"p" | b"pre" | b"br" | b"div" | b"li" | b"blockquote"
        ) {
            end_paragraph(&mut paragraphs, &mut current);
        }
    }
    end_paragraph(&mut paragraphs, &mut current);
    let text = paragraphs.join("\n\n");
    let mut characters = text.chars();
    let bounded = characters
        .by_ref()
        .take(MAX_STORY_TEXT_CHARACTERS)
        .collect();
    (bounded, characters.next().is_some())
}

fn end_paragraph(paragraphs: &mut Vec<String>, current: &mut String) {
    let text = current.split_whitespace().collect::<Vec<_>>().join(" ");
    if !text.is_empty() {
        paragraphs.push(text);
    }
    current.clear();
}
