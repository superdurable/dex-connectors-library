// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

use dex_connector_weiban::{ListTagsInput, ListTagsOperation, TagsResult, new_list_tags_step};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};

const CONNECTION_NAME: &str = "weiban";

// dex:group group-id:tag-directory group-label:"Tag catalog"
// dex:explanation text:"Complete with the provider's visible tag catalog."
struct ReturnTags;

impl Step for ReturnTags {
    type Input = QueryResult<Option<TagsResult>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct TagDirectoryFlow {
    query: QueryStep<ListTagsInput, ListTagsOperation>,
    complete: ReturnTags,
}

impl TagDirectoryFlow {
    fn new(operation: ListTagsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnTags;
        let config = QueryStepConfig::new(
            "ListTags",
            StepAnnotations::new(
                "tag-directory",
                "Tag catalog",
                "Read the available tag catalog.",
            ),
            operation,
            ConnectionRef::new("weiban", CONNECTION_NAME),
            |input: &ListTagsInput| input.clone(),
            BTreeMap::from([("listed".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_tags_step(config)?,
            complete,
        })
    }
}

impl Flow for TagDirectoryFlow {
    type StartInput = ListTagsInput;

    fn flow_type(&self) -> &'static str {
        "RustWeibanTagDirectory"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let flow = TagDirectoryFlow::new(ListTagsOperation::from_project(&project, CONNECTION_NAME)?)?;
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

#[cfg(test)]
mod tests {
    use super::*;
    use dex_connector_weiban::Credentials;
    use dex_connectors_sdk::dex_sdk::{Client, ClientOptions, FlowStatus};
    use dex_connectors_sdk::providerhttp::{
        ProviderHttpClient, ProviderHttpError, ProviderHttpRequest, ProviderHttpResponse,
        ProviderHttpTransport, ResponseHeaders,
    };
    use dex_connectors_sdk::{SecretString, StaticCredentialProvider};
    use std::collections::HashMap;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::time::{Duration, SystemTime, UNIX_EPOCH};

    #[derive(Default)]
    struct TagResponse(AtomicUsize);

    impl ProviderHttpTransport for TagResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let index = self.0.fetch_add(1, Ordering::SeqCst);
            let is_auth = index % 2 == 0;
            assert_eq!(
                request.url.split('?').next().unwrap(),
                if is_auth {
                    "https://open.weibanzhushou.com/open-api/access_token/get"
                } else {
                    "https://open.weibanzhushou.com/open-api/tag/list"
                }
            );
            let token = format!("fixture+token&{}", index / 2);
            if !is_auth {
                assert_eq!(
                    request.url.split_once('?').unwrap().1,
                    format!(
                        "access_token=fixture%2Btoken%26{}&limit=10&offset=0",
                        index / 2
                    )
                );
            }
            let body = if is_auth {
                r#"{"access_token":"FIXTURE_TOKEN","errcode":0,"errmsg":"ok","expires_in":7200}"#
                    .replace("FIXTURE_TOKEN", &token)
            } else {
                r#"{"errcode":0,"errmsg":"ok","total":1,"tag_list":[{"tag_id":"tag_123","tag_name":"Returning","tag_group":"Lifecycle"}]}"#.to_owned()
            };
            Ok(ProviderHttpResponse::new(
                if index == 1 { 503 } else { 200 },
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body.into_bytes())),
            ))
        }
    }

    #[test]
    fn tag_directory_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(TagResponse::default());
        let connection = ConnectionRef::new("weiban", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                corp_id: "corp_test".into(),
                secret: SecretString::new("secret"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(10))?;
            TagDirectoryFlow::new(ListTagsOperation::with_base_url(
                credentials.clone(),
                http,
                1 << 20,
                "https://open.weibanzhushou.com",
            )?)
        };
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
        let verification = (|| -> Result<(), Box<dyn Error>> {
            tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()?
                .block_on(async {
                    tokio::time::timeout(Duration::from_secs(30), worker.wait_pull_registered())
                        .await
                })??;
            let flow_id = format!(
                "rust-tag-directory-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListTagsInput {})?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: TagsResult = result.single_output()?;

            assert_eq!(output.tags[0].group, "Lifecycle");
            assert_eq!(output.tags[0].id, "tag_123");
            assert_eq!(output.tags[0].name, "Returning");
            assert_eq!(output.total, 1);
            assert_eq!(provider.0.load(Ordering::SeqCst), 4);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
