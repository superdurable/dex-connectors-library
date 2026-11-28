// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_ipip::{
    Config, Credentials, LookupIPLocationInput, LookupIPLocationOperation, LookupIPLocationOutput,
    new_lookup_ip_location_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "ipip";

// dex:group group-id:ip-location group-label:"IP location"
// dex:explanation text:"Complete with the location fields returned by IPIP."
struct ReturnLocation;

impl Step for ReturnLocation {
    type Input = QueryResult<Option<LookupIPLocationOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct IPLocationFlow {
    query: QueryStep<LookupIPLocationInput, LookupIPLocationOperation>,
    complete: ReturnLocation,
}

impl IPLocationFlow {
    fn new(operation: LookupIPLocationOperation) -> Result<Self, ValidationError> {
        let complete = ReturnLocation;
        let config = QueryStepConfig::new(
            "LookupIPLocation",
            StepAnnotations::new(
                "ip-location",
                "IP location",
                "Look up the location of the supplied IP address.",
            ),
            operation,
            ConnectionRef::new("ipip", CONNECTION_NAME),
            |input: &LookupIPLocationInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_lookup_ip_location_step(config)?,
            complete,
        })
    }
}

impl Flow for IPLocationFlow {
    type StartInput = LookupIPLocationInput;

    fn flow_type(&self) -> &'static str {
        "RustIPIPIPLocation"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        token: SecretString::new(std::env::var("IPIP_TOKEN")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("ipip", CONNECTION_NAME),
        credentials,
    )]));
    let flow = IPLocationFlow::new(LookupIPLocationOperation::new(
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
    struct LocationResponse(AtomicUsize);

    impl ProviderHttpTransport for LocationResponse {
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
                    br#"{"ret":"ok","data":{"info":{"country_name":"US","region_name":"California","city_name":"Mountain View","country_code":"US"}}}"#,
                )),
            ))
        }
    }

    #[test]
    fn ip_location_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(LocationResponse::default());
        let connection = ConnectionRef::new("ipip", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                token: SecretString::new("test-key"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            IPLocationFlow::new(LookupIPLocationOperation::with_http_client(
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
                "rust-ip-location-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(
                &new_flow()?,
                &flow_id,
                LookupIPLocationInput {
                    ip_address: "8.8.8.8".into(),
                },
            )?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: LookupIPLocationOutput = result.single_output()?;
            assert_eq!(output.city_name, "Mountain View");
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
