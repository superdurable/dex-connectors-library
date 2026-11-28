// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::{BTreeMap, HashMap};
use std::error::Error;
use std::sync::Arc;

use dex_connector_istero::{
    Config, Credentials, GetAccountStatusInput, GetAccountStatusOperation, GetAccountStatusOutput,
    new_get_account_status_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, SecretString,
    StaticCredentialProvider, StepAnnotations, ValidationError,
};

const CONNECTION_NAME: &str = "istero";

// dex:group group-id:account-status group-label:"Account status"
// dex:explanation text:"Complete with the provider's visible account status."
struct ReturnAccount;

impl Step for ReturnAccount {
    type Input = QueryResult<Option<GetAccountStatusOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct AccountStatusFlow {
    query: QueryStep<GetAccountStatusInput, GetAccountStatusOperation>,
    complete: ReturnAccount,
}

impl AccountStatusFlow {
    fn new(operation: GetAccountStatusOperation) -> Result<Self, ValidationError> {
        let complete = ReturnAccount;
        let config = QueryStepConfig::new(
            "GetAccountStatus",
            StepAnnotations::new(
                "account-status",
                "Account status",
                "Read the connected account’s quota and status.",
            ),
            operation,
            ConnectionRef::new("istero", CONNECTION_NAME),
            |input: &GetAccountStatusInput| input.clone(),
            BTreeMap::from([("retrieved".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_get_account_status_step(config)?,
            complete,
        })
    }
}

impl Flow for AccountStatusFlow {
    type StartInput = GetAccountStatusInput;

    fn flow_type(&self) -> &'static str {
        "RustIsTeroAccountStatus"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let credentials = Credentials {
        isid: std::env::var("ISTERO_ISID")?,
        api_key: SecretString::new(std::env::var("ISTERO_API_KEY")?),
    };
    let provider = StaticCredentialProvider::new(HashMap::from([(
        ConnectionRef::new("istero", CONNECTION_NAME),
        credentials,
    )]));
    let flow = AccountStatusFlow::new(GetAccountStatusOperation::new(
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
            assert_eq!(
                request.url,
                "https://api.istero.com/resource/v1/isas/mine/information?isid=60066023"
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
                    r#"{"code":200,"data":{"isid":60066023,"today_used":3,"today_remaining":997}}"#
                        .as_bytes(),
                )),
            ))
        }
    }

    #[test]
    fn account_status_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(AccountResponse::default());
        let connection = ConnectionRef::new("istero", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            Credentials {
                isid: "60066023".into(),
                api_key: SecretString::new("private-test-token"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            AccountStatusFlow::new(GetAccountStatusOperation::with_http_client(
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
                "rust-account-status-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, GetAccountStatusInput {})?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: GetAccountStatusOutput = result.single_output()?;

            assert_eq!(output.isid, 60066023);
            assert_eq!(output.today_used, 3);
            assert_eq!(output.today_remaining, 997);
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
