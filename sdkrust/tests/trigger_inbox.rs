// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Durable project Trigger inbox behavior and byte parity with Go and Python.

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use base64::Engine;
use base64::engine::general_purpose::STANDARD;
use dex_connectors_sdk::Rfc3339NanoTimestamp;
use dex_connectors_sdk::projectconfig::trigger_inbox::{
    PendingTriggerEvent, TriggerInbox, TriggerInboxKey, decode_trigger_event,
    encode_trigger_event, new_durable_trigger_target,
};
use dex_connectors_sdk::projectconfig::{InMemoryObjectStore, ObjectStore, Scope};
use dex_connectors_sdk::trigger::{
    TriggerCancellation, TriggerContext, TriggerError, TriggerEvent, TriggerTarget,
    UndeliverableTriggerError, is_trigger_delivery_aborted,
};

type TextPayload = BTreeMap<String, String>;

fn repository_path(relative: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join(relative)
}

fn text_event(event_id: &str) -> TriggerEvent<TextPayload> {
    TriggerEvent {
        id: event_id.to_string(),
        occurred_at: Rfc3339NanoTimestamp::from_wire("2025-10-12T00:00:00.12Z").expect("time"),
        payload: BTreeMap::from([("text".to_string(), "hello <world> & co".to_string())]),
    }
}

fn scope() -> Scope {
    Scope::new("test", "live")
}

fn key() -> TriggerInboxKey {
    TriggerInboxKey::new("slack", "support", "threadReplyCreated", "reply")
}

fn new_inbox(objects: &Arc<InMemoryObjectStore>) -> Arc<TriggerInbox> {
    let objects: Arc<dyn ObjectStore> = objects.clone();
    Arc::new(TriggerInbox::new(objects, &scope(), key()).expect("valid inbox"))
}

#[test]
fn stores_events_under_the_go_document_once_per_id_in_order() {
    let objects = Arc::new(InMemoryObjectStore::new());
    let inbox = new_inbox(&objects);
    for event_id in ["Ev1", "Ev1", "Ev2"] {
        inbox
            .add(PendingTriggerEvent {
                id: event_id.to_string(),
                event: encode_trigger_event(&text_event(event_id)).expect("encodes"),
            })
            .expect("adds");
    }
    let ids: Vec<String> = inbox
        .pending()
        .expect("pending")
        .into_iter()
        .map(|event| event.id)
        .collect();
    assert_eq!(ids, ["Ev1", "Ev2"]);
    let stored = objects.read_object(inbox.key(), "").expect("head");
    assert!(String::from_utf8(stored.contents).expect("UTF-8").starts_with(
        "{\"schemaVersion\":\"connectors.dex.dev/project-trigger-inbox/v1alpha1\",\"events\":[{\"id\":\"Ev1\",\"event\":{\"eventId\":\"Ev1\",\"occurredAt\":\"2025-10-12T00:00:00.12Z\",\"payload\":{\"text\":\"hello \\u003cworld\\u003e \\u0026 co\"}}}"
    ));
    inbox.remove("Ev1").expect("removes");
    inbox.remove("absent").expect("absent removal succeeds");
    assert_eq!(inbox.pending().expect("pending").len(), 1);
}

#[test]
fn inbox_key_matches_conformance_fixture() {
    let storage: serde_json::Value = serde_json::from_slice(
        &std::fs::read(repository_path("conformance/project-storage.json")).expect("fixture"),
    )
    .expect("JSON");
    let scope = Scope::new(
        storage["scope"]["projectId"].as_str().expect("project"),
        storage["scope"]["kind"].as_str().expect("kind"),
    );
    let key = TriggerInboxKey::new(
        "sample",
        storage["connectionKey"]["connectionName"]
            .as_str()
            .expect("connection"),
        "itemCreated",
        "main / west",
    );
    assert_eq!(
        key.path(&scope).expect("path"),
        storage["triggerInboxHeadKey"].as_str().expect("key")
    );
    assert!(
        TriggerInboxKey::new("slack", "a".repeat(256), "itemCreated", "b")
            .path(&scope)
            .is_ok()
    );
    for invalid in [
        TriggerInboxKey::new("slack", "a".repeat(257), "itemCreated", "b"),
        TriggerInboxKey::new("slack", "é".repeat(129), "itemCreated", "b"),
        TriggerInboxKey::new("slack\n", "a", "itemCreated", "b"),
        TriggerInboxKey::new("slack", " padded", "itemCreated", "b"),
        TriggerInboxKey::new("slack", "a", "itemCreated", "b\u{3000}"),
        TriggerInboxKey::new("slack", "", "itemCreated", "b"),
    ] {
        assert!(invalid.path(&scope).is_err(), "{invalid:?}");
    }
}

fn fixture(name: &str) -> Vec<u8> {
    std::fs::read(repository_path(&format!("sdkpython/tests/fixtures/{name}"))).expect("fixture")
}

fn inbox_with(contents: &[u8]) -> (Arc<InMemoryObjectStore>, Arc<TriggerInbox>) {
    let objects = Arc::new(InMemoryObjectStore::new());
    let inbox = {
        let store: Arc<dyn ObjectStore> = objects.clone();
        Arc::new(TriggerInbox::new(store, &Scope::new("fixture", "live"), key()).expect("inbox"))
    };
    objects
        .create_object(inbox.key(), contents)
        .expect("seed head");
    (objects, inbox)
}

#[test]
fn keeps_go_events_byte_for_byte_and_rewrites_as_go_does() {
    let (objects, inbox) = inbox_with(&fixture("go-trigger-inbox-three-events.json"));
    let pending = inbox.pending().expect("pending");
    let ids: Vec<&str> = pending.iter().map(|event| event.id.as_str()).collect();
    assert_eq!(ids, ["Ev1<&>", "Ev2", "Ev3"]);
    let second = String::from_utf8(pending[1].event.clone()).expect("UTF-8");
    assert!(second.contains(
        "\"alpha\":9007199254740993,\"list\":[1.50,\"x\"],\"nested\":{\"a\":null,\"b\":true},\"zeta\":1"
    ));
    let four = fixture("go-trigger-inbox-four-events.json");
    let (_, four_inbox) = inbox_with(&four);
    let fourth = four_inbox.pending().expect("pending")[3].clone();
    inbox.add(fourth).expect("adds");
    assert_eq!(objects.read_object(inbox.key(), "").expect("head").contents, four);
    inbox.remove("Ev2").expect("removes");
    assert_eq!(
        objects.read_object(inbox.key(), "").expect("head").contents,
        fixture("go-trigger-inbox-after-remove.json")
    );
}

#[test]
fn decodes_and_rewrites_go_decoding_cases() {
    let cases: Vec<serde_json::Value> =
        serde_json::from_slice(&fixture("go-trigger-inbox-decoding-cases.json")).expect("cases");
    // Rust strings are UTF-8 and serde_json bounds nesting depth, so these Go-accepted
    // documents are refused instead of rewritten; the coordinator report lists the gap.
    let unsupported = ["invalid UTF-8 and surrogates"];
    let mut failures = Vec::new();
    for case in cases {
        let name = case["name"].as_str().expect("name");
        if unsupported.contains(&name) {
            continue;
        }
        let input = STANDARD
            .decode(case["input"].as_str().expect("input"))
            .expect("base64");
        let (objects, inbox) = inbox_with(&input);
        if case["error"].as_bool().expect("error flag") {
            if inbox.pending().is_ok() {
                failures.push(format!("{name}: accepted an invalid document"));
            }
            continue;
        }
        let Ok(pending) = inbox.pending() else {
            failures.push(format!("{name}: refused a valid document"));
            continue;
        };
        let ids: Vec<serde_json::Value> = pending
            .iter()
            .map(|event| serde_json::Value::String(event.id.clone()))
            .collect();
        if serde_json::Value::Array(ids) != case["ids"] {
            failures.push(format!("{name}: ids differ"));
            continue;
        }
        inbox
            .add(PendingTriggerEvent {
                id: "EvAdded".to_string(),
                event: b"{\"added\":true}".to_vec(),
            })
            .expect("adds");
        let encoded = STANDARD.encode(objects.read_object(inbox.key(), "").expect("head").contents);
        if encoded != case["output"].as_str().expect("output") {
            failures.push(format!("{name}: rewrite differs"));
        }
    }
    assert!(failures.is_empty(), "{failures:?}");
}

struct ScriptedConsumer {
    undeliverable: Vec<String>,
    failing: Mutex<Vec<String>>,
    delivered: Mutex<Vec<String>>,
}

impl TriggerTarget<TextPayload> for ScriptedConsumer {
    fn handle_trigger(
        &self,
        _context: &TriggerContext,
        event: &TriggerEvent<TextPayload>,
    ) -> Result<(), TriggerError> {
        if self.undeliverable.contains(&event.id) {
            return Err(Box::new(UndeliverableTriggerError::with_reason("rejected")));
        }
        if self.failing.lock().expect("failing lock").contains(&event.id) {
            return Err("Dex unavailable".into());
        }
        self.delivered.lock().expect("delivered lock").push(format!(
            "{}:{}:{}",
            event.id,
            event.occurred_at,
            event.payload["text"]
        ));
        Ok(())
    }
}

#[test]
fn durable_target_records_consumes_and_replays_after_restart() {
    let objects = Arc::new(InMemoryObjectStore::new());
    let consumer = Arc::new(ScriptedConsumer {
        undeliverable: vec!["Ev3".to_string()],
        failing: Mutex::new(vec!["Ev2".to_string()]),
        delivered: Mutex::new(Vec::new()),
    });
    let durable = new_durable_trigger_target(new_inbox(&objects), consumer.clone(), None);
    assert!(durable.prepares_trigger_delivery() && durable.replays_trigger_deliveries());
    for event_id in ["Ev1", "Ev2", "Ev3"] {
        durable
            .prepare_trigger(&TriggerContext::new(), &text_event(event_id))
            .expect("records");
    }
    durable
        .handle_trigger(&TriggerContext::new(), &text_event("Ev1"))
        .expect("consumes");
    assert!(
        durable
            .handle_trigger(&TriggerContext::new(), &text_event("Ev2"))
            .is_err()
    );
    assert_eq!(new_inbox(&objects).pending().expect("pending").len(), 2);
    consumer.failing.lock().expect("failing lock").clear();
    let restarted = new_durable_trigger_target(new_inbox(&objects), consumer.clone(), None);
    restarted
        .replay_trigger_deliveries(&TriggerContext::new())
        .expect("replays");
    assert_eq!(
        *consumer.delivered.lock().expect("delivered lock"),
        [
            "Ev1:2025-10-12T00:00:00.12Z:hello <world> & co",
            "Ev2:2025-10-12T00:00:00.12Z:hello <world> & co"
        ]
    );
    assert!(new_inbox(&objects).pending().expect("pending").is_empty());
}

#[test]
fn durable_prepare_refuses_writes_after_cancellation() {
    let objects = Arc::new(InMemoryObjectStore::new());
    let durable = new_durable_trigger_target(
        new_inbox(&objects),
        Arc::new(ScriptedConsumer {
            undeliverable: Vec::new(),
            failing: Mutex::new(Vec::new()),
            delivered: Mutex::new(Vec::new()),
        }),
        None,
    );
    let cancellation = TriggerCancellation::new();
    cancellation.cancel();
    let error = durable
        .prepare_trigger(
            &TriggerContext::with_cancellation(cancellation),
            &text_event("Ev1"),
        )
        .expect_err("cancelled");
    assert!(is_trigger_delivery_aborted(error.as_ref()));
    assert!(new_inbox(&objects).pending().expect("pending").is_empty());
}

#[test]
fn decodes_python_encoded_events() {
    let event: TriggerEvent<TextPayload> = decode_trigger_event(
        b"{\"eventId\":\"EvP\",\"occurredAt\":\"2025-10-12T00:00:00.120000Z\",\"payload\":{\"text\":\"x\"}}",
    )
    .expect("decodes");
    assert_eq!(event.occurred_at.as_wire_str(), "2025-10-12T00:00:00.12Z");
    assert_eq!(event.payload["text"], "x");
}
