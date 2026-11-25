// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Real Dex verification of the Flow-start Trigger target.
//!
//! Start `dexcli dev -dex-port 19911 -web-port 19912 -open=false`, then run
//! `DEX_SERVER_ADDRESS=127.0.0.1:19911 cargo test --test trigger_dex_integration -- --ignored`.

use std::sync::{Arc, LazyLock};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use dex_connectors_sdk::Rfc3339NanoTimestamp;
use dex_connectors_sdk::dex_sdk::{
    Attribute, BlobCache, BlobCacheConfig, Client, ClientOptions, Context, Flow, FlowStatus,
    HandlerResult, PersistenceSchema, Registry, Step, StepDecision, StepList, Worker,
    WorkerOptions,
};
use dex_connectors_sdk::trigger::{
    DexFlowTriggerTargetConfig, TriggerContext, TriggerEvent, TriggerTarget,
    is_trigger_undeliverable, new_dex_flow_trigger_target,
};

static RECEIVED_TEXT: LazyLock<Attribute<String>> =
    LazyLock::new(|| Attribute::new("receivedText"));

#[derive(Default)]
struct RecordEvent;

impl Step for RecordEvent {
    type Input = String;

    fn execute(&self, context: &mut Context, input: String) -> HandlerResult<StepDecision> {
        RECEIVED_TEXT.set(context, input.clone())?;
        Ok(StepDecision::graceful_complete(input))
    }
}

#[derive(Default)]
struct TriggeredEventFlow {
    record: RecordEvent,
}

impl Flow for TriggeredEventFlow {
    type StartInput = String;

    fn steps(&self) -> StepList<'_, String> {
        StepList::start(&self.record)
    }

    fn persistence(&self) -> PersistenceSchema {
        PersistenceSchema::new().attribute(&RECEIVED_TEXT)
    }
}

fn server_address() -> String {
    std::env::var("DEX_SERVER_ADDRESS").unwrap_or_else(|_| "127.0.0.1:19911".to_string())
}

fn unique_suffix() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("clock after epoch")
        .as_nanos()
}

#[test]
#[ignore = "requires dexcli dev"]
fn flow_target_starts_once_per_event_and_marks_blank_ids_undeliverable() {
    let cache_directory = tempfile::tempdir().expect("cache directory");
    let cache = Arc::new(
        BlobCache::open(
            BlobCacheConfig::new(cache_directory.path().to_path_buf(), 16 * 1024 * 1024, 1000)
                .expect("cache config"),
        )
        .expect("cache opens"),
    );
    let registry = Registry::new()
        .register(TriggeredEventFlow::default())
        .expect("registry");
    let worker = Arc::new(
        Worker::try_new(
            registry.clone(),
            Arc::clone(&cache),
            WorkerOptions::new().server_address(server_address()),
        )
        .expect("worker"),
    );
    let client = Arc::new(
        Client::try_new(
            registry,
            Arc::clone(&cache),
            ClientOptions::new().server_address(server_address()),
        )
        .expect("client"),
    );
    let running_worker = Arc::clone(&worker);
    let worker_thread = thread::spawn(move || running_worker.start());
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .expect("runtime");
    runtime
        .block_on(async {
            tokio::time::timeout(Duration::from_secs(30), worker.wait_pull_registered()).await
        })
        .expect("registration in time")
        .expect("worker registered");

    let suffix = unique_suffix();
    let flow_id = format!("sdkrust-trigger-{suffix}");
    let resolved_flow_id = flow_id.clone();
    let target = new_dex_flow_trigger_target(DexFlowTriggerTargetConfig {
        client: Arc::clone(&client),
        flow: TriggeredEventFlow::default(),
        accepts_event: Box::new(|event: &TriggerEvent<String>| event.payload != "ignored"),
        resolve_flow_id: Box::new(move |event: &TriggerEvent<String>| {
            if event.payload == "blank" {
                String::new()
            } else {
                resolved_flow_id.clone()
            }
        }),
        map_to_flow_input: Box::new(|event: &TriggerEvent<String>| Ok(event.payload.clone())),
        logger: None,
    });
    let event = |id: &str, payload: &str| TriggerEvent {
        id: id.to_string(),
        occurred_at: Rfc3339NanoTimestamp::now_utc(),
        payload: payload.to_string(),
    };
    let context = TriggerContext::new();
    let event_id = format!("event-{suffix}");
    target
        .handle_trigger(&context, &event(&event_id, "hello"))
        .expect("first delivery starts the Flow");
    let result = client
        .wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))
        .expect("Flow completes");
    assert_eq!(result.status(), FlowStatus::Completed);
    // A redelivery after completion is a duplicate start, delivered without a second run.
    target
        .handle_trigger(&context, &event(&event_id, "hello"))
        .expect("redelivery is consumed as a duplicate");
    target
        .handle_trigger(&context, &event("ignored-event", "ignored"))
        .expect("filtered event is consumed");
    let blank = target
        .handle_trigger(&context, &event("blank-event", "blank"))
        .expect_err("blank Flow ID is undeliverable");
    assert!(is_trigger_undeliverable(blank.as_ref()));
    assert_eq!(
        result.single_output::<String>().expect("Flow output"),
        "hello"
    );

    worker.stop();
    worker_thread
        .join()
        .expect("worker thread joins")
        .expect("worker stops cleanly");
    cache.close().expect("cache closes");
}
