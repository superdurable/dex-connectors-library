# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation parity checks against the Go connector's HTTP behavior."""

from dataclasses import replace
from datetime import UTC, datetime
from typing import Any
from uuid import UUID

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
    run_query,
)

from dex_connectors.stripe import (
    Client,
    Config,
    CreateACHCheckoutSessionInput,
    CreateCardCheckoutSessionInput,
    CreateRefundInput,
    Credentials,
    GetCheckoutSessionInput,
    GetPaymentIntentInput,
)
from dex_connectors.stripe.client import retry_after_seconds

CALL_ID = CallID(str(UUID("ffdd13f6-fd46-4d55-ad11-7afce245c321")))
CONNECTION = ConnectionRef("stripe", "payments")
SESSION_BODY = {"id": "cs_test_123", "object": "checkout.session", "status": "open", "currency": "usd"}
PAYMENT_BODY = {
    "id": "pi_test_123",
    "object": "payment_intent",
    "status": "succeeded",
    "currency": "usd",
    "amount": 7500,
    "latest_charge": {
        "id": "ch_123",
        "amount_refunded": 2500,
        "billing_details": {"email": "person@example.com"},
    },
}
REFUND_BODY = {
    "id": "re_test_123",
    "object": "refund",
    "status": "pending",
    "currency": "usd",
    "amount": 2500,
}


def checkout_input(card: bool = False) -> CreateACHCheckoutSessionInput | CreateCardCheckoutSessionInput:
    """Return one provider-valid hosted Checkout request."""
    input_type = CreateCardCheckoutSessionInput if card else CreateACHCheckoutSessionInput
    return input_type(
        "registration-123",
        "attendee@example.com",
        "https://events.example.com/success",
        "https://events.example.com/cancel",
        "usd",
        7500,
        "Community Event",
        {"registration_id": "registration-123"},
    )


def make_call(operation: str, mutation: bool = False) -> Call:
    """Use the same stable call and key across retry attempts."""
    return Call(
        CALL_ID, CONNECTION, OperationRef("stripe", operation), IdempotencyKey(CALL_ID if mutation else "")
    )


def client_for(handler: httpx.MockTransport, max_bytes: int = 1024) -> Client:
    """Construct a test client with credential-safe HTTP interception."""
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString("sk_test_example"))})
    return Client(
        Config("http://127.0.0.1:12345/v1", max_bytes),
        credentials,
        httpx.AsyncClient(transport=handler),
        lambda: datetime(2026, 10, 30, tzinfo=UTC),
    )


@pytest.mark.parametrize(
    "operation,card", [("createCardCheckoutSession", True), ("createACHCheckoutSession", False)]
)
async def test_checkout_creation_request_and_receipt(operation: str, card: bool) -> None:
    """Both Checkout writes preserve key, form, authorization, and receipt."""
    captured: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        captured.append(request)
        return httpx.Response(200, json=SESSION_BODY, headers={"Request-Id": "req_123"})

    client = client_for(httpx.MockTransport(answer))
    operation_value = client.create_card_checkout_session() if card else client.create_ach_checkout_session()
    result = await operation_value.invoke(make_call(operation, True), checkout_input(card))
    assert result.value is not None and result.receipt is not None
    assert result.kind == "branch" and result.branch == "created" and result.value.id == "cs_test_123"
    assert result.receipt.provider_request_id == "req_123"
    assert str(result.receipt.idempotency_key) == str(CALL_ID)
    request = captured[0]
    assert request.method == "POST" and request.url.path == "/v1/checkout/sessions"
    assert request.headers["Idempotency-Key"] == CALL_ID
    assert request.headers["Authorization"].startswith("Basic ")
    form = dict(httpx.QueryParams(request.content))
    assert form["mode"] == "payment"
    assert form["payment_method_types[0]"] == ("card" if card else "us_bank_account")
    assert form["metadata[registration_id]"] == "registration-123"
    assert form["payment_intent_data[metadata][registration_id]"] == "registration-123"


async def test_checkout_query_found_and_payment_intent_query_found() -> None:
    """Queries decode safe fields and expand only the latest charge."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("cs_test_123"):
            return httpx.Response(200, json={**SESSION_BODY, "payment_intent": {"id": "pi_test_123"}})
        return httpx.Response(200, json=PAYMENT_BODY)

    client = client_for(httpx.MockTransport(answer))
    checkout = await client.get_checkout_session().invoke(
        make_call("getCheckoutSession"), GetCheckoutSessionInput("cs_test_123")
    )
    payment = await client.get_payment_intent().invoke(
        make_call("getPaymentIntent"), GetPaymentIntentInput("pi_test_123")
    )
    assert checkout.value is not None and payment.value is not None
    assert checkout.branch == "found" and checkout.value.payment_intent_id == "pi_test_123"
    assert payment.branch == "found" and payment.value.amount_refunded == 2500
    assert payment.value.customer_email == "person@example.com"
    assert requests[1].url.params["expand[]"] == "latest_charge"
    assert "Idempotency-Key" not in requests[1].headers


@pytest.mark.parametrize(
    "operation,identifier,expected_path",
    [
        ("getCheckoutSession", "cs_test_%2f", b"/v1/checkout/sessions/cs_test_%252f"),
        ("getPaymentIntent", "pi_test_%2f", b"/v1/payment_intents/pi_test_%252f"),
        ("getCheckoutSession", "cs_a\x00b", b"/v1/checkout/sessions/cs_a%00b"),
        ("getPaymentIntent", "pi_a\nb", b"/v1/payment_intents/pi_a%0Ab"),
    ],
)
async def test_query_ids_are_escaped_as_single_path_segments(
    operation: str, identifier: str, expected_path: bytes
) -> None:
    """Literal percent sequences and control bytes never change the endpoint."""
    result: Any
    captured: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        captured.append(request)
        return httpx.Response(404)

    client = client_for(httpx.MockTransport(answer))
    if operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput(identifier)
        )
    else:
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput(identifier)
        )
    assert result.branch == "notFound"
    assert captured[0].url.raw_path.split(b"?", 1)[0] == expected_path


@pytest.mark.parametrize("customer_email", ["@x", "foo@", "a@b@c"])
async def test_checkout_rejects_invalid_plain_email(customer_email: str) -> None:
    """Malformed addresses select validation defect before sending a write."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    client = client_for(httpx.MockTransport(answer))
    input_value = replace(checkout_input(card=True), customer_email=customer_email)
    result = await client.create_card_checkout_session().invoke(
        make_call("createCardCheckoutSession", True), input_value
    )
    assert result.branch == "defect"
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert result.receipt is not None
    assert result.receipt.provider == "" and result.receipt.call_id == ""
    assert not requests


async def test_checkout_rejects_naive_expiration() -> None:
    """A local timezone cannot silently change the Stripe expiration."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    client = client_for(httpx.MockTransport(answer))
    input_value = replace(checkout_input(card=True), expires_at=datetime(2026, 10, 30, 12))
    result = await client.create_card_checkout_session().invoke(
        make_call("createCardCheckoutSession", True), input_value
    )
    assert result.branch == "defect"
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert not requests


@pytest.mark.parametrize("value,expected", [("+5", 5), ("7200", 3600), ("9223372036854775808", 0), ("-5", 0)])
def test_retry_after_matches_go_int64_parsing(value: str, expected: float) -> None:
    """Signed hints and int64 overflow have the same delay as the Go client."""
    assert retry_after_seconds(httpx.Headers({"Retry-After": value})) == expected


async def test_refund_success_request_and_reserved_marker() -> None:
    """Refund sends the Call ID as Stripe key and read-back metadata."""
    captured: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        captured.append(request)
        return httpx.Response(200, json=REFUND_BODY)

    client = client_for(httpx.MockTransport(answer))
    result = await client.create_refund().invoke(
        make_call("createRefund", True), CreateRefundInput("pi_test_123", 2500)
    )
    assert result.value is not None
    assert result.branch == "refunded" and result.value.refund_id == "re_test_123"
    assert result.value.status == "pending"
    form = dict(httpx.QueryParams(captured[0].content))
    assert captured[0].headers["Idempotency-Key"] == CALL_ID
    assert form["metadata[dex_call_id]"] == CALL_ID
    assert form["payment_intent"] == "pi_test_123"


@pytest.mark.parametrize(
    "operation,body",
    [
        ("createCardCheckoutSession", {**SESSION_BODY, "amount_total": "7500"}),
        ("createACHCheckoutSession", {**SESSION_BODY, "payment_intent": 123}),
        ("getCheckoutSession", {**SESSION_BODY, "metadata": {"quantity": 5}}),
        ("getPaymentIntent", {**PAYMENT_BODY, "latest_charge": 123}),
        ("createRefund", {**REFUND_BODY, "amount": "2500"}),
    ],
)
async def test_malformed_provider_field_is_invalid_response(operation: str, body: dict[str, object]) -> None:
    """Wrong provider field types select invalidResponse, matching Go JSON decoding."""
    result: Any
    client = client_for(httpx.MockTransport(lambda request: httpx.Response(200, json=body)))
    if operation == "createCardCheckoutSession":
        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), checkout_input(True)
        )
    elif operation == "createACHCheckoutSession":
        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), checkout_input()
        )
    elif operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("cs_test_123")
        )
    elif operation == "getPaymentIntent":
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("pi_test_123")
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 2500)
        )
    assert result.branch == "invalidResponse"
    assert result.failure.kind == FailureKind.PROTOCOL


async def test_checkout_redirect_does_not_forward_credentials() -> None:
    """A provider redirect is terminal and never sends the key to its target."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(307, headers={"Location": "https://other.example/path"})

    client = client_for(httpx.MockTransport(answer))
    result = await client.create_ach_checkout_session().invoke(
        make_call("createACHCheckoutSession", True), checkout_input()
    )
    assert result.branch == "providerRejected"
    assert len(requests) == 1


@pytest.mark.parametrize("operation", ["getCheckoutSession", "getPaymentIntent"])
async def test_missing_query_resource_is_not_found(operation: str) -> None:
    """Both read operations preserve the declared notFound branch."""
    result: Any
    client = client_for(
        httpx.MockTransport(
            lambda request: httpx.Response(
                404, json={"error": {"code": "resource_missing", "message": "No such resource"}}
            )
        )
    )
    if operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("cs_test_123")
        )
        assert result.failure.message == "Stripe Checkout Session was not found"
    else:
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("pi_test_123")
        )
        assert "resource_missing" in result.failure.message
    assert result.branch == "notFound" and result.failure.kind == FailureKind.NOT_FOUND


@pytest.mark.parametrize(
    "operation",
    ["createCardCheckoutSession", "createACHCheckoutSession", "createRefund"],
)
@pytest.mark.parametrize(
    "status,body,kind",
    [
        (409, {}, "retry"),
        (409, {"error": {"type": "idempotency_error"}}, "providerRejected"),
        (500, {}, "uncertain"),
    ],
)
async def test_keyed_mutation_conflict_and_uncertainty(
    operation: str, status: int, body: dict[str, object], kind: str
) -> None:
    """All keyed writes share 409 and unhinted 500 behavior."""
    result: Any

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json={"object": "list", "data": [], "has_more": False})
        return httpx.Response(status, json=body)

    client = client_for(httpx.MockTransport(answer))
    if operation == "createCardCheckoutSession":
        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), checkout_input(True)
        )
    elif operation == "createACHCheckoutSession":
        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), checkout_input()
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 2500)
        )
    assert (result.kind if kind in ("retry", "uncertain") else result.branch) == kind


@pytest.mark.parametrize(
    "operation",
    [
        "createCardCheckoutSession",
        "createACHCheckoutSession",
        "getCheckoutSession",
        "getPaymentIntent",
        "createRefund",
    ],
)
@pytest.mark.parametrize("status,expected", [(429, "retry"), (503, "retry"), (401, "providerRejected")])
async def test_oversized_error_keeps_status_classification(
    operation: str, status: int, expected: str
) -> None:
    """Truncated error bodies cannot turn status-based failures into protocol failures."""
    result: Any

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET" and request.url.path.endswith("refunds"):
            return httpx.Response(200, json={"object": "list", "data": [], "has_more": False})
        return httpx.Response(status, content=b"x" * 65)

    client = client_for(httpx.MockTransport(answer), 64)
    if operation == "createCardCheckoutSession":
        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), checkout_input(True)
        )
    elif operation == "createACHCheckoutSession":
        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), checkout_input()
        )
    elif operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("cs_test_123")
        )
    elif operation == "getPaymentIntent":
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("pi_test_123")
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 2500)
        )
    assert (result.kind if expected == "retry" else result.branch) == expected


@pytest.mark.parametrize(
    "operation",
    [
        "createCardCheckoutSession",
        "createACHCheckoutSession",
        "getCheckoutSession",
        "getPaymentIntent",
        "createRefund",
    ],
)
@pytest.mark.parametrize(
    "status,branch,kind",
    [
        (401, "providerRejected", FailureKind.AUTHENTICATION),
        (403, "providerRejected", FailureKind.AUTHORIZATION),
        (400, "providerRejected", FailureKind.PROVIDER_REJECTION),
        (200, "invalidResponse", FailureKind.PROTOCOL),
    ],
)
async def test_terminal_branches(operation: str, status: int, branch: str, kind: FailureKind) -> None:
    """Every operation classifies terminal provider responses consistently."""
    result: Any

    def answer(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            status, json={} if status == 200 else {"error": {"type": "invalid_request_error"}}
        )

    client = client_for(httpx.MockTransport(answer))
    if operation == "createCardCheckoutSession":
        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), checkout_input(True)
        )
    elif operation == "createACHCheckoutSession":
        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), checkout_input()
        )
    elif operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("cs_test_123")
        )
    elif operation == "getPaymentIntent":
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("pi_test_123")
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 2500)
        )
    assert result.branch == branch
    assert result.failure.kind == kind


@pytest.mark.parametrize(
    "operation",
    [
        "createCardCheckoutSession",
        "createACHCheckoutSession",
        "getCheckoutSession",
        "getPaymentIntent",
        "createRefund",
    ],
)
@pytest.mark.parametrize("status,kind", [(429, FailureKind.RATE_LIMIT), (503, FailureKind.AVAILABILITY)])
async def test_retry_after_and_availability(operation: str, status: int, kind: FailureKind) -> None:
    """429 and safe 503 failures retry, with bounded integer Retry-After."""
    result: Any
    client = client_for(
        httpx.MockTransport(lambda request: httpx.Response(status, headers={"Retry-After": "4000"}))
    )
    if operation == "createCardCheckoutSession":
        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), checkout_input(True)
        )
    elif operation == "createACHCheckoutSession":
        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), checkout_input()
        )
    elif operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("cs_test_123")
        )
    elif operation == "getPaymentIntent":
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("pi_test_123")
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 2500)
        )
    assert result.kind == "retry" and result.failure.kind == kind
    assert result.retry_after_seconds == (3600 if status == 429 or operation.startswith("create") else 0)


@pytest.mark.parametrize(
    "operation",
    [
        "createCardCheckoutSession",
        "createACHCheckoutSession",
        "getCheckoutSession",
        "getPaymentIntent",
        "createRefund",
    ],
)
async def test_oversized_success_is_invalid_response(operation: str) -> None:
    """Oversized successful responses never get decoded or persisted."""
    result: Any
    client = client_for(httpx.MockTransport(lambda request: httpx.Response(200, content=b"x" * 65)), 64)
    if operation == "createCardCheckoutSession":
        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), checkout_input(True)
        )
    elif operation == "createACHCheckoutSession":
        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), checkout_input()
        )
    elif operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("cs_test_123")
        )
    elif operation == "getPaymentIntent":
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("pi_test_123")
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 2500)
        )
    assert result.branch == "invalidResponse"
    assert result.failure.kind == FailureKind.RESPONSE_TOO_LARGE


@pytest.mark.parametrize(
    "operation",
    [
        "createCardCheckoutSession",
        "createACHCheckoutSession",
        "getCheckoutSession",
        "getPaymentIntent",
        "createRefund",
    ],
)
async def test_invalid_input_is_defect_without_provider_call(operation: str) -> None:
    """Invalid local input returns VALIDATION before credential or network access."""
    result: Any

    def no_request(request: httpx.Request) -> httpx.Response:
        raise AssertionError("invalid input reached Stripe")

    client = client_for(httpx.MockTransport(no_request))
    if operation == "createCardCheckoutSession":
        from dataclasses import replace

        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), replace(checkout_input(True), currency="USD")
        )
    elif operation == "createACHCheckoutSession":
        from dataclasses import replace

        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), replace(checkout_input(), currency="USD")
        )
    elif operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("invalid")
        )
    elif operation == "getPaymentIntent":
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("invalid")
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 0)
        )
    assert result.branch == "defect" and result.failure.kind == FailureKind.VALIDATION


@pytest.mark.parametrize(
    "status,headers,body,expected",
    [
        (409, {}, {}, "retry"),
        (409, {}, {"error": {"type": "idempotency_error"}}, "providerRejected"),
        (409, {"Stripe-Should-Retry": "false"}, {}, "providerRejected"),
        (500, {}, {}, "uncertain"),
        (500, {"Stripe-Should-Retry": "true"}, {}, "retry"),
        (503, {"Idempotent-Replayed": "true"}, {}, "uncertain"),
    ],
)
async def test_keyed_checkout_conflict_and_server_classification(
    status: int, headers: dict[str, str], body: dict[str, object], expected: str
) -> None:
    """Keyed writes follow Stripe's replay, 409, and 500 classification."""
    client = client_for(
        httpx.MockTransport(lambda request: httpx.Response(status, json=body, headers=headers))
    )
    result = await client.create_card_checkout_session().invoke(
        make_call("createCardCheckoutSession", True), checkout_input(True)
    )
    assert result.kind == expected if expected in ("retry", "uncertain") else result.branch == expected


async def test_refund_readback_finds_original_call_and_keeps_one_key() -> None:
    """An unhinted 500 resolves through a bounded marker lookup."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            return httpx.Response(500)
        return httpx.Response(
            200,
            json={
                "object": "list",
                "data": [{**REFUND_BODY, "metadata": {"dex_call_id": CALL_ID}}],
                "has_more": False,
            },
        )

    client = client_for(httpx.MockTransport(answer))
    result = await client.create_refund().invoke(
        make_call("createRefund", True), CreateRefundInput("pi_test_123", 2500)
    )
    assert result.value is not None
    assert result.branch == "refunded" and result.value.refund_id == "re_test_123"
    assert len(requests) == 2 and requests[1].url.params["limit"] == "100"
    assert "Idempotency-Key" not in requests[1].headers


@pytest.mark.parametrize(
    "readback_status,kind",
    [
        (200, FailureKind.AVAILABILITY),
        (401, FailureKind.AUTHENTICATION),
        (403, FailureKind.AUTHORIZATION),
        (503, FailureKind.AVAILABILITY),
    ],
)
async def test_refund_unconfirmed_and_readback_denial(readback_status: int, kind: FailureKind) -> None:
    """Refund read-back absence, denial, and transient failure remain distinct."""

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(500)
        if readback_status == 200:
            return httpx.Response(200, json={"object": "list", "data": [], "has_more": False})
        return httpx.Response(readback_status)

    client = client_for(httpx.MockTransport(answer))
    result = await client.create_refund().invoke(
        make_call("createRefund", True), CreateRefundInput("pi_test_123", 2500)
    )
    assert result.kind == ("retry" if readback_status == 503 else "uncertain")
    assert result.failure is not None
    assert result.failure.kind == kind


async def test_refund_readback_rejects_malformed_candidate_metadata() -> None:
    """A malformed list item cannot prove the original refund succeeded."""

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(500)
        return httpx.Response(
            200,
            json={
                "object": "list",
                "data": [{**REFUND_BODY, "metadata": {"dex_call_id": CALL_ID, "invalid": 5}}],
                "has_more": False,
            },
        )

    client = client_for(httpx.MockTransport(answer))
    result = await client.create_refund().invoke(
        make_call("createRefund", True), CreateRefundInput("pi_test_123", 2500)
    )
    assert result.kind == "uncertain"


@pytest.mark.parametrize("match_on_third_page", [True, False])
async def test_refund_readback_is_bounded_to_three_pages(match_on_third_page: bool) -> None:
    """A matching Call ID on page three resolves the write; page four is never fetched."""
    readback_requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(500)
        readback_requests.append(request)
        page = len(readback_requests)
        refund_id = f"re_page{page}"
        metadata = {"dex_call_id": CALL_ID} if page == 3 and match_on_third_page else {}
        return httpx.Response(
            200,
            json={
                "object": "list",
                "data": [{**REFUND_BODY, "id": refund_id, "metadata": metadata}],
                "has_more": True,
            },
        )

    client = client_for(httpx.MockTransport(answer))
    result = await client.create_refund().invoke(
        make_call("createRefund", True), CreateRefundInput("pi_test_123", 2500)
    )
    assert len(readback_requests) == 3
    assert [request.url.params.get("starting_after") for request in readback_requests] == [
        None,
        "re_page1",
        "re_page2",
    ]
    assert result.branch == ("refunded" if match_on_third_page else "uncertain")


@pytest.mark.parametrize(
    "operation",
    [
        "createCardCheckoutSession",
        "createACHCheckoutSession",
        "getCheckoutSession",
        "getPaymentIntent",
        "createRefund",
    ],
)
async def test_missing_credentials_are_authentication_defects(operation: str) -> None:
    """A missing connection never sends an unauthenticated provider request."""
    result: Any
    credentials: StaticCredentialProvider[Credentials] = StaticCredentialProvider({})
    client = Client(
        Config("http://127.0.0.1:12345/v1"),
        credentials,
        httpx.AsyncClient(
            transport=httpx.MockTransport(lambda request: pytest.fail("sent without credentials"))
        ),
    )
    if operation == "createCardCheckoutSession":
        result = await client.create_card_checkout_session().invoke(
            make_call(operation, True), checkout_input(True)
        )
    elif operation == "createACHCheckoutSession":
        result = await client.create_ach_checkout_session().invoke(
            make_call(operation, True), checkout_input()
        )
    elif operation == "getCheckoutSession":
        result = await client.get_checkout_session().invoke(
            make_call(operation), GetCheckoutSessionInput("cs_test_123")
        )
    elif operation == "getPaymentIntent":
        result = await client.get_payment_intent().invoke(
            make_call(operation), GetPaymentIntentInput("pi_test_123")
        )
    else:
        result = await client.create_refund().invoke(
            make_call(operation, True), CreateRefundInput("pi_test_123", 2500)
        )
    assert result.branch == "defect" and result.failure.kind == FailureKind.AUTHENTICATION


def test_definitions_match_manifest_branches() -> None:
    """Operation definitions preserve Go's stable routes and Execute defaults."""
    from dex_connectors.stripe import (
        CREATE_ACH_CHECKOUT_SESSION_DEFINITION,
        CREATE_CARD_CHECKOUT_SESSION_DEFINITION,
        CREATE_REFUND_DEFINITION,
        GET_CHECKOUT_SESSION_DEFINITION,
        GET_PAYMENT_INTENT_DEFINITION,
    )

    for mutation_definition in (
        CREATE_CARD_CHECKOUT_SESSION_DEFINITION,
        CREATE_ACH_CHECKOUT_SESSION_DEFINITION,
        CREATE_REFUND_DEFINITION,
    ):
        assert mutation_definition.branches is not None
        assert [branch.id for branch in mutation_definition.branches] == [
            "refunded" if mutation_definition.operation.operation_id == "createRefund" else "created",
            "providerRejected",
            "uncertain",
            "invalidResponse",
            "defect",
        ]
        assert mutation_definition.step_defaults.execute_method_timeout.total_seconds() == 30
    for query_definition in (GET_CHECKOUT_SESSION_DEFINITION, GET_PAYMENT_INTENT_DEFINITION):
        assert query_definition.branches is not None
        assert [branch.id for branch in query_definition.branches] == [
            "found",
            "notFound",
            "providerRejected",
            "invalidResponse",
            "defect",
        ]


async def test_sdk_runners_preserve_stripe_key_and_query_branch() -> None:
    """Public operation runners supply the stable key and validate a query result."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json=SESSION_BODY)

    client = client_for(httpx.MockTransport(answer))
    mutation = await run_mutation(
        client.create_card_checkout_session(),
        make_call("createCardCheckoutSession"),
        checkout_input(True),
    )
    query = await run_query(
        client.get_checkout_session(),
        make_call("getCheckoutSession"),
        GetCheckoutSessionInput("cs_test_123"),
    )
    assert mutation.receipt is not None and query.value is not None
    assert mutation.branch == "created" and str(mutation.receipt.idempotency_key) == str(CALL_ID)
    assert requests[0].headers["Idempotency-Key"] == CALL_ID
    assert query.branch == "found" and query.value.id == "cs_test_123"
