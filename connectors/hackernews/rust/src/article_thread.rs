// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use crate::{PublicApiClient, failed, generated_definitions};
use dex_connectors_sdk::providerhttp::ProviderHttpClient;
use dex_connectors_sdk::{
    Call, FailureKind, Query, QueryAttempt, QueryDefinition, QueryStep, QueryStepConfig, Receipt,
    ValidationError,
};
use serde::{Deserialize, Serialize};
use serde_json::value::RawValue;
use std::{sync::Arc, time::SystemTime};

/// Identify an indexed public Hacker News story or poll.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct GetArticleThreadInput {
    /// Positive Hacker News item ID.
    pub id: i64,
}

/// Indexed article with its public comments in provider order.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ArticleThread {
    /// Public Hacker News article ID.
    pub id: i64,
    /// Untrusted title supplied by the provider.
    pub title: String,
    /// Article submitter's public username, when present.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub author: String,
    /// External article URL, when present.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub url: String,
    /// Untrusted HTML of a text submission.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub text_html: String,
    /// Canonical Hacker News discussion permalink.
    pub discussion_url: String,
    /// Public comments in their original nested order.
    pub comments: Vec<ArticleComment>,
}

/// One indexed Hacker News comment and its replies.
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ArticleComment {
    /// Public comment item ID.
    pub id: i64,
    /// Commenter's public username, when present.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub author: String,
    /// Untrusted comment HTML supplied by the provider.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub text_html: String,
    /// Canonical Hacker News comment permalink.
    pub discussion_url: String,
    /// Indexed replies in provider order.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub replies: Vec<ArticleComment>,
}

/// Read one indexed article and its public comments per Execute attempt.
pub struct GetArticleThreadOperation(PublicApiClient);

impl GetArticleThreadOperation {
    /// Use the public Algolia Hacker News Search API and manifest request limits.
    pub fn new() -> Result<Self, ValidationError> {
        Ok(Self(PublicApiClient::new("https://hn.algolia.com/api/v1")?))
    }

    /// Bind a Search API endpoint, bounded transport and clock for Retry-After.
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

/// Build an article thread Step bound to the application's named Hacker News connection.
pub fn new_get_article_thread_step<I>(
    config: QueryStepConfig<I, GetArticleThreadOperation>,
) -> Result<QueryStep<I, GetArticleThreadOperation>, ValidationError> {
    if config.connection.provider != "hacker-news" {
        return Err(ValidationError::new("Hacker News provider is required"));
    }
    QueryStep::new(config)
}

impl Query for GetArticleThreadOperation {
    type Input = GetArticleThreadInput;
    type Output = ArticleThread;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetArticleThread::definition()
    }

    fn invoke(&self, _: &Call<'_>, input: &Self::Input) -> QueryAttempt<ArticleThread> {
        if input.id <= 0 {
            return failed(
                "getArticleThread",
                "defect",
                FailureKind::Validation,
                "article ID must be positive",
            );
        }
        self.0.read_json(
            "getArticleThread",
            &format!("/items/{}", input.id),
            |body| decode_article(body, input.id),
        )
    }
}

const MALFORMED: &str = "Hacker News Search returned malformed article JSON";
const INVALID_TREE: &str = "Hacker News Search returned an invalid comment tree";

fn decode_article(body: &[u8], id: i64) -> QueryAttempt<ArticleThread> {
    let node = match serde_json::from_slice::<Option<ArticleNode>>(body) {
        Ok(None) => {
            return failed(
                "getArticleThread",
                "notFound",
                FailureKind::NotFound,
                "Hacker News article was not found",
            );
        }
        Ok(Some(node)) => node,
        Err(_) => {
            return failed(
                "getArticleThread",
                "invalidResponse",
                FailureKind::Protocol,
                MALFORMED,
            );
        }
    };
    let convert = || {
        if node.id != id || node.title.is_empty() {
            return Err("Hacker News Search returned an invalid article");
        }
        let comments = read_comments(&node.children, id, 0, &mut 5000)?;
        Ok(ArticleThread {
            id,
            title: node.title,
            author: node.author,
            url: node.url,
            text_html: node.text,
            discussion_url: discussion_url(id),
            comments,
        })
    };
    match convert() {
        Ok(thread) => QueryAttempt::branch("found", thread, Receipt::default(), None),
        Err(reason) => failed(
            "getArticleThread",
            "invalidResponse",
            FailureKind::Protocol,
            reason,
        ),
    }
}

#[derive(Default)]
struct ArticleNode<'a> {
    id: i64,
    parent: i64,
    title: String,
    text: String,
    url: String,
    author: String,
    children: Vec<&'a RawValue>,
}

// Decode children separately so the JSON parser's nesting limit cannot reject a valid 63-level comment tree.
impl<'de> Deserialize<'de> for ArticleNode<'de> {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        deserializer.deserialize_map(ArticleNodeVisitor)
    }
}

struct ArticleNodeVisitor;

impl<'de> serde::de::Visitor<'de> for ArticleNodeVisitor {
    type Value = ArticleNode<'de>;
    fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
        formatter.write_str("an indexed Hacker News article or comment")
    }
    fn visit_map<M: serde::de::MapAccess<'de>>(
        self,
        mut fields: M,
    ) -> Result<Self::Value, M::Error> {
        let mut node = ArticleNode::default();
        while let Some(key) = fields.next_key::<String>()? {
            match key.to_ascii_lowercase().as_str() {
                "id" => {
                    if let Some(id) = fields.next_value()? {
                        node.id = id;
                    }
                }
                "parent_id" => {
                    if let Some(id) = fields.next_value()? {
                        node.parent = id;
                    }
                }
                "title" => {
                    if let Some(title) = fields.next_value()? {
                        node.title = title;
                    }
                }
                "url" => {
                    if let Some(url) = fields.next_value()? {
                        node.url = url;
                    }
                }
                "author" => {
                    if let Some(author) = fields.next_value()? {
                        node.author = author;
                    }
                }
                "text" => {
                    if let Some(text) = fields.next_value()? {
                        node.text = text;
                    }
                }
                "children" => {
                    node.children = fields
                        .next_value::<Option<Vec<&RawValue>>>()?
                        .unwrap_or_default()
                }
                _ => {
                    fields.next_value::<serde::de::IgnoredAny>()?;
                }
            }
        }
        Ok(node)
    }
}

fn read_comments(
    children: &[&RawValue],
    parent: i64,
    depth: usize,
    remaining: &mut usize,
) -> Result<Vec<ArticleComment>, &'static str> {
    if depth >= 64 || children.len() > *remaining {
        return Err(INVALID_TREE);
    }
    *remaining -= children.len();
    children
        .iter()
        .map(|child| {
            let node = serde_json::from_str::<Option<ArticleNode>>(child.get())
                .map_err(|_| MALFORMED)?
                .ok_or(INVALID_TREE)?;
            if node.id <= 0 || node.parent != 0 && node.parent != parent {
                return Err(INVALID_TREE);
            }
            Ok(ArticleComment {
                id: node.id,
                author: node.author,
                text_html: node.text,
                discussion_url: discussion_url(node.id),
                replies: read_comments(&node.children, node.id, depth + 1, remaining)?,
            })
        })
        .collect()
}

fn discussion_url(id: i64) -> String {
    format!("https://news.ycombinator.com/item?id={id}")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bounds_depth_and_total_comments_across_the_tree() {
        for (depth, width, expected) in [
            (63, 1, "found"),
            (64, 1, "invalidResponse"),
            (2, 4999, "found"),
            (2, 5000, "invalidResponse"),
        ] {
            let mut children = (0..width)
                .map(|index| format!(r#"{{"id":{},"children":[]}}"#, index + 2))
                .collect::<Vec<_>>()
                .join(",");
            for level in 1..depth {
                children = format!(r#"{{"id":{},"children":[{children}]}}"#, 10000 + level);
            }
            let body = format!(r#"{{"id":1,"title":"Article","children":[{children}]}}"#);
            let QueryAttempt::Branch { branch, .. } = decode_article(body.as_bytes(), 1) else {
                panic!("unexpected retry");
            };
            assert_eq!(branch, expected, "depth {depth}, width {width}");
        }
    }
}
