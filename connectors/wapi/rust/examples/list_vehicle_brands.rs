// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

use dex_connector_wapi::{
    ListVehicleBrandsInput, ListVehicleBrandsOperation, ListVehicleBrandsOutput,
    new_list_vehicle_brands_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};

const CONNECTION_NAME: &str = "wapi";

// dex:group group-id:vehicle-brand-directory group-label:"VehicleBrand catalog"
// dex:explanation text:"Complete with the provider's visible vehicle brand catalog."
struct ReturnBrands;

impl Step for ReturnBrands {
    type Input = QueryResult<Option<ListVehicleBrandsOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct VehicleBrandDirectoryFlow {
    query: QueryStep<ListVehicleBrandsInput, ListVehicleBrandsOperation>,
    complete: ReturnBrands,
}

impl VehicleBrandDirectoryFlow {
    fn new(operation: ListVehicleBrandsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnBrands;
        let config = QueryStepConfig::new(
            "ListVehicleBrands",
            StepAnnotations::new(
                "vehicle-brand-directory",
                "VehicleBrand catalog",
                "Read the available vehicle brand catalog.",
            ),
            operation,
            ConnectionRef::new("wapi", CONNECTION_NAME),
            |input: &ListVehicleBrandsInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_vehicle_brands_step(config)?,
            complete,
        })
    }
}

impl Flow for VehicleBrandDirectoryFlow {
    type StartInput = ListVehicleBrandsInput;

    fn flow_type(&self) -> &'static str {
        "RustWapiVehicleBrandDirectory"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let flow = VehicleBrandDirectoryFlow::new(ListVehicleBrandsOperation::from_project(
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
    struct VehicleBrandResponse(AtomicUsize);

    impl ProviderHttpTransport for VehicleBrandResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let index = self.0.fetch_add(1, Ordering::SeqCst);
            assert_eq!(
                request.url,
                "https://uaqy.api.storeapi.net/pyi/170/324?appid=123&format=json&sign=cdf4f0a9fb8d9e9000b8f4e014b9e41b&time=1795089600"
            );
            let brand = serde_json::json!({"ppid":"15","pp_name":"宝马","pp_price":3400});
            let body = serde_json::json!({"codeid":"10000","retdata":vec![brand;201]}).to_string();
            Ok(ProviderHttpResponse::new(
                if index == 0 { 503 } else { 200 },
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body.into_bytes())),
            ))
        }
    }

    #[test]
    fn vehicle_brand_directory_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(VehicleBrandResponse::default());
        let connection = ConnectionRef::new("wapi", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            dex_connector_wapi::Credentials {
                app_id: "123".into(),
                app_secret: SecretString::new("REDACTEDAPPSECRET000000000000000"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            VehicleBrandDirectoryFlow::new(ListVehicleBrandsOperation::with_http_client(
                credentials.clone(),
                http,
                1 << 20,
                Arc::new(|| UNIX_EPOCH + Duration::from_secs(1795089600)),
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
                "rust-vehicle-brand-directory-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListVehicleBrandsInput::default())?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: ListVehicleBrandsOutput = result.single_output()?;

            assert_eq!(output.brands[0].id, "15");
            assert_eq!(output.brands[0].name, "宝马");
            assert_eq!(output.brands[0].price_fen, 3400);
            assert_eq!(output.total, 201);
            assert_eq!(output.brands.len(), 200);
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
