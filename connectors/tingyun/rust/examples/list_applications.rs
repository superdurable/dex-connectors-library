// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

use dex_connector_tingyun::{
    ListApplicationsInput, ListApplicationsOperation, ListApplicationsOutput,
    new_list_applications_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};

const CONNECTION_NAME: &str = "tingyun";

// dex:group group-id:application-directory group-label:"Application catalog"
// dex:explanation text:"Complete with the provider's visible application catalog."
struct ReturnApplications;

impl Step for ReturnApplications {
    type Input = QueryResult<Option<ListApplicationsOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ApplicationDirectoryFlow {
    query: QueryStep<ListApplicationsInput, ListApplicationsOperation>,
    complete: ReturnApplications,
}

impl ApplicationDirectoryFlow {
    fn new(operation: ListApplicationsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnApplications;
        let config = QueryStepConfig::new(
            "ListApplications",
            StepAnnotations::new(
                "application-directory",
                "Application catalog",
                "Read the available application catalog.",
            ),
            operation,
            ConnectionRef::new("tingyun", CONNECTION_NAME),
            |input: &ListApplicationsInput| input.clone(),
            BTreeMap::from([("listed".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_applications_step(config)?,
            complete,
        })
    }
}

impl Flow for ApplicationDirectoryFlow {
    type StartInput = ListApplicationsInput;

    fn flow_type(&self) -> &'static str {
        "RustTingyunApplicationDirectory"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let flow = ApplicationDirectoryFlow::new(ListApplicationsOperation::from_project(
        &project,
        CONNECTION_NAME,
    )?)?;
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
    struct ApplicationResponse(AtomicUsize);

    impl ProviderHttpTransport for ApplicationResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let index = self.0.fetch_add(1, Ordering::SeqCst);
            assert_eq!(request.url, "https://wukong2.tingyun.com/app-api/app/list");
            let body = r#"[{"id":7461,"name":"app_test","accountId":186938}]"#;
            Ok(ProviderHttpResponse::new(
                if index == 0 { 503 } else { 200 },
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body.as_bytes())),
            ))
        }
    }

    #[test]
    fn application_directory_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(ApplicationResponse::default());
        let connection = ConnectionRef::new("tingyun", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            SecretString::new("key"),
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(10))?;
            ApplicationDirectoryFlow::new(ListApplicationsOperation::with_base_url(
                credentials.clone(),
                http,
                1 << 20,
                "https://wukong2.tingyun.com",
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
                "rust-application-directory-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListApplicationsInput::default())?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: ListApplicationsOutput = result.single_output()?;

            assert_eq!(output.applications[0].id, 7461);
            assert_eq!(output.applications[0].name, "app_test");
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
