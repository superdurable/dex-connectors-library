# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of QuickBooks writes whose outcome is unknown."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
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

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Decode the last value only as its own type, as a strict codec would."""
        if not isinstance(self.value, value_type):
            raise TypeError("heartbeat has another shape")
        return self.value

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's heartbeat."""
        self.value = value

    @property
    def marker(self) -> Marker:
        """Return the recorded single-dispatch marker."""
        assert isinstance(self.value, Marker)
        return self.value


class Script:
    """Answer each request with the next scripted reply, repeating the last."""

    def __init__(self, *replies: httpx.Response) -> None:
        self.replies = list(replies)
        self.requests: list[httpx.Request] = []

    def __call__(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        return self.replies[min(len(self.requests), len(self.replies)) - 1]


async def create(
    answer: Answer, context: AttemptContext, *, max_sends: int = CREATE_CUSTOMER_DEFINITION.max_sends
) -> sdk.MutationResult[quickbooks.CreateCustomerOutput] | sdk.RetryError:
    """Run one Execute attempt of createCustomer, as MutationStep passes the definition cap."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = quickbooks.Client(
            quickbooks.Config(realm_id="9341453050298464"),
            sdk.StaticCredentialProvider(
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
async def test_unreadable_checkpoint_is_retried_without_a_request() -> None:
    """A fail-closed decision returns Retry, never a terminal branch."""
    script = Script(httpx.Response(200, json=CUSTOMER))
    context = AttemptContext()
    context.value = "not a checkpoint"
    context.attempt = 2
    assert isinstance(await create(script, context), sdk.RetryError)
    assert script.requests == []
