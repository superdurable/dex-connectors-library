// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

mod support;
use dex_connector_hackernews::{
    ArticleThread, GetArticleThreadInput, GetArticleThreadOperation, new_get_article_thread_step,
};
use dex_connectors_sdk::dex_sdk::{Context, Flow, HandlerResult, Step, StepDecision, StepList};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};
use std::{collections::BTreeMap, error::Error};

const CONNECTION_NAME: &str = "hacker-news-public";

// dex:group group-id:article group-label:"Article thread"
// dex:explanation text:"Return the article and its nested comments."
struct ReturnArticle;

impl Step for ReturnArticle {
    type Input = QueryResult<Option<ArticleThread>>;
    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ArticleThreadFlow {
    article: QueryStep<GetArticleThreadInput, GetArticleThreadOperation>,
    complete: ReturnArticle,
}

impl ArticleThreadFlow {
    fn new(operation: GetArticleThreadOperation) -> Result<Self, ValidationError> {
        let complete = ReturnArticle;
        let article = new_get_article_thread_step(QueryStepConfig::new(
            "ReadArticle",
            StepAnnotations::new(
                "article",
                "Article thread",
                "Read the selected article and its indexed comments.",
            ),
            operation,
            ConnectionRef::new("hacker-news", CONNECTION_NAME),
            |input: &GetArticleThreadInput| input.clone(),
            BTreeMap::from([("found".into(), BranchTarget::to(&complete))]),
        ))?;
        Ok(Self { article, complete })
    }
}

impl Flow for ArticleThreadFlow {
    type StartInput = GetArticleThreadInput;
    fn flow_type(&self) -> &'static str {
        "RustHackerNewsArticleThread"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.article).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let new_flow = || ArticleThreadFlow::new(GetArticleThreadOperation::new()?);
    let args: Vec<_> = std::env::args().skip(1).collect();
    match args.as_slice() {
        [] => support::serve(new_flow()?),
        [read, id] if read == "--read" => {
            let article: ArticleThread =
                support::read_once(new_flow, GetArticleThreadInput { id: id.parse()? })?;
            println!("{} {}", article.discussion_url, article.comments.len());
            Ok(())
        }
        _ => Err("expected --read ARTICLE_ID or no arguments".into()),
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
    struct ArticleResponses(AtomicUsize);

    impl ProviderHttpTransport for ArticleResponses {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            assert_eq!(request.url, "https://hn.algolia.com/api/v1/items/42");
            let status = if self.0.fetch_add(1, Ordering::SeqCst) == 0 {
                503
            } else {
                200
            };
            let body =
                br#"{"id":42,"title":"Article","children":[{"id":7,"children":[{"id":9}]}]}"#;
            Ok(ProviderHttpResponse::new(
                status,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body)),
            ))
        }
    }

    #[test]
    fn retry_reads_nested_article() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(ArticleResponses::default());
        let article: ArticleThread = support::read_once(
            || {
                ArticleThreadFlow::new(GetArticleThreadOperation::with_client(
                    "https://hn.algolia.com/api/v1",
                    1024,
                    ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?,
                    Arc::new(SystemTime::now),
                )?)
            },
            GetArticleThreadInput { id: 42 },
        )?;
        assert_eq!(
            article.comments[0].replies[0].discussion_url,
            "https://news.ycombinator.com/item?id=9"
        );
        assert_eq!(provider.0.load(Ordering::SeqCst), 2);
        Ok(())
    }
}
