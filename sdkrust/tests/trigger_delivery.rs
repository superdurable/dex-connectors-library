// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Trigger delivery, runner, and webhook endpoint behavior.

use std::collections::HashMap;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use dex_connectors_sdk::trigger::{
    TriggerBindingRef, TriggerCancellation, TriggerConfig, TriggerContext, TriggerDefinition,
    TriggerError, TriggerEvent, TriggerLogAttributes, TriggerLogLevel, TriggerLogger, TriggerRef,
    TriggerSource, TriggerTarget, UndeliverableTriggerError, deliver_trigger,
    has_durable_trigger_inbox, new_trigger, prepare_trigger_delivery,
};
use dex_connectors_sdk::webhooktrigger::{
    WebhookEndpoint, WebhookEndpointConfig, WebhookVerificationUnavailableError,
};
use dex_connectors_sdk::{ConnectionRef, Rfc3339NanoTimestamp, StaticCredentialProvider};

#[derive(Default)]
struct RecordingLogger {
    records: Mutex<Vec<(TriggerLogLevel, String, TriggerLogAttributes)>>,
}

impl RecordingLogger {
    fn messages(&self) -> Vec<String> {
        self.records
            .lock()
            .expect("records lock")
            .iter()
            .map(|(_, message, _)| message.clone())
            .collect()
    }
}

impl TriggerLogger for RecordingLogger {
    fn is_enabled_for(&self, _level: TriggerLogLevel) -> bool {
        true
    }

    fn log(&self, level: TriggerLogLevel, message: &str, attributes: &TriggerLogAttributes) {
        self.records
            .lock()
            .expect("records lock")
            .push((level, message.to_string(), attributes.clone()));
    }
}

fn event(id: &str) -> TriggerEvent<String> {
    TriggerEvent {
        id: id.to_string(),
        occurred_at: Rfc3339NanoTimestamp::from_wire("2026-10-29T12:30:45Z").expect("timestamp"),
        payload: format!("payload-{id}"),
    }
}

struct FlakyTarget {
    failures_before_success: usize,
    calls: AtomicUsize,
    is_undeliverable: bool,
}

impl TriggerTarget<String> for FlakyTarget {
    fn handle_trigger(
        &self,
        _context: &TriggerContext,
        _event: &TriggerEvent<String>,
    ) -> Result<(), TriggerError> {
        let call = self.calls.fetch_add(1, Ordering::SeqCst);
        if self.is_undeliverable {
            return Err(Box::new(UndeliverableTriggerError::with_reason("rejected")));
        }
        if call < self.failures_before_success {
            return Err("temporarily unavailable".into());
        }
        Ok(())
    }
}

#[test]
fn deliver_trigger_retries_until_success_and_logs_recovery() {
    let logger = RecordingLogger::default();
    let target = FlakyTarget {
        failures_before_success: 1,
        calls: AtomicUsize::new(0),
        is_undeliverable: false,
    };
    deliver_trigger(&TriggerContext::new(), &target, &event("Ev1"), Some(&logger))
        .expect("delivered");
    assert_eq!(target.calls.load(Ordering::SeqCst), 2);
    assert_eq!(
        logger.messages(),
        vec![
            "trigger delivery failed; retrying".to_string(),
            "trigger delivered after retry".to_string()
        ]
    );
    let records = logger.records.lock().expect("records lock");
    assert_eq!(
        records[0].2.get("delay_ms").map(ToString::to_string),
        Some("250".to_string())
    );
}

#[test]
fn deliver_trigger_consumes_undeliverable_events() {
    let logger = RecordingLogger::default();
    let target = FlakyTarget {
        failures_before_success: 0,
        calls: AtomicUsize::new(0),
        is_undeliverable: true,
    };
    deliver_trigger(&TriggerContext::new(), &target, &event("Ev1"), Some(&logger))
        .expect("consumed");
    assert_eq!(target.calls.load(Ordering::SeqCst), 1);
    assert_eq!(
        logger.messages(),
        vec!["trigger event skipped: undeliverable".to_string()]
    );
}

#[test]
fn deliver_trigger_aborts_when_cancelled_during_wait() {
    let cancellation = TriggerCancellation::new();
    let context = TriggerContext::with_cancellation(cancellation.clone());
    let target = FlakyTarget {
        failures_before_success: usize::MAX,
        calls: AtomicUsize::new(0),
        is_undeliverable: false,
    };
    let canceller = thread::spawn(move || {
        thread::sleep(Duration::from_millis(50));
        cancellation.cancel();
    });
    let result = deliver_trigger(&context, &target, &event("Ev1"), Some(&RecordingLogger::default()));
    canceller.join().expect("canceller joins");
    assert!(result.is_err());
}

struct ReplayingTarget {
    replayed: Mutex<bool>,
    delivered: Mutex<Vec<String>>,
}

impl TriggerTarget<String> for ReplayingTarget {
    fn handle_trigger(
        &self,
        _context: &TriggerContext,
        event: &TriggerEvent<String>,
    ) -> Result<(), TriggerError> {
        if event.id == "bad" {
            return Err(Box::new(UndeliverableTriggerError::with_reason("bad")));
        }
        self.delivered
            .lock()
            .expect("delivered lock")
            .push(event.id.clone());
        Ok(())
    }

    fn replays_trigger_deliveries(&self) -> bool {
        true
    }

    fn replay_trigger_deliveries(&self, _context: &TriggerContext) -> Result<(), TriggerError> {
        *self.replayed.lock().expect("replayed lock") = true;
        Ok(())
    }
}

struct ListSource {
    events: Vec<TriggerEvent<String>>,
}

impl TriggerSource<String> for ListSource {
    fn run(
        &self,
        context: &TriggerContext,
        target: Arc<dyn TriggerTarget<String>>,
    ) -> Result<(), TriggerError> {
        for event in &self.events {
            prepare_trigger_delivery(context, target.as_ref(), event)?;
            deliver_trigger(context, target.as_ref(), event, None)?;
        }
        Ok(())
    }
}

#[test]
fn runner_replays_then_runs_source_and_consumes_undeliverable() {
    let target = Arc::new(ReplayingTarget {
        replayed: Mutex::new(false),
        delivered: Mutex::new(Vec::new()),
    });
    let definition = TriggerDefinition {
        trigger: TriggerRef::new("sample", "itemCreated"),
        description: "An item was created.".to_string(),
    };
    let binding = TriggerBindingRef {
        connection: ConnectionRef::new("sample", "main"),
        trigger: definition.trigger.clone(),
        name: "items".to_string(),
    };
    let logger = Arc::new(RecordingLogger::default());
    let runner = new_trigger(TriggerConfig {
        definition,
        binding,
        source: Arc::new(ListSource {
            events: vec![event("a"), event("bad"), event("b")],
        }),
        target: target.clone(),
        logger: Some(logger.clone()),
    })
    .expect("valid runner");
    runner.run(&TriggerContext::new()).expect("source finishes");
    assert!(*target.replayed.lock().expect("replayed lock"));
    assert_eq!(
        *target.delivered.lock().expect("delivered lock"),
        vec!["a".to_string(), "b".to_string()]
    );
    let records = logger.records.lock().expect("records lock");
    let skipped = records
        .iter()
        .find(|(_, message, _)| message == "trigger event skipped: undeliverable")
        .expect("skip logged");
    assert_eq!(
        skipped.2.get("binding").map(ToString::to_string),
        Some("items".to_string())
    );
    assert!(!has_durable_trigger_inbox::<String>(target.as_ref()));
}

#[test]
fn runner_rejects_mismatched_binding() {
    let definition = TriggerDefinition {
        trigger: TriggerRef::new("sample", "itemCreated"),
        description: "An item was created.".to_string(),
    };
    let binding = TriggerBindingRef {
        connection: ConnectionRef::new("sample", "main"),
        trigger: TriggerRef::new("sample", "itemDeleted"),
        name: "items".to_string(),
    };
    let result = new_trigger(TriggerConfig {
        definition,
        binding,
        source: Arc::new(ListSource { events: Vec::new() }),
        target: Arc::new(ReplayingTarget {
            replayed: Mutex::new(false),
            delivered: Mutex::new(Vec::new()),
        }),
        logger: None,
    });
    assert!(result.is_err());
}

fn endpoint(
    verify_result: fn(&str) -> Result<(), TriggerError>,
    source_queue_capacity: usize,
) -> WebhookEndpoint<String, String> {
    let connection = ConnectionRef::new("sample", "main");
    let credentials = Arc::new(StaticCredentialProvider::new(HashMap::from([(
        connection.clone(),
        "INERT_SECRET".to_string(),
    )])));
    let mut config = WebhookEndpointConfig::new(
        "sample",
        "itemCreated",
        connection,
        credentials,
        Box::new(move |request, credential: &String, _cancellation| {
            assert_eq!(credential, "INERT_SECRET");
            verify_result(request.header("x-signature").unwrap_or_default())
        }),
        Box::new(|request| {
            let body = String::from_utf8(request.body.clone())?;
            if body == "ignored" {
                return Ok(None);
            }
            if body.is_empty() {
                return Err("empty event".into());
            }
            Ok(Some(event(&body)))
        }),
        64,
    );
    config.source_queue_capacity = source_queue_capacity;
    config.arrival_timeout = Duration::from_secs(5);
    WebhookEndpoint::new(config).expect("valid endpoint")
}

fn verify_signature(signature: &str) -> Result<(), TriggerError> {
    match signature {
        "good" => Ok(()),
        "unavailable" => Err(Box::new(WebhookVerificationUnavailableError::new(None))),
        _ => Err("bad signature".into()),
    }
}

fn post(endpoint: &WebhookEndpoint<String, String>, signature: &str, body: &str) -> u16 {
    endpoint
        .handle_request(
            "POST",
            "/hooks/sample?x=1",
            &[("X-Signature".to_string(), signature.to_string())],
            body.as_bytes(),
            None,
        )
        .status
}

struct CollectingTarget {
    prepared: Mutex<Vec<String>>,
    delivered: Mutex<Vec<String>>,
}

impl TriggerTarget<String> for CollectingTarget {
    fn handle_trigger(
        &self,
        _context: &TriggerContext,
        event: &TriggerEvent<String>,
    ) -> Result<(), TriggerError> {
        self.delivered
            .lock()
            .expect("delivered lock")
            .push(event.id.clone());
        Ok(())
    }

    fn prepares_trigger_delivery(&self) -> bool {
        true
    }

    fn prepare_trigger(
        &self,
        _context: &TriggerContext,
        event: &TriggerEvent<String>,
    ) -> Result<(), TriggerError> {
        self.prepared
            .lock()
            .expect("prepared lock")
            .push(event.id.clone());
        Ok(())
    }
}

#[test]
fn webhook_endpoint_answers_statuses_like_python() {
    let endpoint = endpoint(verify_signature, 4);
    let response = endpoint.handle_request("GET", "/", &[], b"", None);
    assert_eq!(response.status, 405);
    assert_eq!(response.body, b"method not allowed\n");
    assert!(response.headers.contains(&("allow".to_string(), "POST".to_string())));
    assert_eq!(post(&endpoint, "good", &"x".repeat(65)), 413);
    assert_eq!(post(&endpoint, "bad", "Ev1"), 400);
    assert_eq!(post(&endpoint, "unavailable", "Ev1"), 503);
    assert_eq!(post(&endpoint, "good", ""), 400);
    assert_eq!(post(&endpoint, "good", "ignored"), 200);
    // A verified event without a running source cannot be recorded.
    assert_eq!(post(&endpoint, "good", "Ev1"), 503);
}

#[test]
fn webhook_endpoint_records_in_order_and_source_delivers() {
    let endpoint = Arc::new(endpoint(verify_signature, 4));
    let source = endpoint.new_source(Some(Box::new(|event: &TriggerEvent<String>| {
        event.id != "skip"
    })));
    let target = Arc::new(CollectingTarget {
        prepared: Mutex::new(Vec::new()),
        delivered: Mutex::new(Vec::new()),
    });
    let cancellation = TriggerCancellation::new();
    let context = TriggerContext::with_cancellation(cancellation.clone());
    let running_target: Arc<dyn TriggerTarget<String>> = target.clone();
    let running_source = Arc::clone(&source);
    let runner = thread::spawn(move || running_source.run(&context, running_target));
    while endpoint.running_source_count() == 0 {
        thread::yield_now();
    }
    assert_eq!(post(&endpoint, "good", "Ev1"), 200);
    assert_eq!(post(&endpoint, "good", "skip"), 200);
    assert_eq!(post(&endpoint, "good", "Ev2"), 200);
    assert_eq!(
        *target.prepared.lock().expect("prepared lock"),
        vec!["Ev1".to_string(), "Ev2".to_string()]
    );
    while target.delivered.lock().expect("delivered lock").len() < 2 {
        thread::yield_now();
    }
    cancellation.cancel();
    assert!(runner.join().expect("source joins").is_err());
    assert_eq!(
        *target.delivered.lock().expect("delivered lock"),
        vec!["Ev1".to_string(), "Ev2".to_string()]
    );
    assert_eq!(endpoint.running_source_count(), 0);
}

#[test]
fn webhook_endpoint_waits_for_every_created_source() {
    let endpoint = Arc::new(endpoint(verify_signature, 4));
    let first = endpoint.new_source(None);
    let _second = endpoint.new_source(None);
    let cancellation = TriggerCancellation::new();
    let context = TriggerContext::with_cancellation(cancellation.clone());
    let target: Arc<dyn TriggerTarget<String>> = Arc::new(CollectingTarget {
        prepared: Mutex::new(Vec::new()),
        delivered: Mutex::new(Vec::new()),
    });
    let runner = thread::spawn(move || first.run(&context, target));
    while endpoint.running_source_count() == 0 {
        thread::yield_now();
    }
    assert_eq!(post(&endpoint, "good", "Ev1"), 503);
    cancellation.cancel();
    assert!(runner.join().expect("source joins").is_err());
}
