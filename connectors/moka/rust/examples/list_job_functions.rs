// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

use dex_connector_moka::{
    ListJobFunctionsInput, ListJobFunctionsOperation, ListJobFunctionsOutput,
    new_list_job_functions_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};

const CONNECTION_NAME: &str = "moka";

// dex:group group-id:job-function-directory group-label:"JobFunction catalog"
// dex:explanation text:"Complete with the provider's visible job function catalog."
struct ReturnFunctions;

impl Step for ReturnFunctions {
    type Input = QueryResult<Option<ListJobFunctionsOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct JobFunctionDirectoryFlow {
    query: QueryStep<ListJobFunctionsInput, ListJobFunctionsOperation>,
    complete: ReturnFunctions,
}

impl JobFunctionDirectoryFlow {
    fn new(operation: ListJobFunctionsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnFunctions;
        let config = QueryStepConfig::new(
            "ListJobFunctions",
            StepAnnotations::new(
                "job-function-directory",
                "JobFunction catalog",
                "Read the available job function catalog.",
            ),
            operation,
            ConnectionRef::new("moka", CONNECTION_NAME),
            |input: &ListJobFunctionsInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_job_functions_step(config)?,
            complete,
        })
    }
}

impl Flow for JobFunctionDirectoryFlow {
    type StartInput = ListJobFunctionsInput;

    fn flow_type(&self) -> &'static str {
        "RustMokaJobFunctionDirectory"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let flow = JobFunctionDirectoryFlow::new(ListJobFunctionsOperation::from_project(
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
    struct JobFunctionResponse(AtomicUsize);

    impl ProviderHttpTransport for JobFunctionResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let index = self.0.fetch_add(1, Ordering::SeqCst);
            assert_eq!(
                request.url,
                "https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/orgid-test?mode=campus"
            );
            let body = r#"{"jobs":[{"id":2873,"label":"Technology","jobCount":64,"email":"must not persist"}]}"#;
            Ok(ProviderHttpResponse::new(
                if index == 0 { 503 } else { 200 },
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body.as_bytes())),
            ))
        }
    }

    #[test]
    fn job_function_directory_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(JobFunctionResponse::default());
        let connection = ConnectionRef::new("moka", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            SecretString::new("key"),
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(10))?;
            JobFunctionDirectoryFlow::new(ListJobFunctionsOperation::with_http_client(
                dex_connector_moka::Config {
                    org_id: "orgid-test".into(),
                    mode: "campus".into(),
                    ..Default::default()
                },
                credentials.clone(),
                http,
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
                "rust-job-function-directory-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListJobFunctionsInput::default())?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: ListJobFunctionsOutput = result.single_output()?;

            assert_eq!(output.functions[0].id, 2873);
            assert_eq!(output.functions[0].job_count, 64);
            assert_eq!(output.functions[0].label, "Technology");
            assert_eq!(output.functions[0].parent_id, 0);
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
