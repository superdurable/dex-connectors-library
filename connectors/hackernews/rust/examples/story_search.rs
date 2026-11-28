// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;
use dex_connector_hackernews::{
    SearchStoriesInput, SearchStoriesOperation, StorySearchPage, new_search_stories_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::{collections::BTreeMap, error::Error};

const CONNECTION_NAME: &str = "hacker-news-public";

// dex:group group-id:search group-label:"Keyword search"
// dex:explanation text:"Return the indexed hits and continuation page."
struct ReturnPage;

impl Step for ReturnPage {
    type Input = QueryResult<Option<StorySearchPage>>;
    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct StorySearchFlow {
    search: QueryStep<SearchStoriesInput, SearchStoriesOperation>,
    complete: ReturnPage,
}

impl StorySearchFlow {
    fn new(operation: SearchStoriesOperation) -> Result<Self, ValidationError> {
        let complete = ReturnPage;
        let search = new_search_stories_step(QueryStepConfig::new(
            "SearchPage",
            StepAnnotations::new(
                "search",
                "Keyword search",
                "Search public stories by keyword.",
            ),
            operation,
            ConnectionRef::new("hacker-news", CONNECTION_NAME),
            |input: &SearchStoriesInput| input.clone(),
            BTreeMap::from([("searched".into(), BranchTarget::to(&complete))]),
        ))?;
        Ok(Self { search, complete })
    }
}

impl Flow for StorySearchFlow {
    type StartInput = SearchStoriesInput;
    fn flow_type(&self) -> &'static str {
        "RustHackerNewsStorySearch"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.search).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let new_flow = || StorySearchFlow::new(SearchStoriesOperation::new()?);
    let args: Vec<_> = std::env::args().skip(1).collect();
    match args.as_slice() {
        [] => support::serve(new_flow()?),
        [read, query] if read == "--read" => {
            let page: StorySearchPage = support::read_once(
                new_flow,
                SearchStoriesInput {
                    query: query.clone(),
                    hits_per_page: 2,
                    ..Default::default()
                },
            )?;
            println!("{} hits; next page {}", page.stories.len(), page.next_page);
            Ok(())
        }
        _ => Err("expected --read KEYWORDS or no arguments".into()),
    }
}

#[cfg(test)]
#[path = "support/search_fixture.rs"]
mod search_fixture;

#[test]
fn retries_the_search_and_returns_its_page() -> Result<(), Box<dyn Error>> {
    use dex_connectors_sdk::providerhttp::ProviderHttpClient;
    use std::sync::{Arc, atomic::Ordering};
    use std::time::{Duration, SystemTime};
    let transport = Arc::new(search_fixture::Responses::new("&tags=story"));
    let page: StorySearchPage = support::read_once(
        || {
            StorySearchFlow::new(SearchStoriesOperation::with_client(
                "https://hn.algolia.com/api/v1",
                4096,
                ProviderHttpClient::with_transport(transport.clone(), Duration::from_secs(15))?,
                Arc::new(SystemTime::now),
            )?)
        },
        SearchStoriesInput {
            query: "compiler".into(),
            hits_per_page: 2,
            ..Default::default()
        },
    )?;
    assert_eq!(page.stories[0].id, 42);
    assert_eq!(page.next_page, 1);
    assert_eq!(transport.calls.load(Ordering::SeqCst), 2);
    Ok(())
}
