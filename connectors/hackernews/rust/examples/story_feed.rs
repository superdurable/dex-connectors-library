// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::BTreeMap;
use std::error::Error;
mod support;

use dex_connector_hackernews::{
    ListStoryIDsInput, ListStoryIDsOperation, StoryIDs, new_list_story_ids_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};

const CONNECTION_NAME: &str = "hacker-news-public";

// dex:group group-id:story-feed group-label:"Story feed"
// dex:explanation text:"Complete with the provider's visible story IDs."
struct ReturnStoryIDs;

impl Step for ReturnStoryIDs {
    type Input = QueryResult<Option<StoryIDs>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct StoryFeedFlow {
    query: QueryStep<ListStoryIDsInput, ListStoryIDsOperation>,
    complete: ReturnStoryIDs,
}

impl StoryFeedFlow {
    fn new(operation: ListStoryIDsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnStoryIDs;
        let config = QueryStepConfig::new(
            "ListStoryIDs",
            StepAnnotations::new(
                "story-feed",
                "Story feed",
                "Read public Hacker News story IDs.",
            ),
            operation,
            ConnectionRef::new("hacker-news", CONNECTION_NAME),
            |input: &ListStoryIDsInput| input.clone(),
            BTreeMap::from([("listed".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_story_ids_step(config)?,
            complete,
        })
    }
}

impl Flow for StoryFeedFlow {
    type StartInput = ListStoryIDsInput;

    fn flow_type(&self) -> &'static str {
        "RustHackerNewsStoryFeed"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    if std::env::args().nth(1).as_deref() == Some("--read") {
        let result = read_once(|| StoryFeedFlow::new(ListStoryIDsOperation::new()?))?;
        println!("{}", serde_json::to_string(&result)?);
        return Ok(());
    }
    let flow = StoryFeedFlow::new(ListStoryIDsOperation::new()?)?;
    support::serve(flow)
}

fn read_once(
    new_flow: impl Fn() -> Result<StoryFeedFlow, ValidationError>,
) -> Result<StoryIDs, Box<dyn Error>> {
    support::read_once(
        new_flow,
        ListStoryIDsInput {
            feed: "top".into(),
            limit: 2,
        },
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        sync::Arc,
        time::{Duration, SystemTime},
    };

    use dex_connectors_sdk::providerhttp::{
        ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, ProviderHttpResponse,
        ProviderHttpTransport, ResponseHeaders,
    };
    use std::sync::atomic::{AtomicUsize, Ordering};

    #[derive(Default)]
    struct FeedResponse(AtomicUsize);

    impl ProviderHttpTransport for FeedResponse {
        fn send(
            &self,
            _: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let status = if self.0.fetch_add(1, Ordering::SeqCst) == 0 {
                503
            } else {
                200
            };
            Ok(ProviderHttpResponse::new(
                status,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(b"[101,102,103]")),
            ))
        }
    }

    #[test]
    fn story_feed_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(FeedResponse::default());
        let result = read_once(|| {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            StoryFeedFlow::new(ListStoryIDsOperation::with_client(
                "https://hacker-news.firebaseio.com/v0",
                1 << 20,
                http,
                Arc::new(SystemTime::now),
            )?)
        })?;
        assert_eq!(result.ids, [101, 102]);
        assert!(result.has_more);
        assert_eq!(provider.0.load(Ordering::SeqCst), 2);
        Ok(())
    }
}
