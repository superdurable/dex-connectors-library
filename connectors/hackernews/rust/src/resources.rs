// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use super::*;
use serde_json::Value;

/// Identify a Hacker News item by its public ID.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct GetItemInput {
    /// Positive item ID.
    pub id: i64,
}

/// Public Hacker News item returned by the query.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Item {
    /// Public item ID.
    pub id: i64,
    /// Story, comment, job, poll, or pollopt.
    #[serde(rename = "type")]
    pub item_type: String,
    /// Public author username.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub by: String,
    /// Creation time in Unix seconds.
    #[serde(skip_serializing_if = "is_zero")]
    pub time: i64,
    /// Untrusted provider title.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub title: String,
    /// Untrusted submitted HTML.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub text: String,
    /// External URL submitted with the item.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub url: String,
    /// Canonical Hacker News discussion permalink.
    pub discussion_url: String,
    /// Public story score.
    #[serde(skip_serializing_if = "is_zero")]
    pub score: i64,
    /// Comment count.
    #[serde(skip_serializing_if = "is_zero")]
    pub descendants: i64,
    /// Parent item ID.
    #[serde(skip_serializing_if = "is_zero")]
    pub parent: i64,
    /// Child IDs in provider order.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub kids: Vec<i64>,
    /// Whether the item is deleted.
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub deleted: bool,
    /// Whether the item is moderated.
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub dead: bool,
}

/// Read one public item per Execute attempt.
pub struct GetItemOperation(FirebaseClient);

impl GetItemOperation {
    /// Use the public Firebase endpoint and the manifest timeout and size limit.
    pub fn new() -> Result<Self, ValidationError> {
        Ok(Self(FirebaseClient::new()?))
    }

    /// Use a validated base URL, bounded transport and clock for Retry-After.
    pub fn with_client(
        base_url: &str,
        max_response_bytes: usize,
        http: ProviderHttpClient,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        Ok(Self(FirebaseClient::with_client(
            base_url,
            max_response_bytes,
            http,
            clock,
        )?))
    }
}

/// Build a Step that reads a public item with the application's logical connection.
pub fn new_get_item_step<I>(
    config: QueryStepConfig<I, GetItemOperation>,
) -> Result<QueryStep<I, GetItemOperation>, ValidationError> {
    if config.connection.provider != "hacker-news" {
        return Err(ValidationError::new("Hacker News provider is required"));
    }
    QueryStep::new(config)
}

impl Query for GetItemOperation {
    type Input = GetItemInput;
    type Output = Item;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetItem::definition()
    }

    fn invoke(&self, _: &Call<'_>, input: &GetItemInput) -> QueryAttempt<Item> {
        if input.id <= 0 {
            return failed(
                "getItem",
                "defect",
                FailureKind::Validation,
                "item ID must be positive",
            );
        }
        self.0
            .read_json("getItem", &format!("/item/{}.json", input.id), |body| {
                let mut item: Item = match parse_resource(body, ITEM_FIELDS) {
                    Ok(Some(item)) => item,
                    Ok(None) => {
                        return failed(
                            "getItem",
                            "notFound",
                            FailureKind::NotFound,
                            "Hacker News item is deleted or dead",
                        );
                    }
                    Err(_) => {
                        return failed(
                            "getItem",
                            "invalidResponse",
                            FailureKind::Protocol,
                            "Hacker News returned malformed item JSON",
                        );
                    }
                };
                if item.deleted || item.dead {
                    return failed(
                        "getItem",
                        "notFound",
                        FailureKind::NotFound,
                        "Hacker News item is deleted or dead",
                    );
                }
                if item.id != input.id {
                    return failed(
                        "getItem",
                        "invalidResponse",
                        FailureKind::Protocol,
                        "Hacker News returned a different item ID",
                    );
                }
                if !matches!(
                    item.item_type.as_str(),
                    "story" | "job" | "comment" | "poll" | "pollopt"
                ) {
                    return failed(
                        "getItem",
                        "invalidResponse",
                        FailureKind::Protocol,
                        "Hacker News returned an unknown item type",
                    );
                }
                if item.kids.iter().any(|id| *id <= 0) {
                    return failed(
                        "getItem",
                        "invalidResponse",
                        FailureKind::Protocol,
                        "Hacker News returned an invalid child ID",
                    );
                }
                item.discussion_url = format!("https://news.ycombinator.com/item?id={}", item.id);
                QueryAttempt::branch("found", item, Receipt::default(), None)
            })
    }
}

/// Identify a Hacker News user by its public ID.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct GetUserInput {
    /// Case-sensitive username.
    pub id: String,
}

/// Public Hacker News user returned by the query.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct User {
    /// Case-sensitive username.
    pub id: String,
    /// Account creation in Unix seconds.
    pub created: i64,
    /// Public karma.
    pub karma: i64,
    /// Untrusted profile HTML.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub about: String,
    /// Public submitted item IDs.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub submitted: Vec<i64>,
    /// Canonical Hacker News profile permalink.
    pub profile_url: String,
}

/// Read one public user per Execute attempt.
pub struct GetUserOperation(FirebaseClient);

impl GetUserOperation {
    /// Use the public Firebase endpoint and the manifest timeout and size limit.
    pub fn new() -> Result<Self, ValidationError> {
        Ok(Self(FirebaseClient::new()?))
    }

    /// Use a validated base URL, bounded transport and clock for Retry-After.
    pub fn with_client(
        base_url: &str,
        max_response_bytes: usize,
        http: ProviderHttpClient,
        clock: Arc<dyn Fn() -> SystemTime + Send + Sync>,
    ) -> Result<Self, ValidationError> {
        Ok(Self(FirebaseClient::with_client(
            base_url,
            max_response_bytes,
            http,
            clock,
        )?))
    }
}

/// Build a Step that reads a public user with the application's logical connection.
pub fn new_get_user_step<I>(
    config: QueryStepConfig<I, GetUserOperation>,
) -> Result<QueryStep<I, GetUserOperation>, ValidationError> {
    if config.connection.provider != "hacker-news" {
        return Err(ValidationError::new("Hacker News provider is required"));
    }
    QueryStep::new(config)
}

impl Query for GetUserOperation {
    type Input = GetUserInput;
    type Output = User;

    fn definition(&self) -> QueryDefinition {
        generated_definitions::GetUser::definition()
    }

    fn invoke(&self, _: &Call<'_>, input: &GetUserInput) -> QueryAttempt<User> {
        if input.id.is_empty() || input.id.len() > 100 || input.id.contains(['/', '?', '#']) {
            return failed(
                "getUser",
                "defect",
                FailureKind::Validation,
                "username is invalid",
            );
        }
        self.0.read_json(
            "getUser",
            &format!("/user/{}.json", escape_username(&input.id, false)),
            |body| {
                let mut user: User = match parse_resource(body, USER_FIELDS) {
                    Ok(Some(user)) => user,
                    Ok(None) => {
                        return failed(
                            "getUser",
                            "notFound",
                            FailureKind::NotFound,
                            "Hacker News user was not found",
                        );
                    }
                    Err(_) => {
                        return failed(
                            "getUser",
                            "invalidResponse",
                            FailureKind::Protocol,
                            "Hacker News returned malformed user JSON",
                        );
                    }
                };
                if user.id != input.id || user.created < 0 || user.karma < 0 {
                    return failed(
                        "getUser",
                        "invalidResponse",
                        FailureKind::Protocol,
                        "Hacker News returned invalid user data",
                    );
                }
                if user.submitted.iter().any(|id| *id <= 0) {
                    return failed(
                        "getUser",
                        "invalidResponse",
                        FailureKind::Protocol,
                        "Hacker News returned an invalid submitted item ID",
                    );
                }
                user.profile_url = format!(
                    "https://news.ycombinator.com/user?id={}",
                    escape_username(&user.id, true)
                );
                QueryAttempt::branch("found", user, Receipt::default(), None)
            },
        )
    }
}

const ITEM_FIELDS: &[&str] = &[
    "id",
    "type",
    "by",
    "time",
    "title",
    "text",
    "url",
    "discussionUrl",
    "score",
    "descendants",
    "parent",
    "kids",
    "deleted",
    "dead",
];
const USER_FIELDS: &[&str] = &["id", "created", "karma", "about", "submitted", "profileUrl"];

// Go struct decoding accepts case-insensitive keys and treats null scalar fields as their zero value.
fn parse_resource<T: serde::de::DeserializeOwned>(
    body: &[u8],
    fields: &[&str],
) -> Result<Option<T>, serde_json::Error> {
    let document: Option<serde_json::Map<String, Value>> = serde_json::from_slice(body)?;
    let Some(document) = document else {
        return Ok(None);
    };
    let mut resource = serde_json::Map::new();
    for (key, mut value) in document {
        if let Some(&name) = fields.iter().find(|name| key.eq_ignore_ascii_case(name)) {
            if matches!(name, "kids" | "submitted")
                && let Some(ids) = value.as_array_mut()
            {
                for id in ids.iter_mut().filter(|id| id.is_null()) {
                    *id = Value::from(0);
                }
            }
            if !value.is_null() {
                resource.insert(name.into(), value);
            }
        }
    }
    serde_json::from_value(Value::Object(resource)).map(Some)
}

fn escape_username(username: &str, query: bool) -> String {
    const HEX: &[u8; 16] = b"0123456789ABCDEF";
    let mut escaped = String::new();
    for byte in username.bytes() {
        if byte.is_ascii_alphanumeric()
            || b"-._~".contains(&byte)
            || (!query && b"$&+:=@".contains(&byte))
        {
            escaped.push(char::from(byte));
        } else if query && byte == b' ' {
            escaped.push('+');
        } else {
            escaped.push('%');
            escaped.push(char::from(HEX[usize::from(byte >> 4)]));
            escaped.push(char::from(HEX[usize::from(byte & 15)]));
        }
    }
    escaped
}
