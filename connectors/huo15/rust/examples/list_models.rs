// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_huo15::{
    Config, Credentials, ListModelsInput, ListModelsOperation, ListModelsOutput,
    new_list_models_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "huo15";

// dex:group group-id:model-catalog group-label:"Model catalog"
// dex:explanation text:"Complete with the provider's visible model IDs."
struct ReturnModels;

impl Step for ReturnModels {
    type Input = QueryResult<Option<ListModelsOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct ModelCatalogFlow {
    query: QueryStep<ListModelsInput, ListModelsOperation>,
    complete: ReturnModels,
}

impl ModelCatalogFlow {
    fn new(operation: ListModelsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnModels;
        let config = QueryStepConfig::new(
            "ListModels",
            StepAnnotations::new(
                "model-catalog",
                "Model catalog",
                "Read the available model IDs.",
            ),
            operation,
            ConnectionRef::new("huo15", CONNECTION_NAME),
            |input: &ListModelsInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_models_step(config)?,
            complete,
        })
    }
}

impl Flow for ModelCatalogFlow {
    type StartInput = ListModelsInput;

    fn flow_type(&self) -> &'static str {
        "RustHuo15ModelCatalog"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        api_key: SecretString::new(std::env::var("HUO15_API_KEY")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("huo15", CONNECTION_NAME),
        credentials,
    )]));
    let flow = ModelCatalogFlow::new(ListModelsOperation::new(
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
    struct CatalogResponse(AtomicUsize);

    impl ProviderHttpTransport for CatalogResponse {
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
                Box::new(std::io::Cursor::new(
                    br#"{"object":"list","data":[{"id":"example","object":"model"}]}"#,
                )),
            ))
        }
    }

    #[test]
    fn model_catalog_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(CatalogResponse::default());
        let connection = ConnectionRef::new("huo15", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                api_key: SecretString::new("fsk-test-secret"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            ModelCatalogFlow::new(ListModelsOperation::with_http_client(
                credentials.clone(),
                http,
                1 << 20,
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
                "rust-model-catalog-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListModelsInput {})?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: ListModelsOutput = result.single_output()?;
            assert_eq!(output.model_ids, ["example"]);
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
