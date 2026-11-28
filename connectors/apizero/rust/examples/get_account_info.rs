// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_apizero::{
    Config, Credentials, GetAccountInfoInput, GetAccountInfoOperation, GetAccountInfoOutput,
    new_get_account_info_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "apizero";

// dex:group group-id:account-info group-label:"Account information"
// dex:explanation text:"Complete with the provider's visible account information."
struct ReturnAccount;

impl Step for ReturnAccount {
    type Input = QueryResult<Option<GetAccountInfoOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct AccountInfoFlow {
    query: QueryStep<GetAccountInfoInput, GetAccountInfoOperation>,
    complete: ReturnAccount,
}

impl AccountInfoFlow {
    fn new(operation: GetAccountInfoOperation) -> Result<Self, ValidationError> {
        let complete = ReturnAccount;
        let config = QueryStepConfig::new(
            "GetAccountInfo",
            StepAnnotations::new(
                "account-info",
                "Account information",
                "Read wallet or prepaid quota information.",
            ),
            operation,
            ConnectionRef::new("apizero", CONNECTION_NAME),
            |input: &GetAccountInfoInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_account_info_step(config)?,
            complete,
        })
    }
}

impl Flow for AccountInfoFlow {
    type StartInput = GetAccountInfoInput;

    fn flow_type(&self) -> &'static str {
        "RustApiZeroAccountInfo"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        api_key: SecretString::new(std::env::var("APIZERO_API_KEY")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("apizero", CONNECTION_NAME),
        credentials,
    )]));
    let flow = AccountInfoFlow::new(GetAccountInfoOperation::new(
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
    struct AccountResponse(AtomicUsize);

    impl ProviderHttpTransport for AccountResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            assert_eq!(request.url, "https://v1.apizero.cn/api/user-info");
            let status = if self.0.fetch_add(1, Ordering::SeqCst) == 0 {
                503
            } else {
                200
            };
            Ok(ProviderHttpResponse::new(
                status,
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(if status == 503 {
                    &b"unavailable"[..]
                } else {
                    r#"{"code":0,"data":{"scope":"account","balance":{"yuan":"123.4000"},"key":{"quota_remaining":99}}}"#.as_bytes()
                })),
            ))
        }
    }

    #[test]
    fn account_info_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(AccountResponse::default());
        let connection = ConnectionRef::new("apizero", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                api_key: SecretString::new("sk_live_test-secret"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            AccountInfoFlow::new(GetAccountInfoOperation::with_http_client(
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
                "rust-account-info-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, GetAccountInfoInput {})?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: GetAccountInfoOutput = result.single_output()?;

            assert_eq!(output.scope, "account");
            assert_eq!(output.balance_yuan.as_deref(), Some("123.4000"));
            assert!(output.key_quota_remaining.is_none());
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
