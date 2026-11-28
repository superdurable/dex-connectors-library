// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;
use dex_connector_hackernews::{
    GetItemInput, GetItemOperation, GetUserInput, GetUserOperation, Item, User, new_get_item_step,
    new_get_user_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::{collections::BTreeMap, error::Error};

const CONNECTION_NAME: &str = "hacker-news-public";

// dex:group group-id:item-author group-label:"Item author"
// dex:explanation text:"Return the public author profile."
struct ReturnProfile;

impl Step for ReturnProfile {
    type Input = QueryResult<Option<User>>;
    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ItemAuthorFlow {
    item: QueryStep<GetItemInput, GetItemOperation>,
    author: QueryStep<QueryResult<Option<Item>>, GetUserOperation>,
    complete: ReturnProfile,
}

impl ItemAuthorFlow {
    fn new(item: GetItemOperation, user: GetUserOperation) -> Result<Self, ValidationError> {
        let complete = ReturnProfile;
        let connection = ConnectionRef::new("hacker-news", CONNECTION_NAME);
        let author = new_get_user_step(QueryStepConfig::new(
            "ReadAuthor",
            StepAnnotations::new(
                "item-author",
                "Item author",
                "Read the public profile of the selected item's author.",
            ),
            user,
            connection.clone(),
            |result: &QueryResult<Option<Item>>| GetUserInput {
                id: result
                    .value
                    .as_ref()
                    .map(|item| item.by.clone())
                    .unwrap_or_default(),
            },
            BTreeMap::from([("found".into(), BranchTarget::to(&complete))]),
        ))?;
        let item = new_get_item_step(QueryStepConfig::new(
            "ReadItem",
            StepAnnotations::new(
                "item-author",
                "Item author",
                "Read the selected public item.",
            ),
            item,
            connection,
            |input: &GetItemInput| input.clone(),
            BTreeMap::from([("found".into(), BranchTarget::to(&author))]),
        ))?;
        Ok(Self {
            item,
            author,
            complete,
        })
    }
}

impl Flow for ItemAuthorFlow {
    type StartInput = GetItemInput;
    fn flow_type(&self) -> &'static str {
        "RustHackerNewsItemAuthor"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.item)
            .and(&self.author)
            .and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let new_flow = || ItemAuthorFlow::new(GetItemOperation::new()?, GetUserOperation::new()?);
    let args: Vec<_> = std::env::args().skip(1).collect();
    match args.as_slice() {
        [] => support::serve(new_flow()?),
        [read, id] if read == "--read" => {
            let user: User = support::read_once(new_flow, GetItemInput { id: id.parse()? })?;
            println!("{}", user.profile_url);
            Ok(())
        }
        _ => Err("expected --read ITEM_ID or no arguments".into()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use dex_connectors_sdk::providerhttp::*;
    use std::{
        sync::{
            Arc,
            atomic::{AtomicUsize, Ordering},
        },
        time::{Duration, SystemTime},
    };

    #[derive(Default)]
    struct AuthorResponses {
        items: AtomicUsize,
        users: AtomicUsize,
    }

    impl ProviderHttpTransport for AuthorResponses {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let (status, body) = match request.url.as_str() {
                "https://hacker-news.firebaseio.com/v0/item/42.json" => {
                    self.items.fetch_add(1, Ordering::SeqCst);
                    (200, r#"{"id":42,"type":"story","by":"author"}"#)
                }
                "https://hacker-news.firebaseio.com/v0/user/author.json" => {
                    let status = if self.users.fetch_add(1, Ordering::SeqCst) == 0 {
                        503
                    } else {
                        200
                    };
                    (status, r#"{"id":"author","karma":12}"#)
                }
                _ => panic!("unexpected Hacker News request"),
            };
            Ok(ProviderHttpResponse::new(
                status,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body.as_bytes())),
            ))
        }
    }

    #[test]
    fn retries_only_the_author_query() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(AuthorResponses::default());
        let transport =
            || ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15));
        let origin = "https://hacker-news.firebaseio.com/v0";
        let user: User = support::read_once(
            || {
                ItemAuthorFlow::new(
                    GetItemOperation::with_client(
                        origin,
                        1024,
                        transport()?,
                        Arc::new(SystemTime::now),
                    )?,
                    GetUserOperation::with_client(
                        origin,
                        1024,
                        transport()?,
                        Arc::new(SystemTime::now),
                    )?,
                )
            },
            GetItemInput { id: 42 },
        )?;
        assert_eq!(
            user.profile_url,
            "https://news.ycombinator.com/user?id=author"
        );
        assert_eq!(user.karma, 12);
        assert_eq!(provider.items.load(Ordering::SeqCst), 1);
        assert_eq!(provider.users.load(Ordering::SeqCst), 2);
        Ok(())
    }
}
