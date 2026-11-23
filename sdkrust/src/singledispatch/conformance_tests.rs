// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Replays `conformance/single-dispatch-markers.json` through real Fences.

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::sync::Mutex;
use std::time::Duration;

use serde::Deserialize;
use serde_json::Value as JsonValue;
use serde_json::value::RawValue;

use super::markers::{CheckpointHeartbeat, Marker, markers_from_heartbeat};
use super::{DecisionKind, Fence, State};
use crate::step_attempt::{StepAttemptAccess, lock};
use crate::types::{Call, CallId, ConnectionRef, OperationRef, Progress};

struct FakeStepAttempt {
    attempt: u32,
    previous_heartbeat: Option<JsonValue>,
    heartbeats: Mutex<Vec<String>>,
    checkpoint_markers: Mutex<Option<BTreeMap<String, Marker>>>,
}

impl FakeStepAttempt {
    fn new(attempt: u32, previous_heartbeat: Option<JsonValue>) -> Self {
        Self {
            attempt,
            previous_heartbeat,
            heartbeats: Mutex::new(Vec::new()),
            checkpoint_markers: Mutex::new(None),
        }
    }
}

impl StepAttemptAccess for FakeStepAttempt {
    fn attempt_number(&self) -> u32 {
        self.attempt
    }

    fn attempt_flow_id(&self) -> &str {
        "flow-42"
    }

    fn has_progress_stream(&self) -> bool {
        false
    }

    fn has_text_stream(&self) -> bool {
        false
    }

    fn report_progress(&self, _call_id: &CallId, _progress: Progress) {}

    fn write_text(&self, _value: &str) {}

    fn begin_running_mutation(&self, _operation: &OperationRef) -> bool {
        true
    }

    fn end_running_mutation(&self, _operation: &OperationRef) {}

    fn checkpoint_markers(&self) -> &Mutex<Option<BTreeMap<String, Marker>>> {
        &self.checkpoint_markers
    }

    fn last_heartbeat_value(&self) -> Option<JsonValue> {
        self.previous_heartbeat.clone()
    }

    fn record_heartbeat_value(&self, value: CheckpointHeartbeat) -> Result<(), String> {
        let compact = serde_json::to_string(&value).map_err(|error| error.to_string())?;
        lock(&self.heartbeats).push(compact);
        Ok(())
    }
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ScenarioCall {
    call_id: String,
    max_sends: u32,
    dispatch: bool,
    decision: String,
    maximum_prior_sends: u32,
    #[serde(default)]
    decision_provider_id: String,
    #[serde(default)]
    provider_id: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Scenario {
    name: String,
    attempt: u32,
    #[serde(default)]
    previous_heartbeat: Option<JsonValue>,
    calls: Vec<ScenarioCall>,
    heartbeats: Vec<Box<RawValue>>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MarkersFixture {
    markers: Vec<Box<RawValue>>,
    checkpoint_scenarios: Vec<Scenario>,
}

fn load_fixture() -> MarkersFixture {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../conformance/single-dispatch-markers.json");
    let text = std::fs::read_to_string(path).expect("fixture is present");
    serde_json::from_str(&text).expect("fixture decodes")
}

/// Removes insignificant whitespace while keeping the fixture's key order.
fn compact_json(raw: &str) -> String {
    let mut output = String::with_capacity(raw.len());
    let mut is_in_string = false;
    let mut is_escaped = false;
    for character in raw.chars() {
        if is_in_string {
            output.push(character);
            if is_escaped {
                is_escaped = false;
            } else if character == '\\' {
                is_escaped = true;
            } else if character == '"' {
                is_in_string = false;
            }
        } else if character == '"' {
            is_in_string = true;
            output.push(character);
        } else if !character.is_whitespace() {
            output.push(character);
        }
    }
    output
}

fn call_for<'attempt>(attempt: &'attempt FakeStepAttempt, call_id: &str) -> Call<'attempt> {
    let mut call = Call::new(
        CallId::new(call_id),
        ConnectionRef::new("sample", "main"),
        OperationRef::new("sample", "createItem"),
    )
    .expect("valid call");
    call.step_attempt = Some(attempt);
    call
}

#[test]
fn markers_round_trip_with_go_omissions() {
    let fixture = load_fixture();
    assert_eq!(fixture.markers.len(), 8);
    for raw in &fixture.markers {
        let value: JsonValue = serde_json::from_str(raw.get()).expect("marker JSON");
        let marker = Marker::from_json(&value).expect("marker decodes");
        assert_eq!(
            serde_json::to_string(&marker).expect("marker encodes"),
            compact_json(raw.get())
        );
    }
}

#[test]
fn checkpoint_scenarios_replay_byte_for_byte() {
    let fixture = load_fixture();
    assert!(fixture.checkpoint_scenarios.len() >= 10);
    for scenario in &fixture.checkpoint_scenarios {
        let attempt = FakeStepAttempt::new(scenario.attempt, scenario.previous_heartbeat.clone());
        for scenario_call in &scenario.calls {
            let call = call_for(&attempt, &scenario_call.call_id);
            let fence = Fence::new(&call, Duration::from_secs(30), scenario_call.max_sends)
                .expect("fence constructs");
            let decision = fence.decide();
            assert_eq!(
                decision.kind.as_wire_str(),
                scenario_call.decision,
                "{}",
                scenario.name
            );
            assert_eq!(
                decision.prior_send_upper_bound, scenario_call.maximum_prior_sends,
                "{}",
                scenario.name
            );
            let decision_provider_id = decision
                .marker
                .as_ref()
                .map_or("", |marker| marker.provider_id.as_str());
            assert_eq!(
                decision_provider_id, scenario_call.decision_provider_id,
                "{}",
                scenario.name
            );
            if scenario_call.dispatch && decision.kind.permits_dispatch() {
                fence.dispatch(|_deadline| ()).expect("dispatch runs");
                if !scenario_call.provider_id.is_empty() {
                    fence
                        .record_provider_id(&scenario_call.provider_id)
                        .expect("provider ID recorded");
                }
            }
        }
        let expected: Vec<String> = scenario
            .heartbeats
            .iter()
            .map(|raw| compact_json(raw.get()))
            .collect();
        assert_eq!(*lock(&attempt.heartbeats), expected, "{}", scenario.name);
    }
}

#[test]
fn two_fenced_calls_in_one_step_both_send_and_survive_retry() {
    let gmail_call_id = "0b9d3c3e-6a51-5f0e-9a43-7d2f1f7c2b10";
    let slack_call_id = "5af8fafa-06ff-5b2d-bb13-04693809fabf";
    let attempt = FakeStepAttempt::new(1, None);
    for call_id in [gmail_call_id, slack_call_id] {
        let call = call_for(&attempt, call_id);
        let fence = Fence::new(&call, Duration::from_secs(30), 1).expect("fence");
        assert_eq!(fence.decide().kind, DecisionKind::SendAllowed);
        fence.dispatch(|_deadline| ()).expect("dispatch");
    }
    let last: JsonValue =
        serde_json::from_str(lock(&attempt.heartbeats).last().expect("heartbeat")).expect("JSON");
    let markers = markers_from_heartbeat(&last);
    assert_eq!(markers.len(), 2);
    assert!(
        markers
            .values()
            .all(|marker| marker.state == State::Claimed && marker.send_count == 1)
    );

    let retry = FakeStepAttempt::new(2, Some(last));
    let slack = call_for(&retry, slack_call_id);
    let slack_fence = Fence::new(&slack, Duration::from_secs(30), 1).expect("fence");
    assert_eq!(slack_fence.decide().kind, DecisionKind::ReadBackRequired);
    let gmail = call_for(&retry, gmail_call_id);
    let gmail_fence = Fence::new(&gmail, Duration::from_secs(30), 1).expect("fence");
    assert_eq!(gmail_fence.decide().kind, DecisionKind::ReadBackRequired);
    assert!(gmail_fence.dispatch(|_deadline| ()).is_err());
}

#[test]
fn short_write_window_records_not_sent_without_sending() {
    let attempt = FakeStepAttempt::new(1, None);
    let call = call_for(&attempt, "5af8fafa-06ff-5b2d-bb13-04693809fabf");
    let fence =
        Fence::with_write_window_fraction(&call, Duration::from_nanos(5), 1, 0.01).expect("fence");
    assert_eq!(fence.decide().kind, DecisionKind::SendAllowed);
    let mut has_sent = false;
    let result = fence.dispatch(|_deadline| has_sent = true);
    assert!(matches!(
        result,
        Err(super::FenceError::InsufficientAttemptTime {
            checkpoint_error: None
        })
    ));
    assert!(!has_sent);
    assert_eq!(lock(&attempt.heartbeats).len(), 1);
    assert!(lock(&attempt.heartbeats)[0].contains("\"state\":\"notSent\""));
}
