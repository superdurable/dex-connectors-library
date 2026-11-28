// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use std::collections::BTreeMap;
use std::error::Error;
use std::sync::Arc;

use dex_connector_likelic::{
    ListRoomsInput, ListRoomsOperation, ListRoomsOutput, new_list_rooms_step,
};
use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Context, Flow, HandlerResult, Registry, Step, StepDecision,
    StepList, Worker, WorkerOptions,
};
use dex_connectors_sdk::{
    BranchTarget, ConnectionRef, QueryResult, QueryStep, QueryStepConfig, StepAnnotations,
    ValidationError,
};

const CONNECTION_NAME: &str = "likelic";

// dex:group group-id:room-directory group-label:"Room catalog"
// dex:explanation text:"Complete with the provider's visible room catalog."
struct ReturnRooms;

impl Step for ReturnRooms {
    type Input = QueryResult<Option<ListRoomsOutput>>;

    fn execute(&self, _: &mut Context, result: Self::Input) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(result.value))
    }
}

struct RoomDirectoryFlow {
    query: QueryStep<ListRoomsInput, ListRoomsOperation>,
    complete: ReturnRooms,
}

impl RoomDirectoryFlow {
    fn new(operation: ListRoomsOperation) -> Result<Self, ValidationError> {
        let complete = ReturnRooms;
        let config = QueryStepConfig::new(
            "ListRooms",
            StepAnnotations::new(
                "room-directory",
                "Room catalog",
                "Read the available room catalog.",
            ),
            operation,
            ConnectionRef::new("likelic", CONNECTION_NAME),
            |input: &ListRoomsInput| input.clone(),
            BTreeMap::from([("found".to_owned(), BranchTarget::to(&complete))]),
        );
        Ok(Self {
            query: new_list_rooms_step(config)?,
            complete,
        })
    }
}

impl Flow for RoomDirectoryFlow {
    type StartInput = ListRoomsInput;

    fn flow_type(&self) -> &'static str {
        "RustLikelicRoomDirectory"
    }
    fn steps(&self) -> StepList<'_, Self::StartInput> {
        StepList::start(&self.query).and(&self.complete)
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let project = dex_connectors_sdk::projectconfig::load_from_environment()?;
    let flow =
        RoomDirectoryFlow::new(ListRoomsOperation::from_project(&project, CONNECTION_NAME)?)?;
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
    struct RoomResponse(AtomicUsize);

    impl ProviderHttpTransport for RoomResponse {
        fn send(
            &self,
            request: &ProviderHttpRequest,
            _: Duration,
        ) -> Result<ProviderHttpResponse, ProviderHttpError> {
            let index = self.0.fetch_add(1, Ordering::SeqCst);
            assert_eq!(
                request.url,
                "https://agent.likelic.com/api/v1/rooms?page=1&limit=20"
            );
            let body = r#"{"success":true,"data":[{"channel":"wework","instance_id":3,"room_id":"room-123","push_state":"ready","room_name":"private group","company_name":"private customer","member_count":12}],"meta":{"page":1,"limit":20,"total":1}}"#;
            Ok(ProviderHttpResponse::new(
                if index == 0 { 503 } else { 200 },
                ResponseHeaders::new(vec![]),
                Box::new(std::io::Cursor::new(body.as_bytes())),
            ))
        }
    }

    #[test]
    fn room_directory_retries_and_completes() -> Result<(), Box<dyn Error>> {
        let provider = Arc::new(RoomResponse::default());
        let connection = ConnectionRef::new("likelic", CONNECTION_NAME);
        let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
            connection,
            dex_connector_likelic::Credentials {
                customer_code: "demo_company".into(),
                api_key: SecretString::new("sk-0123456789abcdef0123456789abcdef0123456789abcdef"),
            },
        )])));
        let new_flow = || {
            let http =
                ProviderHttpClient::with_transport(provider.clone(), Duration::from_secs(15))?;
            RoomDirectoryFlow::new(ListRoomsOperation::with_http_client(
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
                "rust-room-directory-{}",
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
            );
            client.start_flow(&new_flow()?, &flow_id, ListRoomsInput { page: 1 })?;
            let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
            assert_eq!(
                result.status(),
                FlowStatus::Completed,
                "{:?}",
                result.error_message()
            );
            let output: ListRoomsOutput = result.single_output()?;

            assert_eq!(output.rooms[0].channel, "wework");
            assert_eq!(output.rooms[0].instance_id, 3);
            assert_eq!(output.rooms[0].push_state, "ready");
            assert_eq!(output.rooms[0].room_id, "room-123");
            assert_eq!(output.page, 1);
            assert_eq!(output.total, 1);
            assert_eq!(provider.0.load(Ordering::SeqCst), 2);
            Ok(())
        })();
        worker.stop();
        thread.join().expect("Worker thread")?;
        cache.close()?;
        verification
    }
}
