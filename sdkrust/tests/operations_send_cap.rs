// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! `run_mutation` applies the definition's send cap and validates overrides.

use std::sync::Mutex;

use dex_connectors_sdk::{
    BranchDefinition, Call, CallId, ConnectionRef, IdempotencyKey, Mutation, MutationAttempt,
    MutationDefinition, OperationOutcome, OperationRef, Receipt, RepeatOnUnknown, StepDefaults,
    derive_call_id, run_mutation,
};

struct CapturingMutation {
    repeat_on_unknown: Option<RepeatOnUnknown>,
    max_sends: u32,
    observed_caps: Mutex<Vec<u32>>,
}

impl CapturingMutation {
    fn new(repeat_on_unknown: Option<RepeatOnUnknown>, max_sends: u32) -> Self {
        Self {
            repeat_on_unknown,
            max_sends,
            observed_caps: Mutex::new(Vec::new()),
        }
    }
}

impl Mutation for CapturingMutation {
    type Input = ();
    type Output = String;

    fn definition(&self) -> MutationDefinition {
        MutationDefinition {
            operation: OperationRef::new("sample", "createItem"),
            branches: Some(vec![
                BranchDefinition::new("created", "Item created", false),
                BranchDefinition::new("defect", "Local defect", true),
            ]),
            step_defaults: StepDefaults::default(),
            repeat_on_unknown: self.repeat_on_unknown,
            max_sends: self.max_sends,
        }
    }

    fn idempotency_key(&self, _call_id: &CallId, _input: &()) -> IdempotencyKey {
        IdempotencyKey::default()
    }

    fn invoke(&self, call: &Call<'_>, _input: &()) -> MutationAttempt<String> {
        self.observed_caps
            .lock()
            .expect("cap lock")
            .push(call.max_sends());
        MutationAttempt::branch("created", "object-1".to_string(), Receipt::default(), None)
    }
}

fn call() -> Call<'static> {
    let operation = OperationRef::new("sample", "createItem");
    let id = derive_call_id("flow-1", "step-1", &operation, 1).expect("Call ID");
    Call::new(id, ConnectionRef::new("sample", "main"), operation).expect("valid Call")
}

fn branch_of(
    outcome: OperationOutcome<dex_connectors_sdk::MutationResult<Option<String>>>,
) -> String {
    match outcome {
        OperationOutcome::Completed(result) => result.branch,
        OperationOutcome::Retry(retry) => panic!("unexpected retry: {retry}"),
    }
}

#[test]
fn direct_limited_mutation_receives_the_manifest_cap() {
    let operation = CapturingMutation::new(Some(RepeatOnUnknown::Limited), 3);
    assert_eq!(branch_of(run_mutation(&operation, &call(), &())), "created");
    assert_eq!(
        branch_of(run_mutation(
            &operation,
            &call().with_max_sends_override(5),
            &()
        )),
        "created"
    );
    assert_eq!(
        *operation.observed_caps.lock().expect("cap lock"),
        vec![3, 5]
    );
}

#[test]
fn other_tiers_get_one_send_and_reject_overrides_before_invoking() {
    for tier in [
        Some(RepeatOnUnknown::Unsafe),
        Some(RepeatOnUnknown::Safe),
        None,
    ] {
        let operation = CapturingMutation::new(tier, 0);
        assert_eq!(branch_of(run_mutation(&operation, &call(), &())), "created");
        assert_eq!(
            branch_of(run_mutation(
                &operation,
                &call().with_max_sends_override(3),
                &()
            )),
            "defect"
        );
        assert_eq!(*operation.observed_caps.lock().expect("cap lock"), vec![1]);
    }
    let limited = CapturingMutation::new(Some(RepeatOnUnknown::Limited), 3);
    assert_eq!(
        branch_of(run_mutation(
            &limited,
            &call().with_max_sends_override(11),
            &()
        )),
        "defect"
    );
    assert!(limited.observed_caps.lock().expect("cap lock").is_empty());
}
