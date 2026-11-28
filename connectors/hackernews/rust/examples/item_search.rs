// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;
use dex_connector_hackernews::{
    ItemSearchPage, SearchItemsInput, SearchItemsOperation, new_search_items_step,
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
    type Input = QueryResult<Option<ItemSearchPage>>;
    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ItemSearchFlow {
    search: QueryStep<SearchItemsInput, SearchItemsOperation>,
    complete: ReturnPage,
}

impl ItemSearchFlow {
    fn new(operation: SearchItemsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnPage;
        let search = new_search_items_step(QueryStepConfig::new(
            "SearchPage",
            StepAnnotations::new(
                "search",
                "Keyword search",
                "Search public indexed items by keyword.",
            ),
            operation,
            ConnectionRef::new("hacker-news", CONNECTION_NAME),
            |input: &SearchItemsInput| input.clone(),
            BTreeMap::from([("searched".into(), BranchTarget::to(&complete))]),
        ))?;
        Ok(Self { search, complete })
    }
}

impl Flow for ItemSearchFlow {
    type StartInput = SearchItemsInput;
    fn flow_type(&self) -> &'static str {
        "RustHackerNewsItemSearch"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.search).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let new_flow = || ItemSearchFlow::new(SearchItemsOperation::new()?);
    let args: Vec<_> = std::env::args().skip(1).collect();
    match args.as_slice() {
        [] => support::serve(new_flow()?),
        [read, query] if read == "--read" => {
            let page: ItemSearchPage = support::read_once(
                new_flow,
                SearchItemsInput {
                    query: query.clone(),
                    hits_per_page: 2,
                    ..Default::default()
                },
            )?;
            println!("{} hits; next page {}", page.items.len(), page.next_page);
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
    let transport = Arc::new(search_fixture::Responses::new(""));
    let page: ItemSearchPage = support::read_once(
        || {
            ItemSearchFlow::new(SearchItemsOperation::with_client(
                "https://hn.algolia.com/api/v1",
                4096,
                ProviderHttpClient::with_transport(transport.clone(), Duration::from_secs(15))?,
                Arc::new(SystemTime::now),
            )?)
        },
        SearchItemsInput {
            query: "compiler".into(),
            hits_per_page: 2,
            ..Default::default()
        },
    )?;
    assert_eq!(page.items[0].id, 42);
    assert_eq!(page.next_page, 1);
    assert_eq!(transport.calls.load(Ordering::SeqCst), 2);
    Ok(())
}
