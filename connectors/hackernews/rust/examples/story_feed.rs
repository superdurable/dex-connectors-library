// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

use dex_connector_hackernews::{
    ListStoryIDsInput, ListStoryIDsOperation, StoryIDs, new_list_story_ids_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
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
    let registry = Registry::new().register(flow)?;
    let directory = tempfile::tempdir()?;
    let cache = Arc::new(BlobCache::open(BlobCacheConfig::new(
        directory.path(),
        16 << 20,
        1000,
    )?)?);
    let address =
        std::env::var("DEX_FLOW_SERVICE_ADDRESS").unwrap_or_else(|_| "127.0.0.1:8801".into());
    let worker = Worker::try_new(
        registry,
        Arc::clone(&cache),
        WorkerOptions::new().server_address(address),
    )?;
    let result = worker.start();
    cache.close()?;
    result?;
    Ok(())
}

use dex_connectors_sdk::dex_sdk::{Client, ClientOptions, FlowStatus};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

fn read_once(
    new_flow: impl Fn() -> Result<StoryFeedFlow, ValidationError>,
) -> Result<StoryIDs, Box<dyn Error>> {
    let registry = Registry::new().register(new_flow()?)?;
    let directory = tempfile::tempdir()?;
    let cache = Arc::new(BlobCache::open(BlobCacheConfig::new(
        directory.path(),
        16 << 20,
        1000,
    )?)?);
    let address =
        std::env::var("DEX_FLOW_SERVICE_ADDRESS").unwrap_or_else(|_| "127.0.0.1:8801".into());
    let worker = Arc::new(Worker::try_new(
        registry.clone(),
        cache.clone(),
        WorkerOptions::new().server_address(&address),
    )?);
    let client = Client::try_new(
        registry,
        cache.clone(),
        ClientOptions::new().server_address(address),
    )?;
    let running_worker = worker.clone();
    let thread = std::thread::spawn(move || running_worker.start());
    let verification = (|| -> Result<StoryIDs, Box<dyn Error>> {
        tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()?
            .block_on(async {
                tokio::time::timeout(Duration::from_secs(30), worker.wait_pull_registered()).await
            })??;
        let flow_id = format!(
            "rust-story-feed-{}",
            SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
        );
        client.start_flow(
            &new_flow()?,
            &flow_id,
            ListStoryIDsInput {
                feed: "top".into(),
                limit: 2,
            },
        )?;
        let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
        if result.status() != FlowStatus::Completed {
            return Err("story feed Flow did not complete".into());
        }
        Ok(result.single_output::<StoryIDs>()?)
    })();
    worker.stop();
    thread.join().expect("Worker thread")?;
    cache.close()?;
    verification
}

#[cfg(test)]
mod tests {
    use super::*;

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
