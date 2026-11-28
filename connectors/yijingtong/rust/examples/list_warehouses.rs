// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_yijingtong::{
    Config, Credentials, ListWarehousesInput, ListWarehousesOperation, WarehousePage,
    new_list_warehouses_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "yijingtong";

// dex:group group-id:warehouse-catalog group-label:"Warehouse catalog"
// dex:explanation text:"Complete with the provider's visible warehouse catalog."
struct ReturnWarehouses;

impl Step for ReturnWarehouses {
    type Input = QueryResult<Option<WarehousePage>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct WarehouseCatalogFlow {
    query: QueryStep<ListWarehousesInput, ListWarehousesOperation>,
    complete: ReturnWarehouses,
}

impl WarehouseCatalogFlow {
    fn new(operation: ListWarehousesOperation) -> Result<Self, ValidationError> {
        let complete = ReturnWarehouses;
        let config = QueryStepConfig::new(
            "ListWarehouses",
            StepAnnotations::new(
                "warehouse-catalog",
                "Warehouse catalog",
                "Read the available warehouse catalog.",
            ),
            operation,
            ConnectionRef::new("yijingtong", CONNECTION_NAME),
            |input: &ListWarehousesInput| input.clone(),
            BTreeMap::from([("listed".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_warehouses_step(config)?,
            complete,
        })
    }
}

impl Flow for WarehouseCatalogFlow {
    type StartInput = ListWarehousesInput;

    fn flow_type(&self) -> &'static str {
        "RustYijingtongWarehouseCatalog"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        token: SecretString::new(std::env::var("YIJINGTONG_TOKEN")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("yijingtong", CONNECTION_NAME),
        credentials,
    )]));
    let flow = WarehouseCatalogFlow::new(ListWarehousesOperation::new(
        Config {
            base_url: std::env::var("YIJINGTONG_BASE_URL")?,
            ..Default::default()
        },
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
    struct WarehouseResponse(AtomicUsize);

    impl ProviderHttpTransport for WarehouseResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            assert_eq!(
                request.url,
                "http://127.0.0.1:12345/hwc_api/hwc_storage_list.php"
            );
            let status = if self.0.fetch_add(1, Ordering::SeqCst) == 0 {
                503
            } else {
                200
            };
            Ok(ProviderHttpResponse::new(
                status,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(
                    r#"{"error":"T","content":"ok","data":[{"nums":"US01","pname":"US Warehouse","country":"United States","is_tc":0}]}"#.as_bytes(),
                )),
            ))
        }
    }

    #[test]
    fn warehouse_catalog_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(WarehouseResponse::default());
        let connection = ConnectionRef::new("yijingtong", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                token: SecretString::new("sk-test-token"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            WarehouseCatalogFlow::new(ListWarehousesOperation::with_base_url(
                credentials.clone(),
                http,
                1 << 20,
                "http://127.0.0.1:12345/",
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
                "rust-warehouse-catalog-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListWarehousesInput {})?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: WarehousePage = result.single_output()?;

            assert_eq!(output.warehouses[0].id, "US01");
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
