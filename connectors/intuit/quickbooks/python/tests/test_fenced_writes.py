# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of QuickBooks writes whose outcome is unknown."""

import asyncio
from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.intuit_quickbooks as quickbooks
from dex_connectors.intuit_quickbooks._generated.definitions import CREATE_CUSTOMER_DEFINITION

CONNECTION = sdk.ConnectionRef("quickbooks", "quickbooks-test")
CALL_ID = sdk.CallID("0fda9033-4ba6-5462-98fa-01d400ec4987")
CUSTOMER = {
    "Customer": {
        "Id": "58",
        "SyncToken": "0",
        "DisplayName": "City Agency",
        "PrimaryEmailAddr": {"Address": "accounts@cityagency.example.com"},
        "Active": True,
        "Balance": 0,
    }
}

Answer = Callable[[httpx.Request], httpx.Response]


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.value: object | None = None
        self.read_error: Exception | None = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.value is not None or self.read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Decode the last value as the requested type, whatever checkpoint format the SDK wrote."""
        if self.read_error is not None:
            raise self.read_error
        if value_type is Marker:
            return self.marker
        if isinstance(self.value, Marker):
            return {"singleDispatchMarkers": {str(self.value.call_id): self.value.to_wire()}}
        return self.value

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's heartbeat exactly as written."""
        self.value = value

    @property
    def marker(self) -> Marker:
        """Decode this call's marker from a single marker or the checkpoint map."""
        if isinstance(self.value, Marker):
            return self.value
        if not isinstance(self.value, dict):
            raise TypeError("heartbeat has another shape")
        return Marker.from_wire(self.value["singleDispatchMarkers"][str(CALL_ID)])


class Script:
    """Answer each request with the next scripted reply, repeating the last."""

    def __init__(self, *replies: httpx.Response) -> None:
        self.replies = list(replies)
        self.requests: list[httpx.Request] = []

    def __call__(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        return self.replies[min(len(self.requests), len(self.replies)) - 1]


class UnavailableBrokerCredentials:
    """Fail every credential read as a transient Dex credential broker outage."""

    async def resolve(self, call: sdk.Call) -> quickbooks.Credentials:
        """Raise the broker's transient unavailability error."""
        raise CredentialBrokerUnavailableError("credential broker is unavailable")


class HangingBodyStream(httpx.AsyncByteStream):
    """Send part of a 2xx body, then stall past the Fence write window."""

    async def __aiter__(self):  # type: ignore[override]
        yield b'{"Customer":{"Id":"5'
        await asyncio.sleep(30)


async def create(
    answer: Answer,
    context: AttemptContext,
    *,
    max_sends: int = CREATE_CUSTOMER_DEFINITION.max_sends,
    credentials: object | None = None,
) -> sdk.MutationResult[quickbooks.CreateCustomerOutput] | sdk.RetryError:
    """Run one Execute attempt of createCustomer, as MutationStep passes the definition cap."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = quickbooks.Client(
            quickbooks.Config(realm_id="9341453050298464"),
            credentials  # type: ignore[arg-type]
            or sdk.StaticCredentialProvider(
                {
                    CONNECTION: quickbooks.Credentials(
                        "ABtestQuickBooksClientID0123456789",
                        sdk.SecretString("client-secret"),
                        sdk.SecretString("intuit-test-access-token-0123456789"),
                        sdk.SecretString("refresh-token"),
                        sdk.SecretString(""),
                    )
                }
            ),
            http_client,
            local_provider_url="http://127.0.0.1:12345",
        )
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("quickbooks", "createCustomer"),
            context=context,  # type: ignore[arg-type]
            max_sends=max_sends,
        )
        try:
            return await sdk.run_mutation(
                client.create_customer(),
                call,
                quickbooks.CreateCustomerInput(
                    "City Agency", email_address="accounts@cityagency.example.com"
                ),
            )
        except sdk.RetryError as error:
            return error


def branch(result: sdk.MutationResult[quickbooks.CreateCustomerOutput] | sdk.RetryError) -> str:
    """Return the selected branch of a terminal result."""
    assert isinstance(result, sdk.MutationResult), result
    return result.branch


@pytest.mark.asyncio
async def test_unknown_outcome_is_resent_with_the_same_requestid_below_the_cap() -> None:
    """A 500, 409, or 501 keeps the claim, and the retry resends under the same requestid."""
    for status in (500, 409, 501, 425):
        script = Script(httpx.Response(status, json={}), httpx.Response(200, json=CUSTOMER))
        context = AttemptContext()
        assert isinstance(await create(script, context), sdk.RetryError), status
        assert context.marker.state == "claimed" and context.marker.send_count == 1
        context.attempt = 2
        assert branch(await create(script, context)) == "created"
        assert script.requests[0].url.params["requestid"] == script.requests[1].url.params["requestid"]


@pytest.mark.asyncio
async def test_unknown_outcome_selects_uncertain_only_at_the_cap() -> None:
    """Sends stop at maxSends, and an attempt at the cap never sends."""
    script = Script(httpx.Response(500, json={}))
    context = AttemptContext()
    for attempt in range(1, CREATE_CUSTOMER_DEFINITION.max_sends):
        context.attempt = attempt
        assert isinstance(await create(script, context), sdk.RetryError)
    context.attempt = CREATE_CUSTOMER_DEFINITION.max_sends
    assert branch(await create(script, context)) == "uncertain"
    context.attempt += 1
    assert branch(await create(script, context)) == "uncertain"
    assert len(script.requests) == CREATE_CUSTOMER_DEFINITION.max_sends


@pytest.mark.asyncio
async def test_accepted_unusable_write_is_read_back_by_its_saved_id() -> None:
    """An accepted ID is checkpointed, and the next attempt reads it back without resending."""
    script = Script(
        httpx.Response(200, json={"Customer": {"Id": "58"}}),
        httpx.Response(503, json={}),
        httpx.Response(200, json=CUSTOMER),
    )
    context = AttemptContext()
    assert isinstance(await create(script, context), sdk.RetryError)
    assert context.marker.provider_id == "58"
    context.attempt = 2
    assert branch(await create(script, context)) == "created"
    assert [request.method for request in script.requests] == ["POST", "GET", "GET"]


class _BrokenStream(httpx.AsyncByteStream):
    async def __aiter__(self):  # type: ignore[override]
        yield b'{"Customer":{"Id":"5'
        raise httpx.ReadError("body interrupted")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "accepted",
    [
        lambda: httpx.Response(200, json={"Customer": {}}),
        lambda: httpx.Response(200, stream=_BrokenStream()),
    ],
    ids=["unusable body", "body broken after 2xx headers"],
)
async def test_accepted_write_without_an_id_is_never_resent_below_the_cap(
    accepted: Callable[[], httpx.Response],
) -> None:
    """QuickBooks accepted the write, so without an ID to read back it selects uncertain at once."""
    script = Script(accepted())
    context = AttemptContext()
    assert branch(await create(script, context)) == "uncertain"
    assert [request.method for request in script.requests] == ["POST"]


@pytest.mark.asyncio
async def test_definite_refusal_of_a_first_write_records_can_resend() -> None:
    """A 6240 Fault selects nameConflict after recording canResend."""
    fault = {"Fault": {"type": "ValidationFault", "Error": [{"code": "6240"}]}}
    script = Script(httpx.Response(400, json=fault))
    context = AttemptContext()
    assert branch(await create(script, context)) == "nameConflict"
    assert context.marker.state == "canResend"


@pytest.mark.asyncio
async def test_refusal_after_a_possible_earlier_write_selects_uncertain() -> None:
    """The earlier send may have created the customer that now conflicts."""
    fault = {"Fault": {"type": "ValidationFault", "Error": [{"code": "6240"}]}}
    script = Script(httpx.Response(502, json={}), httpx.Response(400, json=fault))
    context = AttemptContext()
    assert isinstance(await create(script, context), sdk.RetryError)
    context.attempt = 2
    assert branch(await create(script, context)) == "uncertain"


@pytest.mark.asyncio
async def test_unprocessed_write_is_retried_without_counting_it() -> None:
    """A 429 or 503 records canResend with the earlier count."""
    for status in (429, 503):
        script = Script(
            httpx.Response(status, json={}, headers={"Retry-After": "1"}), httpx.Response(200, json=CUSTOMER)
        )
        context = AttemptContext()
        assert isinstance(await create(script, context), sdk.RetryError)
        assert context.marker.state == "canResend" and context.marker.send_count == 0
        context.attempt = 2
        assert branch(await create(script, context)) == "created"
        assert context.marker.send_count == 1


@pytest.mark.asyncio
async def test_write_window_cutting_a_2xx_body_selects_uncertain_without_resending() -> None:
    """2xx headers arrived before the write deadline cut the body, so QuickBooks accepted the write."""
    script = Script(httpx.Response(200, stream=HangingBodyStream()))
    context = AttemptContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    result = await create(script, context)
    assert branch(result) == "uncertain"
    assert isinstance(result, sdk.MutationResult) and result.failure is not None
    assert "write window ended" in result.failure.message
    assert [request.method for request in script.requests] == ["POST"]


class _BrokenErrorStream(httpx.AsyncByteStream):
    async def __aiter__(self):  # type: ignore[override]
        yield b'{"Fault":'
        raise httpx.ReadError("error body interrupted")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "refusal",
    [
        lambda: httpx.Response(
            400,
            content=b'{"Fault":{"type":"ValidationFault","Error":[{"code":"6240"}]}}' + b" " * 70_000,
            headers={"Content-Type": "application/json"},
        ),
        lambda: httpx.Response(404, stream=_BrokenErrorStream()),
    ],
    ids=["oversized 400", "404 body broken mid-read"],
)
async def test_refusal_status_with_an_incomplete_error_body_is_an_unknown_outcome(
    refusal: Callable[[], httpx.Response],
) -> None:
    """An unread refusal body proves nothing, so the send counts and is resent below the cap."""
    script = Script(refusal(), httpx.Response(200, json=CUSTOMER))
    context = AttemptContext()
    assert isinstance(await create(script, context), sdk.RetryError)
    assert context.marker.state == "claimed" and context.marker.send_count == 1
    context.attempt = 2
    assert branch(await create(script, context)) == "created"
    assert len(script.requests) == 2


@pytest.mark.asyncio
async def test_unavailable_credential_broker_retries_without_counting_a_send() -> None:
    """A transient credential broker failure is never a defect, and nothing is sent."""
    script = Script(httpx.Response(200, json=CUSTOMER))
    context = AttemptContext()
    result = await create(script, context, credentials=UnavailableBrokerCredentials())
    assert isinstance(result, sdk.RetryError)
    assert result.failure.kind == sdk.FailureKind.AVAILABILITY
    assert context.marker.state == "canResend" and context.marker.send_count == 0
    assert script.requests == []


class FailingCredentials:
    """Fail every credential read with error."""

    def __init__(self, error: Exception) -> None:
        """Keep the error to raise."""
        self.error = error

    async def resolve(self, call: sdk.Call) -> quickbooks.Credentials:
        """Raise the configured error."""
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_transient"),
    [
        (ValueError("connection is not configured"), True),
        (RuntimeError("unlisted"), True),
        (sdk.ReauthorizationRequiredError(), False),
        (ObjectNotFoundError(), False),
        (ConnectionNotSelectedError(), False),
        (CredentialBrokerRejectedError(), False),
    ],
)
async def test_credential_resolve_failures_split_into_terminal_and_transient(
    error: Exception, is_transient: bool
) -> None:
    """Only the terminal classes select a branch; every other resolve failure Retries unsent."""
    script = Script(httpx.Response(200, json=CUSTOMER))
    result = await create(script, AttemptContext(), credentials=FailingCredentials(error))
    assert isinstance(result, sdk.RetryError) == is_transient
    if not is_transient:
        assert not isinstance(result, sdk.RetryError) and result.branch not in ("uncertain", "created")
    assert script.requests == []


@pytest.mark.asyncio
async def test_unreadable_checkpoint_is_retried_without_a_request() -> None:
    """A heartbeat read error fails closed and returns Retry, never a terminal branch."""
    script = Script(httpx.Response(200, json=CUSTOMER))
    context = AttemptContext()
    context.read_error = RuntimeError("heartbeat store unavailable")
    context.attempt = 2
    assert isinstance(await create(script, context), sdk.RetryError)
    assert script.requests == []
