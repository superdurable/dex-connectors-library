// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_textin::{
    Config, Credentials, GetAccountBalanceInput, GetAccountBalanceOperation,
    GetAccountBalanceOutput, new_get_account_balance_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "textin";

// dex:group group-id:account-balance group-label:"Account balance"
// dex:explanation text:"Complete with the provider's visible account balance."
struct ReturnBalance;

impl Step for ReturnBalance {
    type Input = QueryResult<Option<GetAccountBalanceOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct AccountBalanceFlow {
    query: QueryStep<GetAccountBalanceInput, GetAccountBalanceOperation>,
    complete: ReturnBalance,
}

impl AccountBalanceFlow {
    fn new(operation: GetAccountBalanceOperation) -> Result<Self, ValidationError> {
        let complete = ReturnBalance;
        let config = QueryStepConfig::new(
            "GetAccountBalance",
            StepAnnotations::new(
                "account-balance",
                "Account balance",
                "Read the available account balance.",
            ),
            operation,
            ConnectionRef::new("textin", CONNECTION_NAME),
            |input: &GetAccountBalanceInput| input.clone(),
            BTreeMap::from([("fetched".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_account_balance_step(config)?,
            complete,
        })
    }
}

impl Flow for AccountBalanceFlow {
    type StartInput = GetAccountBalanceInput;

    fn flow_type(&self) -> &'static str {
        "RustTextInAccountBalance"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        app_id: std::env::var("TEXTIN_APP_ID")?,
        secret_code: SecretString::new(std::env::var("TEXTIN_SECRET_CODE")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("textin", CONNECTION_NAME),
        credentials,
    )]));
    let flow = AccountBalanceFlow::new(GetAccountBalanceOperation::new(
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
    struct BalanceResponse(AtomicUsize);

    impl ProviderHttpTransport for BalanceResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            assert_eq!(request.url, "http://127.0.0.1:12345/user/finance/balance");
            let status = if self.0.fetch_add(1, Ordering::SeqCst) == 0 {
                503
            } else {
                200
            };
            Ok(ProviderHttpResponse::new(
                status,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(
                    r#"{"code":200,"data":{"available_balance":1.2300,"pay_type":2}}"#.as_bytes(),
                )),
            ))
        }
    }

    #[test]
    fn account_balance_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(BalanceResponse::default());
        let connection = ConnectionRef::new("textin", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                app_id: "app".into(),
                secret_code: SecretString::new("fixture-code"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            AccountBalanceFlow::new(GetAccountBalanceOperation::with_base_url(
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
                "rust-account-balance-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, GetAccountBalanceInput {})?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: GetAccountBalanceOutput = result.single_output()?;

            assert_eq!(output.available_t_coins, "1.2300");
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
