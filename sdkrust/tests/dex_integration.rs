// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Real Dex verification of Connector Steps and the single-dispatch Fence.
//!
//! Start `dexcli dev -dex-port 19901 -web-port 19902 -open=false`, then run
//! `DEX_SERVER_ADDRESS=127.0.0.1:19901 cargo test --test dex_integration -- --ignored`.

use std::collections::BTreeMap;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, LazyLock, Mutex};
use std::thread::{self, JoinHandle};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use dex_connectors_sdk::dex_sdk::{
    Attribute, BlobCache, BlobCacheConfig, Client, ClientOptions, Context, Flow, FlowStatus,
    HandlerResult, PersistenceSchema, Registry, Step, StepDecision, StepList, Stream, Worker,
    WorkerOptions,
};
use dex_connectors_sdk::providerhttp::{
    AddressPolicy, ProviderHttpClient, ProviderHttpRequest, read_bounded_body,
};
use dex_connectors_sdk::singledispatch::Fence;
use dex_connectors_sdk::{
    BranchDefinition, BranchTarget, Call, CallId, ConnectionRef, ExecuteDurability, Failure,
    FailureKind, IdempotencyKey, Mutation, MutationAttempt, MutationDefinition, MutationResult,
    MutationStep, MutationStepConfig, OperationRef, Progress, ProgressUpdate, Query, QueryAttempt,
    QueryDefinition, QueryResult, QueryStep, QueryStepConfig, Receipt, RepeatOnUnknown,
    StepAnnotations, StepDefaults, StepOptionsOverride,
};

type ItemResult = QueryResult<Option<String>>;
type CreateResult = MutationResult<Option<String>>;

static QUERY_RESULT: LazyLock<Attribute<ItemResult>> =
    LazyLock::new(|| Attribute::new("queryResult"));
static QUERY_PROGRESS: LazyLock<Stream<ProgressUpdate>> =
    LazyLock::new(|| Stream::new("queryProgress", 64 * 1024));
static QUERY_TEXT: LazyLock<Stream<String>> = LazyLock::new(|| Stream::new("queryText", 64 * 1024));

fn server_address() -> String {
    std::env::var("DEX_SERVER_ADDRESS").unwrap_or_else(|_| "127.0.0.1:19901".to_string())
}

fn unique_flow_id(prefix: &str) -> String {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("clock after epoch")
        .as_nanos();
    format!("sdkrust-{prefix}-{nanos}")
}

/// Answers scripted HTTP responses; `None` drops the connection after reading the request.
struct ScriptedProvider {
    address: String,
    requests: Arc<AtomicU32>,
    _thread: JoinHandle<()>,
}

impl ScriptedProvider {
    fn start(responses: Vec<Option<(u16, &'static str)>>) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind fake provider");
        let address = format!("http://{}", listener.local_addr().expect("address"));
        let requests = Arc::new(AtomicU32::new(0));
        let counter = Arc::clone(&requests);
        let thread = thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(stream) = stream else { return };
                let index = counter.fetch_add(1, Ordering::SeqCst) as usize;
                let response = responses
                    .get(index)
                    .or_else(|| responses.last())
                    .copied()
                    .flatten();
                answer(stream, response);
            }
        });
        Self {
            address,
            requests,
            _thread: thread,
        }
    }

    fn request_count(&self) -> u32 {
        self.requests.load(Ordering::SeqCst)
    }
}

fn answer(stream: TcpStream, response: Option<(u16, &'static str)>) {
    let mut reader = BufReader::new(stream);
    let mut content_length = 0_usize;
    loop {
        let mut line = String::new();
        if reader.read_line(&mut line).unwrap_or(0) == 0 || line == "\r\n" {
            break;
        }
        if let Some(value) = line.to_ascii_lowercase().strip_prefix("content-length:") {
            content_length = value.trim().parse().unwrap_or(0);
        }
    }
    let mut body = vec![0_u8; content_length];
    let _ignored_short_body = reader.read_exact(&mut body);
    let Some((status, text)) = response else {
        return;
    };
    let mut stream = reader.into_inner();
    let reply = format!(
        "HTTP/1.1 {status} Scripted\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{text}",
        text.len()
    );
    let _ignored_closed_client = stream.write_all(reply.as_bytes());
}

fn item_definition() -> QueryDefinition {
    QueryDefinition {
        operation: OperationRef::new("sample", "getItem"),
        branches: Some(vec![
            BranchDefinition::new("found", "Item exists", false),
            BranchDefinition::new("missing", "Item does not exist", true),
            BranchDefinition::new("defect", "Local defect", true),
        ]),
        step_defaults: StepDefaults {
            execute_method_timeout: Duration::from_secs(10),
            execute_durability: ExecuteDurability::Async,
            ..StepDefaults::default()
        },
    }
}

/// Reads one item over HTTP from inside the Dex Worker's Step handler.
struct GetItem {
    endpoint: String,
    http: ProviderHttpClient,
}

impl Query for GetItem {
    type Input = String;
    type Output = String;

    fn definition(&self) -> QueryDefinition {
        item_definition()
    }

    fn invoke(&self, call: &Call<'_>, input: &String) -> QueryAttempt<String> {
        let failure = |kind, message: &str| Failure::new(kind, "sample", "getItem", message);
        call.report_progress(Progress {
            phase: "request".to_string(),
            message: "Fetching an item".to_string(),
            percent: Some(0.5),
            provider_sequence: Some(7),
        })
        .expect("valid progress");
        call.write_text("response ready");
        let request = ProviderHttpRequest::new("GET", format!("{}/items/{input}", self.endpoint));
        let unreachable =
            || QueryAttempt::retry(failure(FailureKind::Transport, "provider unreachable"), 0.0);
        let Ok(mut response) = self.http.send_traced(&request) else {
            return unreachable();
        };
        match response.status() {
            200..=299 => match read_bounded_body(&mut response, 1 << 20) {
                Ok(body) => QueryAttempt::branch(
                    "found",
                    String::from_utf8_lossy(&body).into_owned(),
                    Receipt::default(),
                    None,
                ),
                Err(_) => unreachable(),
            },
            404 => QueryAttempt::branch(
                "missing",
                String::new(),
                Receipt::default(),
                Some(failure(FailureKind::NotFound, "item was not found")),
            ),
            _ => QueryAttempt::retry(
                failure(FailureKind::Availability, "provider unavailable"),
                1.0,
            ),
        }
    }
}

/// Provider client for the loopback fake provider.
fn provider_client() -> ProviderHttpClient {
    ProviderHttpClient::new(Duration::from_secs(10), AddressPolicy::PrivateNetworkHttp)
        .expect("valid provider client")
}

/// Completes the Flow with the routed result.
struct ResultTerminal<Result> {
    step_type: &'static str,
    result: std::marker::PhantomData<fn(Result)>,
}

impl<Result> ResultTerminal<Result> {
    fn new(step_type: &'static str) -> Self {
        Self {
            step_type,
            result: std::marker::PhantomData,
        }
    }
}

impl<Result: dex_connectors_sdk::dex_sdk::Value> Step for ResultTerminal<Result> {
    type Input = Result;

    fn execute(&self, _context: &mut Context, input: Result) -> HandlerResult<StepDecision> {
        Ok(StepDecision::graceful_complete(input))
    }

    fn step_type(&self) -> &'static str {
        self.step_type
    }
}

struct ItemFlow {
    flow_type: &'static str,
    get_item: QueryStep<String, GetItem>,
    found: ResultTerminal<ItemResult>,
    missing: ResultTerminal<ItemResult>,
}

impl ItemFlow {
    fn new(flow_type: &'static str, endpoint: String, should_wire_missing: bool) -> Self {
        let found = ResultTerminal::new("Found");
        let missing = ResultTerminal::new("Missing");
        let mut branches = BTreeMap::from([("found".to_string(), BranchTarget::to(&found))]);
        if should_wire_missing {
            // The missing target records the distinct business outcome before completion.
            branches.insert("missing".to_string(), BranchTarget::to(&missing));
        }
        let mut config = QueryStepConfig::new(
            "GetItem",
            StepAnnotations::new("sample", "Sample", "Get one sample item."),
            GetItem {
                endpoint,
                http: provider_client(),
            },
            ConnectionRef::new("sample", "main"),
            |input: &String| input.clone(),
            branches,
        );
        config.result_attribute = Some(&*QUERY_RESULT);
        config.progress_stream = Some(&*QUERY_PROGRESS);
        config.text_stream = Some(&*QUERY_TEXT);
        Self {
            flow_type,
            get_item: QueryStep::new(config).expect("valid Query Step"),
            found,
            missing,
        }
    }
}

impl Flow for ItemFlow {
    type StartInput = String;

    fn flow_type(&self) -> &'static str {
        self.flow_type
    }

    fn steps(&self) -> StepList<'_, String> {
        StepList::start(&self.get_item)
            .and(&self.found)
            .and(&self.missing)
    }

    fn persistence(&self) -> PersistenceSchema {
        PersistenceSchema::new()
            .attribute(&QUERY_RESULT)
            .stream(&QUERY_PROGRESS)
            .stream(&QUERY_TEXT)
    }
}

/// Creates one item with a fenced, limited-resend write and no read-back.
struct CreateItem {
    endpoint: String,
    http: ProviderHttpClient,
}

impl Mutation for CreateItem {
    type Input = String;
    type Output = String;

    fn definition(&self) -> MutationDefinition {
        MutationDefinition {
            operation: OperationRef::new("sample", "createItem"),
            branches: Some(vec![
                BranchDefinition::new("created", "Item created", false),
                BranchDefinition::new("uncertain", "Outcome cannot be confirmed", true),
                BranchDefinition::new("defect", "Local defect", true),
            ]),
            step_defaults: StepDefaults {
                execute_method_timeout: Duration::from_secs(10),
                execute_durability: ExecuteDurability::Async,
                ..StepDefaults::default()
            },
            repeat_on_unknown: Some(RepeatOnUnknown::Limited),
            max_sends: 2,
        }
    }

    fn idempotency_key(&self, _call_id: &CallId, _input: &String) -> IdempotencyKey {
        IdempotencyKey::default()
    }

    fn invoke(&self, call: &Call<'_>, input: &String) -> MutationAttempt<String> {
        let failure =
            |message: &str| Failure::new(FailureKind::Transport, "sample", "createItem", message);
        let fence =
            Fence::new(call, Duration::from_secs(10), call.max_sends()).expect("valid Fence");
        let decision = fence.decide();
        if !decision.kind.permits_dispatch() {
            // No read-back exists, so an unconfirmed earlier send is uncertain.
            return MutationAttempt::uncertain(
                String::new(),
                failure("write outcome cannot be confirmed"),
                Receipt::default(),
            );
        }
        let url = format!("{}/items", self.endpoint);
        let request = ProviderHttpRequest::new("POST", url).body(input.as_bytes());
        let sent = fence.dispatch(|deadline| {
            let mut response = self
                .http
                .send_traced_before(&request, deadline)
                .map_err(|error| error.to_string())?;
            if !(200..300).contains(&response.status()) {
                return Err(format!("provider HTTP {}", response.status()));
            }
            read_bounded_body(&mut response, 1 << 20)
                .map(|body| String::from_utf8_lossy(&body).into_owned())
                .map_err(|error| error.to_string())
        });
        match sent {
            Ok(Ok(body)) => MutationAttempt::branch("created", body, Receipt::default(), None),
            Ok(Err(_)) => MutationAttempt::retry(failure("response lost after dispatch"), 0.0),
            Err(_) => MutationAttempt::retry(failure("dispatch refused before send"), 0.0),
        }
    }
}

struct CreateFlow {
    create_item: MutationStep<String, CreateItem>,
    created: ResultTerminal<CreateResult>,
    uncertain: ResultTerminal<CreateResult>,
}

impl CreateFlow {
    fn new(endpoint: String) -> Self {
        let created = ResultTerminal::new("Created");
        let uncertain = ResultTerminal::new("Uncertain");
        // The uncertain target hands the unconfirmed write to reconciliation.
        let branches = BTreeMap::from([
            ("created".to_string(), BranchTarget::to(&created)),
            ("uncertain".to_string(), BranchTarget::to(&uncertain)),
        ]);
        let mut config = MutationStepConfig::new(
            "CreateItem",
            StepAnnotations::new("sample", "Sample", "Create one sample item."),
            CreateItem {
                endpoint,
                http: provider_client(),
            },
            ConnectionRef::new("sample", "main"),
            |input: &String| input.clone(),
            branches,
        );
        config.step_options_override = Some(StepOptionsOverride {
            execute_retry: Some(
                dex_connectors_sdk::dex_sdk::RetryPolicy::new()
                    .initial_interval(Duration::from_secs(1))
                    .backoff_coefficient(1.0)
                    .maximum_interval(Duration::from_secs(1))
                    .total_duration(Duration::from_secs(60)),
            ),
            ..StepOptionsOverride::default()
        });
        Self {
            create_item: MutationStep::new(config).expect("valid Mutation Step"),
            created,
            uncertain,
        }
    }
}

impl Flow for CreateFlow {
    type StartInput = String;

    fn flow_type(&self) -> &'static str {
        "SdkRustCreateItemFlow"
    }

    fn steps(&self) -> StepList<'_, String> {
        StepList::start(&self.create_item)
            .and(&self.created)
            .and(&self.uncertain)
    }
}

/// One Worker and Client over a shared cache; stops and closes on drop.
struct DexEnvironment {
    client: Client,
    worker: Arc<Worker>,
    worker_thread: Option<JoinHandle<()>>,
    worker_failure: Arc<Mutex<Option<String>>>,
    cache: Arc<BlobCache>,
    _cache_directory: tempfile::TempDir,
}

impl DexEnvironment {
    fn start(registry: Registry) -> Self {
        let cache_directory = tempfile::tempdir().expect("cache directory");
        let cache = Arc::new(
            BlobCache::open(
                BlobCacheConfig::new(cache_directory.path(), 16 * 1024 * 1024, 1_000)
                    .expect("cache config"),
            )
            .expect("open cache"),
        );
        let worker = Arc::new(
            Worker::try_new(
                registry.clone(),
                Arc::clone(&cache),
                WorkerOptions::new().server_address(server_address()),
            )
            .expect("Worker options"),
        );
        let worker_failure = Arc::new(Mutex::new(None));
        let running_worker = Arc::clone(&worker);
        let failure = Arc::clone(&worker_failure);
        let worker_thread = thread::spawn(move || {
            if let Err(error) = running_worker.start() {
                *failure.lock().expect("failure lock") = Some(error.to_string());
            }
        });
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .expect("runtime");
        runtime
            .block_on(async {
                tokio::time::timeout(Duration::from_secs(30), worker.wait_pull_registered()).await
            })
            .expect("Worker registers in time")
            .expect("Worker registers");
        let client = Client::try_new(
            registry,
            Arc::clone(&cache),
            ClientOptions::new().server_address(server_address()),
        )
        .expect("Client options");
        Self {
            client,
            worker,
            worker_thread: Some(worker_thread),
            worker_failure,
            cache,
            _cache_directory: cache_directory,
        }
    }
}

impl Drop for DexEnvironment {
    fn drop(&mut self) {
        self.worker.stop();
        if let Some(thread) = self.worker_thread.take() {
            let _ignored_panic = thread.join();
        }
        let worker_failure = self.worker_failure.lock().expect("failure lock").take();
        let _ignored_close = self.cache.close();
        if let Some(error) = worker_failure
            && !thread::panicking()
        {
            panic!("Worker stopped with error: {error}");
        }
    }
}

fn run_item_flow(
    provider: &ScriptedProvider,
    flow_type: &'static str,
    should_wire_missing: bool,
) -> (DexEnvironment, String) {
    let flow = ItemFlow::new(flow_type, provider.address.clone(), should_wire_missing);
    let registry = Registry::new().register(flow).expect("register Flow");
    let environment = DexEnvironment::start(registry);
    let flow_id = unique_flow_id(flow_type);
    let start_flow = ItemFlow::new(flow_type, provider.address.clone(), should_wire_missing);
    environment
        .client
        .start_flow(&start_flow, &flow_id, "item-1".to_string())
        .expect("start Flow");
    (environment, flow_id)
}

#[test]
#[ignore = "requires dexcli dev; see the module documentation"]
fn query_step_routes_result_writes_outputs_and_retries() {
    let provider = ScriptedProvider::start(vec![Some((503, "busy")), Some((200, "object-42"))]);
    let (environment, flow_id) = run_item_flow(&provider, "SdkRustItemFlow", true);
    let result = environment
        .client
        .wait_for_flow_with_timeout(&flow_id, Duration::from_secs(60))
        .expect("Flow completes");
    assert_eq!(
        result.status(),
        FlowStatus::Completed,
        "{:?}",
        result.error_message()
    );
    let output: ItemResult = result.single_output().expect("routed result");
    assert_eq!(output.branch, "found");
    assert_eq!(output.value.as_deref(), Some("object-42"));
    assert_eq!(output.receipt.provider, "sample");
    assert!(!output.receipt.observed_at.is_zero());
    assert_eq!(provider.request_count(), 2, "one retry after a 503");
    let progress = environment
        .client
        .list_stream_messages(&flow_id, &QUERY_PROGRESS, 10, "")
        .expect("progress Stream");
    assert!(!progress.messages.is_empty());
}

#[test]
#[ignore = "requires dexcli dev; see the module documentation"]
fn unwired_optional_branch_fails_the_flow() {
    let provider = ScriptedProvider::start(vec![Some((404, "absent"))]);
    let (environment, flow_id) = run_item_flow(&provider, "SdkRustUnwiredFlow", false);
    let result = environment
        .client
        .wait_for_flow_with_timeout(&flow_id, Duration::from_secs(60))
        .expect("Flow closes");
    assert_eq!(result.status(), FlowStatus::Failed);
    let message = result.error_message().unwrap_or_default();
    assert!(
        message.contains("selected branch missing with no target"),
        "{message}"
    );
    assert!(
        message.contains("NOT_FOUND: item was not found"),
        "{message}"
    );
}

#[test]
#[ignore = "requires dexcli dev; see the module documentation"]
fn fenced_mutation_resends_up_to_its_cap_then_selects_uncertain() {
    let provider = ScriptedProvider::start(vec![None]);
    let flow = CreateFlow::new(provider.address.clone());
    let registry = Registry::new().register(flow).expect("register Flow");
    let environment = DexEnvironment::start(registry);
    let flow_id = unique_flow_id("create");
    environment
        .client
        .start_flow(
            &CreateFlow::new(provider.address.clone()),
            &flow_id,
            "item-1".to_string(),
        )
        .expect("start Flow");
    let result = environment
        .client
        .wait_for_flow_with_timeout(&flow_id, Duration::from_secs(60))
        .expect("Flow completes");
    assert_eq!(
        result.status(),
        FlowStatus::Completed,
        "{:?}",
        result.error_message()
    );
    let output: CreateResult = result.single_output().expect("routed result");
    assert_eq!(output.branch, "uncertain");
    assert_eq!(
        provider.request_count(),
        2,
        "the limited cap of two sends survives Dex retries through heartbeat checkpoints"
    );
    assert_eq!(
        output.receipt.idempotency_key.as_str(),
        output.receipt.call_id.as_str()
    );
}

/// Blocks inside `invoke` until the test releases it, so a second call overlaps the first.
struct GatedCreate {
    gate: Arc<(Mutex<GateState>, std::sync::Condvar)>,
}

#[derive(Default)]
struct GateState {
    is_first_invoking: bool,
    is_released: bool,
}

impl Mutation for GatedCreate {
    type Input = String;
    type Output = String;

    fn definition(&self) -> MutationDefinition {
        MutationDefinition {
            operation: OperationRef::new("sample", "createItem"),
            branches: Some(vec![
                BranchDefinition::new("created", "Item created", false),
                BranchDefinition::new("defect", "Local defect", true),
            ]),
            step_defaults: StepDefaults::default(),
            repeat_on_unknown: Some(RepeatOnUnknown::Unsafe),
            max_sends: 0,
        }
    }

    fn idempotency_key(&self, _call_id: &CallId, _input: &String) -> IdempotencyKey {
        IdempotencyKey::default()
    }

    fn invoke(&self, _call: &Call<'_>, input: &String) -> MutationAttempt<String> {
        let (state, signal) = &*self.gate;
        let mut state = state.lock().expect("gate lock");
        state.is_first_invoking = true;
        signal.notify_all();
        let (state, _timeout) = signal
            .wait_timeout_while(state, Duration::from_secs(10), |state| !state.is_released)
            .expect("gate wait");
        assert!(state.is_released, "the overlapping call returned in time");
        MutationAttempt::branch("created", input.clone(), Receipt::default(), None)
    }
}

/// Runs Mutations directly from one Execute to exercise the public low-level entries.
struct DirectMutationStep {
    gate: Arc<(Mutex<GateState>, std::sync::Condvar)>,
}

impl Step for DirectMutationStep {
    type Input = String;

    fn execute(&self, context: &mut Context, input: String) -> HandlerResult<StepDecision> {
        let operation = GatedCreate {
            gate: Arc::clone(&self.gate),
        };
        let connection = ConnectionRef::new("sample", "main");
        let attempt = dex_connectors_sdk::StepAttempt::new(context);
        let call = attempt
            .new_call(
                connection.clone(),
                OperationRef::new("sample", "createItem"),
            )
            .map_err(dex_connectors_sdk::dex_sdk::HandlerError::from_error)?;
        let unadmitted = match dex_connectors_sdk::run_mutation(&operation, &call, &input) {
            dex_connectors_sdk::OperationOutcome::Completed(result) => result,
            dex_connectors_sdk::OperationOutcome::Retry(retry) => {
                return Err(dex_connectors_sdk::dex_retry_error(&retry));
            }
        };
        let (first, second) = thread::scope(|scope| {
            let first = scope.spawn(|| {
                dex_connectors_sdk::run_step_mutation(
                    &operation,
                    &attempt,
                    connection.clone(),
                    &input,
                    None,
                )
            });
            {
                let (state, signal) = &*self.gate;
                let state = state.lock().expect("gate lock");
                let _first_invoking = signal
                    .wait_timeout_while(state, Duration::from_secs(10), |state| {
                        !state.is_first_invoking
                    })
                    .expect("gate wait");
            }
            let second = dex_connectors_sdk::run_step_mutation(
                &operation,
                &attempt,
                connection.clone(),
                &input,
                None,
            );
            {
                let (state, signal) = &*self.gate;
                state.lock().expect("gate lock").is_released = true;
                signal.notify_all();
            }
            (first.join().expect("first call thread"), second)
        });
        let mut summary: Vec<String> = [unadmitted]
            .into_iter()
            .chain([first, second].into_iter().map(|outcome| match outcome {
                dex_connectors_sdk::OperationOutcome::Completed(result) => result,
                dex_connectors_sdk::OperationOutcome::Retry(retry) => panic!("retry: {retry}"),
            }))
            .map(|result| format!("{} {}", result.branch, result.receipt.call_id))
            .collect();
        summary.push(attempt.step_execution_id().to_string());
        Ok(StepDecision::graceful_complete(summary))
    }

    fn step_type(&self) -> &'static str {
        "DirectMutation"
    }
}

struct DirectMutationFlow {
    step: DirectMutationStep,
}

impl Flow for DirectMutationFlow {
    type StartInput = String;

    fn flow_type(&self) -> &'static str {
        "SdkRustDirectMutationFlow"
    }

    fn steps(&self) -> StepList<'_, String> {
        StepList::start(&self.step)
    }
}

#[test]
#[ignore = "requires dexcli dev; see the module documentation"]
fn low_level_mutation_entries_keep_call_ids_in_admission_order() {
    let new_flow = || DirectMutationFlow {
        step: DirectMutationStep {
            gate: Arc::new((Mutex::new(GateState::default()), std::sync::Condvar::new())),
        },
    };
    let registry = Registry::new().register(new_flow()).expect("register Flow");
    let environment = DexEnvironment::start(registry);
    let flow_id = unique_flow_id("direct");
    environment
        .client
        .start_flow(&new_flow(), &flow_id, "item-1".to_string())
        .expect("start Flow");
    let result = environment
        .client
        .wait_for_flow_with_timeout(&flow_id, Duration::from_secs(60))
        .expect("Flow completes");
    assert_eq!(
        result.status(),
        FlowStatus::Completed,
        "{:?}",
        result.error_message()
    );
    let summary: Vec<String> = result.single_output().expect("summary");
    assert_eq!(summary.len(), 4, "{summary:?}");
    let operation = OperationRef::new("sample", "createItem");
    let expected = |branch: &str, occurrence: u32| {
        let call_id =
            dex_connectors_sdk::derive_call_id(&flow_id, &summary[3], &operation, occurrence)
                .expect("Call ID");
        format!("{branch} {call_id}")
    };
    // new_call took occurrence 1 and was refused before admission; the admitted call then
    // owns 2 and the overlapping call counts 3, in admission order.
    assert_eq!(summary[0], expected("defect", 1));
    assert_eq!(summary[1], expected("created", 2));
    assert_eq!(summary[2], expected("defect", 3));
}
