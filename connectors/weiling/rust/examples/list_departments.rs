// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_weiling::{
    Config, Credentials, ListDepartmentsInput, ListDepartmentsOperation, ListDepartmentsOutput,
    new_list_departments_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "weiling";

// dex:group group-id:department-directory group-label:"Department catalog"
// dex:explanation text:"Complete with the provider's visible department catalog."
struct ReturnDepartments;

impl Step for ReturnDepartments {
    type Input = QueryResult<Option<ListDepartmentsOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct DepartmentDirectoryFlow {
    query: QueryStep<ListDepartmentsInput, ListDepartmentsOperation>,
    complete: ReturnDepartments,
}

impl DepartmentDirectoryFlow {
    fn new(operation: ListDepartmentsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnDepartments;
        let config = QueryStepConfig::new(
            "ListDepartments",
            StepAnnotations::new(
                "department-directory",
                "Department catalog",
                "Read the available department catalog.",
            ),
            operation,
            ConnectionRef::new("weiling", CONNECTION_NAME),
            |input: &ListDepartmentsInput| input.clone(),
            BTreeMap::from([("listed".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_departments_step(config)?,
            complete,
        })
    }
}

impl Flow for DepartmentDirectoryFlow {
    type StartInput = ListDepartmentsInput;

    fn flow_type(&self) -> &'static str {
        "RustWeilingDepartmentDirectory"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        app_id: SecretString::new(std::env::var("WEILING_APP_ID")?),
        app_secret: SecretString::new(std::env::var("WEILING_APP_SECRET")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("weiling", CONNECTION_NAME),
        credentials,
    )]));
    let flow = DepartmentDirectoryFlow::new(ListDepartmentsOperation::new(
        Config::default(),
        Arc::new(provider),
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
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::time::{Duration, SystemTime, UNIX_EPOCH};

    #[derive(Default)]
    struct DepartmentResponse(AtomicUsize);

    impl ProviderHttpTransport for DepartmentResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let index = self.0.fetch_add(1, Ordering::SeqCst);
            let is_auth = index == 0;
            assert_eq!(
                request.url.split('?').next().unwrap(),
                if is_auth {
                    "https://openapi.weiling.cn/openapi/auth/access_token/get"
                } else {
                    "https://openapi.weiling.cn/openapi/department/list"
                }
            );
            let body = if is_auth {
                r#"{"code":0,"data":{"access_token":"test-access","expires_in":180}}"#
            } else {
                r#"{"code":0,"data":{"data_list":[{"dept_id":"D1","name":"Domain","parent_id":"root"}]}}"#
            };
            Ok(ProviderHttpResponse::new(
                if index == 1 { 503 } else { 200 },
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body.as_bytes())),
            ))
        }
    }

    #[test]
    fn department_directory_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(DepartmentResponse::default());
        let connection = ConnectionRef::new("weiling", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                app_id: SecretString::new("key"),
                app_secret: SecretString::new("secret"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(10))?;
            DepartmentDirectoryFlow::new(ListDepartmentsOperation::with_base_url(
                credentials.clone(),
                http,
                1 << 20,
                "https://openapi.weiling.cn",
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
                "rust-department-directory-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListDepartmentsInput::default())?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: ListDepartmentsOutput = result.single_output()?;

            assert_eq!(output.departments[0].id, "D1");
            assert_eq!(output.departments[0].parent_id, "root");
            assert_eq!(provider.0.load(Ordering::SeqCst), 3);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
