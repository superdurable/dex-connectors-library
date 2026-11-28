// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_qweather::{
    Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOperation,
    GetCurrentWeatherOutput, new_get_current_weather_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "qweather";

// dex:group group-id:current-weather group-label:"Current weather"
// dex:explanation text:"Return the weather fields returned by QWeather."
struct ReturnWeather;

impl Step for ReturnWeather {
    type Input = QueryResult<Option<GetCurrentWeatherOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct CurrentWeatherFlow {
    query: QueryStep<GetCurrentWeatherInput, GetCurrentWeatherOperation>,
    complete: ReturnWeather,
}

impl CurrentWeatherFlow {
    fn new(operation: GetCurrentWeatherOperation) -> Result<Self, ValidationError> {
        let complete = ReturnWeather;
        let config = QueryStepConfig::new(
            "GetCurrentWeather",
            StepAnnotations::new(
                "current-weather",
                "Current weather",
                "Look up the current conditions for the requested coordinates.",
            ),
            operation,
            ConnectionRef::new("qweather", CONNECTION_NAME),
            |input: &GetCurrentWeatherInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_current_weather_step(config)?,
            complete,
        })
    }
}

impl Flow for CurrentWeatherFlow {
    type StartInput = GetCurrentWeatherInput;

    fn flow_type(&self) -> &'static str {
        "RustQWeatherCurrentWeather"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        api_host: SecretString::new(std::env::var("QWEATHER_API_HOST")?),
        api_key: SecretString::new(std::env::var("QWEATHER_API_KEY")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("qweather", CONNECTION_NAME),
        credentials,
    )]));
    let flow = CurrentWeatherFlow::new(GetCurrentWeatherOperation::new(
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
    struct WeatherResponse(AtomicUsize);

    impl ProviderHttpTransport for WeatherResponse {
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
                    br#"{"condition":{"text":"Few Clouds","code":"102"},"temperature":{"value":24,"unit":"C"},"validTime":"2024-05-31T03:15Z","metadata":{"attributions":["QWeather"]}}"#,
                )),
            ))
        }
    }

    #[test]
    fn current_weather_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(WeatherResponse::default());
        let connection = ConnectionRef::new("qweather", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                api_host: SecretString::new("abc123.xy.qweatherapi.com"),
                api_key: SecretString::new("test-key"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            CurrentWeatherFlow::new(GetCurrentWeatherOperation::with_http_client(
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
                "rust-current-weather-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(
                &new_flow()?,
                &flow_id,
                GetCurrentWeatherInput {
                    latitude: 39.92,
                    longitude: 116.41,
                },
            )?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: GetCurrentWeatherOutput = result.single_output()?;
            assert_eq!(output.condition_text, "Few Clouds");
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
