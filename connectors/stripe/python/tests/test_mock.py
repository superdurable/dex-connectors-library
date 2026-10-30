# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check all manifest scenarios and default mock scripts."""

from types import SimpleNamespace
from uuid import UUID

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, IdempotencyKey

from dex_connectors.stripe.stripemock import (
    Mock,
    create_ach_checkout_session_case,
    create_card_checkout_session_case,
    create_refund_case,
    get_checkout_session_case,
    get_payment_intent_case,
)

CALL_ID = CallID(str(UUID("ffdd13f6-fd46-4d55-ad11-7afce245c321")))
CONNECTION = ConnectionRef("stripe", "payments")


@pytest.mark.parametrize(
    "factory,scenario,branch,kind",
    [
        (create_card_checkout_session_case, "createdCardSession", "created", None),
        (
            create_card_checkout_session_case,
            "invalidCardRequest",
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
        ),
        (create_card_checkout_session_case, "unconfirmedCardSession", "uncertain", FailureKind.AVAILABILITY),
        (create_card_checkout_session_case, "malformedCardSession", "invalidResponse", FailureKind.PROTOCOL),
        (create_card_checkout_session_case, "invalidCardCurrency", "defect", FailureKind.VALIDATION),
        (create_ach_checkout_session_case, "createdSession", "created", None),
        (
            create_ach_checkout_session_case,
            "invalidRequest",
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
        ),
        (create_ach_checkout_session_case, "unconfirmedSession", "uncertain", FailureKind.AVAILABILITY),
        (create_ach_checkout_session_case, "malformedSession", "invalidResponse", FailureKind.PROTOCOL),
        (create_ach_checkout_session_case, "invalidCurrency", "defect", FailureKind.VALIDATION),
        (get_checkout_session_case, "paidSession", "found", None),
        (get_checkout_session_case, "missingSession", "notFound", FailureKind.NOT_FOUND),
        (get_checkout_session_case, "unauthorizedKey", "providerRejected", FailureKind.AUTHENTICATION),
        (get_checkout_session_case, "malformedSession", "invalidResponse", FailureKind.PROTOCOL),
        (get_checkout_session_case, "invalidSessionID", "defect", FailureKind.VALIDATION),
        (get_payment_intent_case, "partiallyRefundedPayment", "found", None),
        (get_payment_intent_case, "missingPaymentIntent", "notFound", FailureKind.NOT_FOUND),
        (get_payment_intent_case, "unauthorizedKey", "providerRejected", FailureKind.AUTHENTICATION),
        (get_payment_intent_case, "malformedPaymentIntent", "invalidResponse", FailureKind.PROTOCOL),
        (get_payment_intent_case, "invalidPaymentIntentID", "defect", FailureKind.VALIDATION),
        (create_refund_case, "succeededRefund", "refunded", None),
        (create_refund_case, "alreadyRefunded", "providerRejected", FailureKind.PROVIDER_REJECTION),
        (create_refund_case, "unconfirmedRefund", "uncertain", FailureKind.AVAILABILITY),
        (create_refund_case, "malformedRefund", "invalidResponse", FailureKind.PROTOCOL),
        (create_refund_case, "invalidAmount", "defect", FailureKind.VALIDATION),
    ],
)
def test_manifest_mock_scenario(
    factory: object, scenario: str, branch: str, kind: FailureKind | None
) -> None:
    """Every declared scenario carries its branch and Failure kind."""
    case = factory(scenario)
    assert case.branch == branch
    assert case.kind == ("uncertain" if branch == "uncertain" else "branch")
    assert (case.failure.kind if case.failure else None) == kind


async def test_default_mock_answers_all_five_operations() -> None:
    """Unscripted happy paths use their manifest defaults."""
    mock = Mock("payments")
    for operation in (
        mock.create_card_checkout_session,
        mock.create_ach_checkout_session,
        mock.get_checkout_session,
        mock.get_payment_intent,
        mock.create_refund,
    ):
        call = Call(
            CALL_ID,
            CONNECTION,
            operation.definition().operation,
            IdempotencyKey(CALL_ID),
            SimpleNamespace(flow_id="flow-1"),
        )
        attempt = await operation.invoke(call, None)
        assert attempt.kind == "branch"
        assert attempt.branch in ("created", "found", "refunded")
    mock.assert_no_failures()


async def test_mock_flow_scope_and_keyed_replay() -> None:
    """A Flow-specific refund scenario replays by stable Stripe call key."""
    mock = Mock("payments")
    mock.create_refund.for_flow("refund-flow-").respond(create_refund_case("alreadyRefunded"))
    operation = mock.create_refund
    call = Call(
        CALL_ID,
        CONNECTION,
        operation.definition().operation,
        IdempotencyKey(CALL_ID),
        SimpleNamespace(flow_id="refund-flow-123"),
    )
    first = await operation.invoke(call, None)
    second = await operation.invoke(call, None)
    assert first.branch == second.branch == "providerRejected"
    assert operation.calls()[0].flow_id == "refund-flow-123"
    assert operation.calls()[1].replay
    mock.assert_no_failures()
