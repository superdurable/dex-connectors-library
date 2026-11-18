# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of appendTableRows and credential failures of its writes."""

from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta
from typing import get_origin

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.microsoft_excel as excel

CONNECTION = sdk.ConnectionRef("microsoft", "approvals")
CALL_ID = sdk.CallID("6072a0ce-9ddb-5937-8998-675e48cfa407")
COLUMNS = b'{"value":[{"id":"1","index":0,"name":"RequestId"}]}'
EMPTY_KEYS = b'{"rowCount":1,"columnCount":1,"values":[[""]]}'
STORED_KEYS = b'{"rowCount":2,"columnCount":1,"values":[[""],["REQ-1"]]}'


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None
        self.heartbeat_count = 0

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat, failing like Dex when it cannot decode into value_type."""
        value = self.heartbeat_value
        decoded_type = get_origin(value_type) or value_type
        if decoded_type is not object and not isinstance(value, decoded_type):
            raise TypeError("the heartbeat does not decode into the requested type")
        return value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_count += 1
        self.heartbeat_value = value

    def next_attempt(self) -> None:
        """Move to the following attempt of the same Step execution."""
        self.attempt += 1


class BrokenStream(httpx.AsyncByteStream):
    """A response body that fails after its first bytes."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"index":0'
        raise httpx.ReadError("connection reset")


class FailingCredentials:
    """A credential provider that always fails with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> excel.Credentials:
        """Raise the configured error."""
        raise self.error


def _append_input() -> excel.AppendTableRowsInput:
    return excel.AppendTableRowsInput(
        drive_id="b!drive", workbook_id="01WORKBOOK", table="Decisions", key_column="RequestId",
        rows=({"RequestId": "REQ-1"},),
    )


async def _run(
    respond: Callable[[httpx.Request], httpx.Response],
    context: AttemptContext,
    credentials: object | None = None,
) -> str:
    """Run one appendTableRows attempt and return its branch or `retry`."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        provider = credentials or sdk.StaticCredentialProvider(
            {CONNECTION: excel.Credentials(access_token=sdk.SecretString("inert-token"))}
        )
        client = excel.Client(excel.Config(), provider, http_client, local_provider_url="http://localhost")  # type: ignore[arg-type]
        call = sdk.Call(
            CALL_ID, CONNECTION, sdk.OperationRef("microsoft-excel", "appendTableRows"),
            context=context, max_sends=3,
        )
        try:
            return (await sdk.run_mutation(client.append_table_rows(), call, _append_input())).branch
        except sdk.RetryError:
            return "retry"


@pytest.mark.asyncio
async def test_a_success_status_whose_body_breaks_is_appended_and_never_resent() -> None:
    """A 2xx status proves the append even when the body read then fails."""
    posts: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            posts.append(request)
            return httpx.Response(201, stream=BrokenStream())
        return httpx.Response(200, content=COLUMNS if request.url.path.endswith("/columns") else EMPTY_KEYS)

    assert await _run(respond, AttemptContext()) == "appended"
    assert len(posts) == 1


@pytest.mark.asyncio
async def test_the_key_column_is_read_before_a_resend_after_a_throttled_resend() -> None:
    """After unknown, then 429, the third attempt reads the keys and finds the late first append."""
    statuses = [502, 429]
    requests: list[str] = []
    is_first_append_visible = False

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request.method)
        if request.method == "POST":
            return httpx.Response(statuses.pop(0), content=b"{}")
        if request.url.path.endswith("/columns"):
            return httpx.Response(200, content=COLUMNS)
        return httpx.Response(200, content=STORED_KEYS if is_first_append_visible else EMPTY_KEYS)

    context = AttemptContext()
    assert await _run(respond, context) == "retry"
    context.next_attempt()
    assert await _run(respond, context) == "retry"
    context.next_attempt()
    is_first_append_visible = True
    assert await _run(respond, context) == "appended"
    assert requests.count("POST") == 2
    assert requests[-2:] == ["GET", "GET"]


@pytest.mark.asyncio
async def test_a_transient_credential_failure_retries_even_after_a_possible_send() -> None:
    """A credential outage is never a defect, and notSent is never recorded after a possible send."""

    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    outage = FailingCredentials(RuntimeError("credential store offline"))
    first = AttemptContext()
    assert await _run(respond, first, outage) == "retry"
    assert first.heartbeat_count == 1
    after_possible_send = AttemptContext(attempt=2)
    assert await _run(respond, after_possible_send, outage) == "retry"
    assert after_possible_send.heartbeat_count == 0


@pytest.mark.asyncio
async def test_a_terminal_credential_failure_is_uncertain_after_a_possible_send() -> None:
    """Reauthorization selects defect on a first attempt and uncertain after a possible send."""

    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    revoked = FailingCredentials(sdk.ReauthorizationRequiredError())
    assert await _run(respond, AttemptContext(), revoked) == "defect"
    assert await _run(respond, AttemptContext(attempt=2), revoked) == "uncertain"


@pytest.mark.asyncio
async def test_update_values_retries_a_transient_credential_failure() -> None:
    """The repeat-safe write retries a credential outage and rejects a revoked connection."""

    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    update = excel.UpdateValuesInput("b!drive", "01WORKBOOK", "Summary", "A1", (("ready",),))
    branches = []
    for error in (RuntimeError("credential store offline"), sdk.ReauthorizationRequiredError()):
        async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
            client = excel.Client(excel.Config(), FailingCredentials(error), http_client, local_provider_url="http://localhost")  # type: ignore[arg-type]
            call = sdk.Call(CALL_ID, CONNECTION, sdk.OperationRef("microsoft-excel", "updateValues"))
            try:
                branches.append((await sdk.run_mutation(client.update_values(), call, update)).branch)
            except sdk.RetryError:
                branches.append("retry")
    assert branches == ["retry", "defect"]
