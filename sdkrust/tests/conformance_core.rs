// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Cross-language identity, result, failure, retry, and definition fixtures.

use std::path::PathBuf;

use dex_connectors_sdk::{
    Call, CallId, CallIdentity, ConnectionRef, Failure, FailureKind, MutationDefinition,
    MutationResult, OperationRef, QueryDefinition, QueryResult, Receipt, Rfc3339NanoTimestamp,
    RetryPolicy, StepDefaults, call_id_namespace, default_connector_step_execute_retry_policy,
    derive_call_id,
};
use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::Value;

fn fixture(name: &str) -> Value {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("conformance")
        .join(name);
    let text = std::fs::read_to_string(&path).expect("shared conformance fixture is present");
    serde_json::from_str(&text).expect("fixture is JSON")
}

/// JSON value equality that compares numbers numerically, as Python and TypeScript do.
fn assert_json_value_eq(actual: &Value, expected: &Value) {
    fn equal(actual: &Value, expected: &Value) -> bool {
        match (actual, expected) {
            (Value::Number(left), Value::Number(right)) => left.as_f64() == right.as_f64(),
            (Value::Array(left), Value::Array(right)) => {
                left.len() == right.len() && left.iter().zip(right).all(|(a, b)| equal(a, b))
            }
            (Value::Object(left), Value::Object(right)) => {
                left.len() == right.len()
                    && left
                        .iter()
                        .all(|(key, value)| right.get(key).is_some_and(|other| equal(value, other)))
            }
            _ => actual == expected,
        }
    }
    assert!(equal(actual, expected), "actual {actual}\nexpected {expected}");
}

fn round_trip<T: Serialize + DeserializeOwned>(value: &Value) -> Value {
    let decoded: T = serde_json::from_value(value.clone()).expect("fixture decodes");
    serde_json::to_value(decoded).expect("value encodes")
}

#[test]
fn identity_fixture_matches_go_derivation() {
    let identity = fixture("identity.json");
    let source = &identity["source"];
    let text = |value: &Value| value.as_str().expect("string").to_string();
    let operation = OperationRef::new(text(&source["connectorId"]), text(&source["operationId"]));
    let flow_id = text(&source["flowId"]);
    let step_execution_id = text(&source["stepExecutionId"]);
    assert_eq!(
        call_id_namespace().to_string(),
        text(&identity["callIDNamespace"])
    );
    let derived = derive_call_id(&flow_id, &step_execution_id, &operation, 1).expect("derives");
    assert_eq!(derived.as_str(), identity["derivedCall"]["id"]);
    derived.validate().expect("UUID");
    let decoded: CallIdentity =
        serde_json::from_value(identity["derivedCall"].clone()).expect("call decodes");
    assert_json_value_eq(
        &serde_json::to_value(&decoded).expect("call encodes"),
        &identity["derivedCall"],
    );
    let different_step = derive_call_id(&flow_id, "step-execution-8", &operation, 1).expect("derives");
    assert_eq!(different_step.as_str(), identity["differentStepCallId"]);
    let different_operation = derive_call_id(
        &flow_id,
        &step_execution_id,
        &OperationRef::new("sample", "getItem"),
        1,
    )
    .expect("derives");
    assert_eq!(
        different_operation.as_str(),
        identity["differentOperationCallId"]
    );
    assert_eq!(identity["providerKey"], format!("sample/{derived}"));
    let occurrences: Vec<String> = (1..=3)
        .map(|occurrence| {
            derive_call_id(&flow_id, &step_execution_id, &operation, occurrence)
                .expect("derives")
                .to_string()
        })
        .collect();
    assert_eq!(
        serde_json::to_value(&occurrences).expect("encodes"),
        identity["occurrenceCallIds"]
    );
    assert!(derive_call_id(&flow_id, &step_execution_id, &operation, 0).is_err());
    let unicode = &identity["unicodeSource"];
    let unicode_derived = derive_call_id(
        &text(&unicode["flowId"]),
        &text(&unicode["stepExecutionId"]),
        &OperationRef::new(text(&unicode["connectorId"]), text(&unicode["operationId"])),
        1,
    )
    .expect("derives");
    assert_eq!(unicode_derived.as_str(), identity["unicodeDerivedCallId"]);
    assert_json_value_eq(
        &round_trip::<Receipt>(&identity["fallbackCompletedReceipt"]),
        &identity["fallbackCompletedReceipt"],
    );
    let call = Call::new(
        derived,
        ConnectionRef::new("sample", "main"),
        operation,
    )
    .expect("valid call");
    assert!(!call.is_in_step());
    assert_eq!(call.attempt(), None);
}

#[test]
fn results_fixture_round_trips() {
    let results = fixture("results.json");
    for (name, value) in results.as_object().expect("object") {
        let encoded = if name.ends_with("Receipt") {
            round_trip::<Receipt>(value)
        } else if name.starts_with("mutation") {
            round_trip::<MutationResult<Option<String>>>(value)
        } else {
            round_trip::<QueryResult<Option<String>>>(value)
        };
        assert_json_value_eq(&encoded, value);
    }
    let mut nanosecond = results["queryHappy"].clone();
    nanosecond["receipt"]["observedAt"] = Value::from("2026-10-29T12:30:45.123456789Z");
    assert_json_value_eq(
        &round_trip::<QueryResult<Option<String>>>(&nanosecond),
        &nanosecond,
    );
    let zero = serde_json::to_value(Receipt::default()).expect("encodes");
    assert_eq!(
        zero,
        serde_json::json!({"callId": "", "provider": "", "observedAt": "0001-01-01T00:00:00Z"})
    );
    let receipt: Receipt =
        serde_json::from_value(results["nanosecondOffsetReceipt"].clone()).expect("decodes");
    assert_eq!(
        receipt.observed_at.as_wire_str(),
        "2026-10-29T12:30:45.123456789+08:00"
    );
}

#[test]
fn results_validate_routes() {
    let receipt = Receipt::default();
    let uncertain_without_failure = MutationResult {
        branch: "uncertain".to_string(),
        value: (),
        receipt: receipt.clone(),
        failure: None,
    };
    assert!(uncertain_without_failure.validate().is_err());
    let uncertain_query = QueryResult {
        branch: "uncertain".to_string(),
        value: (),
        receipt,
        failure: None,
    };
    assert!(uncertain_query.validate().is_err());
}

#[test]
fn failures_and_retry_policy_fixtures_round_trip() {
    let failures = fixture("failures.json");
    let failures = failures.as_array().expect("array");
    assert_eq!(failures.len(), FailureKind::ALL.len());
    for (value, kind) in failures.iter().zip(FailureKind::ALL) {
        let decoded: Failure = serde_json::from_value(value.clone()).expect("failure decodes");
        assert_eq!(decoded.kind, kind);
        decoded.validate().expect("valid failure");
        assert_json_value_eq(&serde_json::to_value(&decoded).expect("encodes"), value);
    }
    let mut unknown = failures[0].clone();
    unknown["extra"] = Value::from("x");
    assert!(serde_json::from_value::<Failure>(unknown).is_err());

    let policy = default_connector_step_execute_retry_policy();
    let expected = fixture("retry-policy.json");
    let encoded = dex_connectors_sdk::go_json::to_go_json_string(&policy).expect("encodes");
    assert_eq!(
        encoded,
        "{\"InitialInterval\":1000000000,\"BackoffCoefficient\":2,\"MaximumInterval\":300000000000,\"MaximumAttempts\":0,\"TotalDuration\":3600000000000}",
        "Go field order and the integral coefficient"
    );
    assert_json_value_eq(&serde_json::to_value(policy).expect("encodes"), &expected);
    let mut nanosecond = expected.clone();
    nanosecond["InitialInterval"] = Value::from(1_000_000_001_u64);
    assert_json_value_eq(&round_trip::<RetryPolicy>(&nanosecond), &nanosecond);
    let defaults = serde_json::json!({
        "executeMethodTimeout": 15_000_000_001_u64,
        "heartbeatTimeout": 1,
        "executeDurability": 2,
    });
    assert_json_value_eq(&round_trip::<StepDefaults>(&defaults), &defaults);
}

#[test]
fn operation_definitions_round_trip() {
    let fixture = fixture("connectormock.json");
    let query: QueryDefinition =
        serde_json::from_value(fixture["queryDefinition"].clone()).expect("decodes");
    query.validate().expect("valid query");
    assert_json_value_eq(
        &serde_json::to_value(&query).expect("encodes"),
        &fixture["queryDefinition"],
    );
    let mutation: MutationDefinition =
        serde_json::from_value(fixture["mutationDefinition"].clone()).expect("decodes");
    mutation.validate().expect("valid mutation");
    assert_json_value_eq(
        &serde_json::to_value(&mutation).expect("encodes"),
        &fixture["mutationDefinition"],
    );
    for name in ["emptyBranches", "nullBranches"] {
        assert_json_value_eq(&round_trip::<QueryDefinition>(&fixture[name]), &fixture[name]);
    }
}

#[test]
fn go_time_json_input_acceptance() {
    let cases = [
        ("2026-02-03T04:05:06Z", true),
        ("2026-02-03T04:05:06,123456789Z", true),
        ("2026-02-03T04:05:06.123456789123Z", true),
        ("2026-02-03T04:05:06+24:00", true),
        ("2026-02-03T04:05:06+23:60", true),
        ("2026-02-03T04:05:06+24:60", true),
        ("2026-02-03T4:05:06Z", true),
        ("0000-02-29T00:00:00Z", true),
        ("2026-02-03t04:05:06Z", false),
        ("2026-02-03T04:05:06z", false),
        ("2026-02-03T04:05:06+25:00", false),
        ("2026-02-03T04:05:06+24:61", false),
        ("2026-02-03T04:05:60Z", false),
        ("2026-02-30T04:05:06Z", false),
        ("2026-02-03T04:05:06.Z", false),
        ("２０２６-02-03T04:05:06Z", false),
    ];
    for (wire, is_accepted) in cases {
        assert_eq!(
            Rfc3339NanoTimestamp::from_wire(wire).is_ok(),
            is_accepted,
            "{wire}"
        );
    }
    assert!(CallId::new("not-a-uuid").validate().is_err());
}
