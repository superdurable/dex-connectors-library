# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep hand-written Stripe Python definitions and scenarios aligned with the manifest."""

from dataclasses import MISSING, fields, is_dataclass
from pathlib import Path
from typing import Any, cast
from uuid import UUID

import pytest
import yaml
from dex_connectors.sdk import CallID, IdempotencyKey
from dex_connectors.sdk.types import MutationDefinition, QueryDefinition

from dex_connectors.stripe import (
    CREATE_ACH_CHECKOUT_SESSION_DEFINITION,
    CREATE_CARD_CHECKOUT_SESSION_DEFINITION,
    CREATE_REFUND_DEFINITION,
    GET_CHECKOUT_SESSION_DEFINITION,
    GET_PAYMENT_INTENT_DEFINITION,
)
from dex_connectors.stripe.stripemock import (
    Mock,
    create_ach_checkout_session_case,
    create_card_checkout_session_case,
    create_refund_case,
    get_checkout_session_case,
    get_payment_intent_case,
)

MANIFEST = yaml.safe_load((Path(__file__).resolve().parents[2] / "connector.yaml").read_text())
OPERATIONS = {operation["name"]: operation for operation in MANIFEST["spec"]["operations"]}
DEFINITIONS: dict[str, QueryDefinition | MutationDefinition] = {
    "createCardCheckoutSession": CREATE_CARD_CHECKOUT_SESSION_DEFINITION,
    "createACHCheckoutSession": CREATE_ACH_CHECKOUT_SESSION_DEFINITION,
    "getCheckoutSession": GET_CHECKOUT_SESSION_DEFINITION,
    "getPaymentIntent": GET_PAYMENT_INTENT_DEFINITION,
    "createRefund": CREATE_REFUND_DEFINITION,
}
MOCK_CASES = {
    "createCardCheckoutSession": create_card_checkout_session_case,
    "createACHCheckoutSession": create_ach_checkout_session_case,
    "getCheckoutSession": get_checkout_session_case,
    "getPaymentIntent": get_payment_intent_case,
    "createRefund": create_refund_case,
}
DEFAULT_SCENARIOS = {
    "createCardCheckoutSession": "createdCardSession",
    "createACHCheckoutSession": "createdSession",
    "getCheckoutSession": "paidSession",
    "getPaymentIntent": "partiallyRefundedPayment",
    "createRefund": "succeededRefund",
}


def _model_wire(value: object) -> dict[str, object]:
    if not is_dataclass(value):
        raise AssertionError("mock value must be a dataclass")
    encoded: dict[str, object] = {}
    for model_field in fields(value):
        field_value = getattr(value, model_field.name)
        if model_field.metadata.get("dex_json_omit_default"):
            default = (
                model_field.default_factory()
                if model_field.default_factory is not MISSING
                else model_field.default
            )
            if field_value == default:
                continue
        encoded[model_field.metadata.get("dex_json_name", model_field.name)] = field_value
    return encoded


def test_all_manifest_operations_match_python_definitions() -> None:
    """Names, kind, auth, idempotency, branches, and defaults cannot drift."""
    assert set(OPERATIONS) == set(DEFINITIONS) == set(MOCK_CASES)
    call_id = CallID(str(UUID("ffdd13f6-fd46-4d55-ad11-7afce245c321")))
    mock = Mock("stripe-payments")
    operation_instances: dict[str, Any] = {
        "createCardCheckoutSession": mock.create_card_checkout_session,
        "createACHCheckoutSession": mock.create_ach_checkout_session,
        "getCheckoutSession": mock.get_checkout_session,
        "getPaymentIntent": mock.get_payment_intent,
        "createRefund": mock.create_refund,
    }
    for name, manifest_operation in OPERATIONS.items():
        definition = DEFINITIONS[name]
        assert definition.operation.connector_id == MANIFEST["metadata"]["name"]
        assert definition.operation.operation_id == name
        assert operation_instances[name].definition() == definition
        is_mutation = manifest_operation["kind"] == "mutation"
        assert manifest_operation["kind"] in ("query", "mutation")
        assert isinstance(definition, MutationDefinition) == is_mutation
        assert manifest_operation["idempotency"] == ("required" if is_mutation else "none")
        assert manifest_operation.get("authorization", "required") == "required"
        if is_mutation:
            assert operation_instances[name].idempotency_key(call_id, None) == IdempotencyKey(call_id)
        assert definition.branches is not None
        assert [(branch.id, branch.description, branch.optional) for branch in definition.branches] == [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in manifest_operation["branches"]
        ]
        execution = manifest_operation["execution"]
        assert execution["executeMethodTimeout"] == "30s"
        assert execution["durability"] == "async"
        assert "retry" not in execution
        assert definition.step_defaults.execute_method_timeout.total_seconds() == 30
        assert (
            definition.step_defaults.execute_durability
            == {
                "default": 0,
                "sync": 1,
                "async": 2,
            }[execution["durability"]]
        )
        assert definition.step_defaults.execute_retry is None
        assert definition.step_defaults.heartbeat_timeout.total_seconds() == 0


@pytest.mark.parametrize("name", sorted(OPERATIONS))
def test_manifest_mock_scenarios_match_hand_written_cases(name: str) -> None:
    """Every named mock case, output, Failure, and default matches connector.yaml."""
    manifest_operation = OPERATIONS[name]
    scenarios = manifest_operation["mocks"]
    assert len(scenarios) == 5
    assert [item["name"] for item in scenarios].count(DEFAULT_SCENARIOS[name]) == 1
    assert [item["name"] for item in scenarios if item.get("default")] == [DEFAULT_SCENARIOS[name]]
    for scenario in scenarios:
        case = cast(Any, MOCK_CASES[name])(scenario["name"])
        uncertain = scenario.get("uncertain")
        assert case.kind == ("uncertain" if uncertain else "branch")
        assert case.branch == (scenario.get("branch") or "uncertain")
        if "output" in scenario:
            assert _model_wire(case.value) == scenario["output"]
        else:
            assert case.value == type(case.value)()
        manifest_failure = uncertain["failure"] if uncertain else scenario.get("failure")
        if manifest_failure is None:
            assert case.failure is None
        else:
            assert case.failure.kind.value == manifest_failure["kind"]
            assert case.failure.message == manifest_failure["message"]
            assert case.failure.provider == "stripe" and case.failure.operation == name
