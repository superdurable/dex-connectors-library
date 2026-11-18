# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded resend of Asana creates and comments whose outcome is unknown."""

import asyncio
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.singledispatch import Marker, State

import dex_connectors.asana as asana
from dex_connectors.asana._generated.definitions import ADD_COMMENT_DEFINITION, CREATE_TASK_DEFINITION

CONNECTION = sdk.ConnectionRef("asana", "asana-test")
CALL_ID = sdk.CallID("6072a0ce-9ddb-5937-8998-675e48cfa406")
TASK_ID = "1204567890123456"
CREATED = f'{{"data":{{"gid":"{TASK_ID}"}}}}'


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, attempt: int = 1, heartbeat_value: object = None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value = heartbeat_value
        self.heartbeat_read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode this call's checkpoint from either SDK heartbeat format."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        entry = self.heartbeat_value["singleDispatchMarkers"].get(str(CALL_ID))
        return None if entry is None else Marker.from_wire(entry)

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None or self.heartbeat_read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        if self.heartbeat_read_error is not None:
            raise self.heartbeat_read_error
        seeded = self.heartbeat_value
        if isinstance(seeded, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(seeded.call_id): seeded.to_wire()}}
        if isinstance(self.heartbeat_value, dict) and value_type is Marker:
            return self.marker
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value


class BrokenBodyStream(httpx.AsyncByteStream):
    """Fail after the response headers arrived."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        raise httpx.ReadError("connection lost while reading the body")
        yield b""


class FailingCredentialProvider:
    """Raise one credential resolution error for every call."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, _call: sdk.Call) -> asana.Credentials:
        """Raise the configured error."""
        raise self.error


async def run(
    operation_name: str,
    status: int,
    body: str,
    context: AttemptContext,
    requests: list[httpx.Request],
    *,
    max_sends: int = CREATE_TASK_DEFINITION.max_sends,
    is_body_broken: bool = False,
    is_body_stalled: bool = False,
    has_credentials: bool = True,
    credential_error: Exception | None = None,
) -> str:
    """Run one Execute attempt of an Asana write and return its branch or `retry`."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if is_body_broken:
            return httpx.Response(status, stream=BrokenBodyStream())
        if is_body_stalled:
            return httpx.Response(status, stream=StallingBodyStream())
        return httpx.Response(status, content=body.encode(), headers={"Content-Type": "application/json"})

    credentials = {CONNECTION: asana.Credentials(sdk.SecretString("token"))} if has_credentials else {}
    provider: sdk.CredentialProvider[asana.Credentials] = (
        sdk.StaticCredentialProvider(credentials)
        if credential_error is None
        else FailingCredentialProvider(credential_error)
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = asana.Client(
            asana.Config(endpoint="http://127.0.0.1:22201/api/1.0"),
            provider,
            http_client,
        )
        operation = sdk.OperationRef("asana", operation_name)
        call = sdk.Call(CALL_ID, CONNECTION, operation, context=context, max_sends=max_sends)
        try:
            if operation_name == "createTask":
                task = asana.CreateTaskInput(name="Replace badge reader", project_id="1201000000000001")
                return (await sdk.run_mutation(client.create_task(), call, task)).branch
            comment = asana.AddCommentInput(task_id=TASK_ID, text="Approved.")
            return (await sdk.run_mutation(client.add_comment(), call, comment)).branch
        except sdk.RetryError:
            return "retry"


@pytest.mark.asyncio
async def test_unknown_outcome_resends_below_the_cap_then_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    for attempt in range(1, CREATE_TASK_DEFINITION.max_sends):
        context.attempt = attempt
        assert await run("createTask", 500, "{}", context, requests) == "retry"
        assert context.marker is not None and context.marker.send_count == attempt
    context.attempt = CREATE_TASK_DEFINITION.max_sends
    assert await run("createTask", 500, "{}", context, requests) == "uncertain"
    context.attempt += 1
    assert await run("createTask", 500, "{}", context, requests) == "uncertain"
    assert len(requests) == 3


@pytest.mark.asyncio
async def test_send_cap_of_one_never_resends() -> None:
    requests: list[httpx.Request] = []
    assert await run("addComment", 409, "{}", AttemptContext(), requests, max_sends=1) == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_saved_identity_is_read_back_instead_of_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(attempt=2)
    assert await run("createTask", 201, CREATED, context, requests) == "created"
    assert context.marker is not None and context.marker.provider_id == TASK_ID
    context.attempt = 3
    assert await run("createTask", 200, CREATED, context, requests) == "created"
    assert [request.method for request in requests] == ["POST", "GET"]
    at_cap = AttemptContext(attempt=ADD_COMMENT_DEFINITION.max_sends + 1)
    assert await run("addComment", 201, CREATED, at_cap, requests) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_is_never_resent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    refusal = '{"errors":[{"message":"bad"}]}'
    assert await run("createTask", 400, refusal, context, requests) == "providerRejected"
    assert context.marker is not None and context.marker.state == "canResend"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_refusal_of_a_resend_is_uncertain() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("addComment", 502, "{}", context, requests) == "retry"
    context.attempt = 2
    assert await run("addComment", 403, "{}", context, requests) == "uncertain"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_unreadable_checkpoint_retries_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(2)
    context.heartbeat_read_error = RuntimeError("heartbeat store unavailable")
    assert await run("createTask", 201, CREATED, context, requests) == "retry"
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation_name", ["createTask", "addComment"])
@pytest.mark.parametrize(
    ("body", "is_body_broken"),
    [('{"data":{"gid":"invalid"}}', False), ("", True)],
    ids=["no usable ID", "body breaks after 2xx headers"],
)
async def test_accepted_write_without_an_identity_is_uncertain_and_never_resent(
    operation_name: str, body: str, is_body_broken: bool
) -> None:
    requests: list[httpx.Request] = []
    result = await run(operation_name, 201, body, AttemptContext(), requests, is_body_broken=is_body_broken)
    assert result == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_unreadable_error_body_is_an_unknown_outcome() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run("createTask", 400, "", context, requests, is_body_broken=True) == "retry"
    assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createTask", "addComment"])
@pytest.mark.parametrize(
    "error",
    [CredentialBrokerUnavailableError(), ValueError("unlisted"), LookupError("unlisted"), None],
    ids=["broker unavailable", "unlisted value", "unlisted lookup", "static connection not configured"],
)
async def test_transient_credential_failure_after_a_possible_send_retries_without_not_sent(
    operation: str, error: Exception | None
) -> None:
    requests: list[httpx.Request] = []
    marker = Marker(CALL_ID, 1, State.CLAIMED, send_count=1)
    context = AttemptContext(2, marker)
    result = await run(
        operation, 201, CREATED, context, requests, has_credentials=error is not None, credential_error=error
    )
    assert result == "retry"
    assert context.marker is not None and context.marker.state == "claimed" and context.marker.attempt == 1
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createTask", "addComment"])
@pytest.mark.parametrize(
    "error",
    [
        sdk.ReauthorizationRequiredError("reauthorize"),
        ConnectionNotSelectedError(),
        CredentialBrokerRejectedError(),
        ObjectNotFoundError("absent"),
    ],
    ids=["reauthorization", "not selected", "broker rejected", "object not found"],
)
async def test_terminal_credential_failure_after_a_possible_send_is_uncertain(
    operation: str, error: Exception
) -> None:
    for marker in (
        Marker(CALL_ID, 1, State.CLAIMED, send_count=1),
        Marker(CALL_ID, 1, State.CLAIMED, send_count=1, provider_id=TASK_ID),
    ):
        requests: list[httpx.Request] = []
        context = AttemptContext(2, marker)
        result = await run(
            operation, 201, CREATED, context, requests, has_credentials=False, credential_error=error
        )
        assert result == "uncertain"
        assert context.marker is not None and context.marker.state == "claimed"
        assert requests == []


@pytest.mark.asyncio
async def test_credential_failure_before_any_send() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    unavailable = CredentialBrokerUnavailableError()
    assert await run("createTask", 201, CREATED, context, requests, credential_error=unavailable) == "retry"
    assert context.marker is not None and context.marker.state == State.NOT_SENT
    reauthorize = sdk.ReauthorizationRequiredError("reauthorize")
    result = await run("addComment", 201, CREATED, AttemptContext(), requests, credential_error=reauthorize)
    assert result == "defect"
    assert requests == []


class StallingBodyStream(httpx.AsyncByteStream):
    """Yield nothing until cancelled, so the write window cuts the 2xx body."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(3600)
        yield b""


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createTask", "addComment"])
async def test_write_deadline_after_2xx_headers_is_uncertain_and_never_resent(operation: str) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    # The Fence write window is 7/12 of the remaining attempt time.
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    assert await run(operation, 201, "", context, requests, is_body_stalled=True) == "uncertain"
    assert len(requests) == 1
    assert context.marker is not None and context.marker.state == State.CLAIMED


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        f'{{"data":{{"gid":"{TASK_ID}","permalink_url":7}}}}',
        f'{{"data":{{"gid":"{TASK_ID}","created_at":7}}}}',
        f'{{"data":{{"gid":"{TASK_ID}","created_by":"someone"}}}}',
        f'{{"data":{{"gid":"{TASK_ID}","created_by":{{"gid":7}}}}}}',
    ],
    ids=["permalink", "created_at", "author", "author gid"],
)
async def test_valid_gid_with_mistyped_fields_retries_and_reads_back_by_id(body: str) -> None:
    operation = "addComment" if "created_by" in body else "createTask"
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert await run(operation, 201, body, context, requests) == "retry"
    assert context.marker is not None and context.marker.provider_id == TASK_ID
    context.attempt = 2
    expected = "added" if operation == "addComment" else "created"
    assert await run(operation, 200, CREATED, context, requests) == expected
    assert [request.method for request in requests] == ["POST", "GET"]
