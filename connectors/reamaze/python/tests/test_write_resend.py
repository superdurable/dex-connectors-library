# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read-back, resend, and acceptance rules of the fenced createConversation and replyToConversation."""

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk import projectconfig
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.reamaze as reamaze
from dex_connectors.reamaze import Credentials
from dex_connectors.reamaze._generated.connection import CredentialMaterialError

CONNECTION = sdk.ConnectionRef("reamaze", "reamaze-test")


def sdk_wrapped_material_error() -> ValueError:
    """Build the error the SDK project provider raises over a decoder failure."""
    try:
        try:
            raise CredentialMaterialError("Re:amaze credential api_token is required")
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
CALL_ID = sdk.CallID("0d6f4bc4-7a51-5d24-9a38-6d8f4f6a1e10")
CREDENTIALS = Credentials("agent@acme.example.com", sdk.SecretString("reamazeTestToken0123456789"))
CREATE_INPUT = reamaze.CreateConversationInput(
    subject="Double charge",
    message="I was charged twice.",
    channel="support",
    requester=reamaze.ConversationRequesterInput(email="jane@acme.example.com"),
)
REPLY_INPUT = reamaze.ReplyToConversationInput(
    conversation_id="double-charge", text="Note.", is_internal_note=True
)


def conversation_wire(key: str) -> dict[str, Any]:
    """Return a conversation that carries the Step's dispatch key."""
    return {
        "slug": "double-charge",
        "subject": "Double charge",
        "status": 0,
        "created_at": "2026-01-26T14:02:00Z",
        "tag_list": ["billing"],
        "category": {"slug": "support", "name": "Support", "channel": 1},
        "data": {"dex_dispatch_key": key},
    }


def message_wire(origin_id: str) -> dict[str, Any]:
    """Return an internal note with the given origin ID."""
    return {
        "body": "Note.",
        "visibility": 1,
        "origin": 7,
        "origin_id": origin_id,
        "created_at": "2026-01-27T12:00:00Z",
    }


def page(name: str, entries: list[Any]) -> dict[str, Any]:
    """Return one Re:amaze list page."""
    return {"page_size": 30, "page_count": 1 if entries else 0, "total_count": len(entries), name: entries}


def json_response(status: int, value: object) -> httpx.Response:
    """Encode one JSON response."""
    return httpx.Response(status, content=json.dumps(value).encode())


class BodyBreaksAfterHeaders(httpx.AsyncByteStream):
    """A response body whose connection drops after the status line and headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"slug":"double'
        raise httpx.ReadError("connection reset after headers")


class HangingBody(httpx.AsyncByteStream):
    """A response body that never finishes, so the fence write window cuts it off."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        await asyncio.sleep(60)
        yield b""


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self, deadline_seconds: float = 120) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=deadline_seconds)
        self.heartbeat_value: object = None
        self.heartbeat_count = 0

    @property
    def marker(self) -> Marker | None:
        """Decode this call's marker from a single marker or the checkpoint map."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        return Marker.from_wire(self.heartbeat_value["singleDispatchMarkers"][str(CALL_ID)])

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat, translating a single marker to the checkpoint map the SDK reads."""
        if isinstance(self.heartbeat_value, Marker) and value_type is not Marker:
            marker = self.heartbeat_value
            return {"singleDispatchMarkers": {str(marker.call_id): marker.to_wire()}}
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's heartbeat exactly as written."""
        self.heartbeat_count += 1
        self.heartbeat_value = value

    def next_attempt(self) -> None:
        """Start the next Execute attempt with the stored heartbeat."""
        self.attempt += 1
        self.heartbeat_count = 0


class SwitchingCredentialProvider:
    """Resolve the test credentials until a test sets an error."""

    def __init__(self) -> None:
        self.error: Exception | None = None

    async def resolve(self, call: sdk.Call) -> Credentials:
        """Return the credentials or raise the configured error."""
        if self.error is not None:
            raise self.error
        return CREDENTIALS


Responder = Callable[[httpx.Request], httpx.Response]


async def run(
    operation: str,
    respond: Responder,
    context: AttemptContext,
    requests: list[httpx.Request],
    credentials: SwitchingCredentialProvider | None = None,
    max_response_bytes: int = 4_194_304,
) -> sdk.MutationResult[Any] | sdk.RetryError:
    """Run one Execute attempt of a fenced write with the generated send cap."""

    def record(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return respond(request)

    async with httpx.AsyncClient(transport=httpx.MockTransport(record)) as transport:
        client = reamaze.Client(
            reamaze.Config("acme", max_response_bytes=max_response_bytes),
            credentials or SwitchingCredentialProvider(),
            transport,
            "http://127.0.0.1:12345/api/v1",
        )
        operation_ref = sdk.OperationRef("reamaze", operation)
        call = sdk.Call(CALL_ID, CONNECTION, operation_ref, context=context, max_sends=3)
        try:
            if operation == "createConversation":
                return await sdk.run_mutation(client.create_conversation(), call, CREATE_INPUT)
            return await sdk.run_mutation(client.reply_to_conversation(), call, REPLY_INPUT)
        except sdk.RetryError as error:
            return error


def methods(requests: list[httpx.Request]) -> list[str]:
    """Return each request's method in order."""
    return [request.method for request in requests]


class ScriptedProvider:
    """Answer POSTs in order and serve read-backs that find the write only after the resend."""

    def __init__(self, operation: str, post_statuses: list[int]) -> None:
        self.operation = operation
        self.post_statuses = post_statuses
        self.key = ""
        self.reads = 0
        self.posts = 0

    def __call__(self, request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            payload = json.loads(request.content)
            if self.operation == "createConversation":
                self.key = payload["conversation"]["data"]["dex_dispatch_key"]
            else:
                self.key = payload["message"]["origin_id"]
            status = self.post_statuses[self.posts]
            self.posts += 1
            return json_response(status, {})
        self.reads += 1
        is_visible = self.reads > 1
        if self.operation == "createConversation":
            conversations = [conversation_wire(self.key)] if is_visible else []
            return json_response(200, page("conversations", conversations))
        return json_response(200, page("messages", [message_wire(self.key)] if is_visible else []))


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createConversation", "replyToConversation"])
@pytest.mark.parametrize(
    "credential_error, want",
    [
        (None, "confirmed"),
        (sdk.ReauthorizationRequiredError("reconnect"), "uncertain"),
        (OSError("credential store outage"), "retry"),
    ],
)
async def test_attempt_after_a_rate_limited_resend_reads_back_first(
    operation: str, credential_error: Exception | None, want: str
) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    credentials = SwitchingCredentialProvider()
    provider = ScriptedProvider(operation, [502, 429])
    assert isinstance(await run(operation, provider, context, requests, credentials), sdk.RetryError)
    context.next_attempt()
    assert isinstance(await run(operation, provider, context, requests, credentials), sdk.RetryError)
    assert methods(requests) == ["POST", "GET", "POST"]
    assert context.marker is not None and context.marker.state == "canResend"

    context.next_attempt()
    credentials.error = credential_error
    result = await run(operation, provider, context, requests, credentials)
    if want == "confirmed":
        assert isinstance(result, sdk.MutationResult)
        assert result.branch in ("created", "replied")
        assert result.value.was_already_applied
        assert methods(requests) == ["POST", "GET", "POST", "GET"], "the first send still counts"
        return
    if want == "uncertain":
        assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    else:
        assert isinstance(result, sdk.RetryError)
    assert len(requests) == 3, "nothing is sent without credentials"
    assert context.heartbeat_count == 0, "no notSent is recorded after a possible send"


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createConversation", "replyToConversation"])
@pytest.mark.parametrize("is_found", [True, False])
async def test_accepted_write_whose_body_breaks_after_headers_is_never_resent(
    operation: str, is_found: bool
) -> None:
    key = ""

    def respond(request: httpx.Request) -> httpx.Response:
        nonlocal key
        if request.method == "POST":
            payload = json.loads(request.content)
            if operation == "createConversation":
                key = payload["conversation"]["data"]["dex_dispatch_key"]
            else:
                key = payload["message"]["origin_id"]
            return httpx.Response(201, stream=BodyBreaksAfterHeaders())
        if operation == "createConversation":
            return json_response(200, page("conversations", [conversation_wire(key)] if is_found else []))
        return json_response(200, page("messages", [message_wire(key)] if is_found else []))

    requests: list[httpx.Request] = []
    result = await run(operation, respond, AttemptContext(), requests)
    assert isinstance(result, sdk.MutationResult)
    confirmed_branch = "created" if operation == "createConversation" else "replied"
    assert result.branch == (confirmed_branch if is_found else "uncertain")
    assert methods(requests) == ["POST", "GET"], "the default cap of three never resends an accepted write"


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createConversation", "replyToConversation"])
@pytest.mark.parametrize("is_found", [True, False])
async def test_2xx_headers_then_the_write_deadline_cuts_the_body_is_read_back_and_never_resent(
    operation: str, is_found: bool
) -> None:
    key = ""

    def respond(request: httpx.Request) -> httpx.Response:
        nonlocal key
        if request.method == "POST":
            payload = json.loads(request.content)
            if operation == "createConversation":
                key = payload["conversation"]["data"]["dex_dispatch_key"]
            else:
                key = payload["message"]["origin_id"]
            return httpx.Response(201, stream=HangingBody())
        if operation == "createConversation":
            return json_response(200, page("conversations", [conversation_wire(key)] if is_found else []))
        return json_response(200, page("messages", [message_wire(key)] if is_found else []))

    requests: list[httpx.Request] = []
    # The read-back ends one second before the deadline, after a write window of 7/12 of it.
    context = AttemptContext(deadline_seconds=3)
    result = await run(operation, respond, context, requests)
    assert isinstance(result, sdk.MutationResult), "an accepted write without a saved ID is never retried"
    confirmed_branch = "created" if operation == "createConversation" else "replied"
    assert result.branch == (confirmed_branch if is_found else "uncertain")
    assert methods(requests) == ["POST", "GET"]
    assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
async def test_non_2xx_headers_then_the_write_deadline_cuts_the_body_retries_with_the_send_counted() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext(deadline_seconds=0.6)
    result = await run(
        "createConversation", lambda _: httpx.Response(400, stream=HangingBody()), context, requests
    )
    assert isinstance(result, sdk.RetryError), "an unread refusal cannot prove Re:amaze applied nothing"
    assert methods(requests) == ["POST"]
    assert context.marker is not None and context.marker.state == "claimed"


@pytest.mark.asyncio
async def test_accepted_write_read_back_that_outlasts_the_attempt_deadline_is_uncertain() -> None:
    async def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            return httpx.Response(201, stream=HangingBody())
        await asyncio.sleep(60)
        return json_response(200, page("conversations", []))

    requests: list[httpx.Request] = []
    context = AttemptContext(deadline_seconds=3)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = reamaze.Client(
            reamaze.Config("acme"), SwitchingCredentialProvider(), transport, "http://127.0.0.1:12345/api/v1"
        )
        operation = sdk.OperationRef("reamaze", "createConversation")
        call = sdk.Call(CALL_ID, CONNECTION, operation, context=context, max_sends=3)
        result = await sdk.run_mutation(client.create_conversation(), call, CREATE_INPUT)
    assert result.branch == "uncertain", "the next attempt could resend a write Re:amaze accepted"
    assert methods(requests) == ["POST", "GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createConversation", "replyToConversation"])
@pytest.mark.parametrize(
    "error",
    [
        projectconfig.ObjectNotFoundError("connection"),
        projectconfig.ConnectionNotSelectedError("connection"),
        projectconfig.CredentialBrokerRejectedError("broker"),
        CredentialMaterialError("Re:amaze credential api_token is required"),
        sdk_wrapped_material_error(),
    ],
)
async def test_terminal_credential_failure_is_defect_before_a_send_and_uncertain_after_one(
    operation: str, error: Exception
) -> None:
    credentials = SwitchingCredentialProvider()
    credentials.error = error
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run(operation, lambda _: json_response(500, {}), context, requests, credentials)
    assert isinstance(result, sdk.MutationResult) and result.branch == "defect"
    assert not requests
    assert context.marker is not None and context.marker.state == "notSent"

    credentials.error = None
    context = AttemptContext()
    first = await run(operation, lambda _: json_response(502, {}), context, requests, credentials)
    assert isinstance(first, sdk.RetryError)
    context.next_attempt()
    credentials.error = error
    result = await run(operation, lambda _: json_response(500, {}), context, requests, credentials)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert methods(requests) == ["POST"]
    assert context.heartbeat_count == 0, "no notSent is recorded after a possible send"


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["createConversation", "replyToConversation"])
@pytest.mark.parametrize(
    "error",
    [
        projectconfig.CredentialBrokerUnavailableError("broker"),
        projectconfig.RelayTokenRenewalDeferredError(),
        ValueError("connection is not configured"),
        RuntimeError("unlisted"),
    ],
)
async def test_transient_credential_failure_retries_before_and_after_a_possible_send(
    operation: str, error: Exception
) -> None:
    credentials = SwitchingCredentialProvider()
    credentials.error = error
    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run(operation, lambda _: json_response(500, {}), context, requests, credentials)
    assert isinstance(result, sdk.RetryError)
    assert not requests

    credentials.error = None
    context = AttemptContext()
    first = await run(operation, lambda _: json_response(502, {}), context, requests, credentials)
    assert isinstance(first, sdk.RetryError)
    context.next_attempt()
    credentials.error = error
    result = await run(operation, lambda _: json_response(500, {}), context, requests, credentials)
    assert isinstance(result, sdk.RetryError)
    assert methods(requests) == ["POST"]


@pytest.mark.asyncio
async def test_accepted_create_with_transient_read_back_and_no_slug_is_uncertain() -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return json_response(201, {"subject": "accepted"})
        return json_response(503, {})

    requests: list[httpx.Request] = []
    result = await run("createConversation", respond, AttemptContext(), requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert methods(requests) == ["POST", "GET"]


@pytest.mark.asyncio
async def test_accepted_undecodable_create_reads_back_by_its_saved_slug() -> None:
    key = ""
    reads = 0

    def respond(request: httpx.Request) -> httpx.Response:
        nonlocal key, reads
        if request.method == "POST":
            key = json.loads(request.content)["conversation"]["data"]["dex_dispatch_key"]
            return json_response(201, {"slug": "double-charge", "status": "open"})
        reads += 1
        assert request.url.path == "/api/v1/conversations/double-charge"
        if reads == 1:
            return json_response(429, {})
        return json_response(200, conversation_wire(key))

    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run("createConversation", respond, context, requests)
    assert isinstance(result, sdk.RetryError), "a saved slug makes the next attempt read back, not resend"
    assert context.marker is not None and context.marker.provider_id == "double-charge"
    context.next_attempt()
    result = await run("createConversation", respond, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "created"
    assert result.value.conversation.id == "double-charge"
    assert methods(requests) == ["POST", "GET", "GET"]


@pytest.mark.asyncio
async def test_read_back_refusal_after_a_possible_send_is_uncertain() -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return json_response(502, {})
        return json_response(404, {})

    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await run("replyToConversation", respond, context, requests), sdk.RetryError)
    context.next_attempt()
    result = await run("replyToConversation", respond, context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert methods(requests) == ["POST", "GET"]


@pytest.mark.asyncio
@pytest.mark.parametrize("body", [b'{"slug":"double-charge"}', b'{"slug":"' + b"x" * 4096 + b'"}'])
async def test_update_retries_an_unusable_read_before_the_change(body: bytes) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, content=body)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = reamaze.Client(
            reamaze.Config("acme", max_response_bytes=1024),
            SwitchingCredentialProvider(),
            transport,
            "http://127.0.0.1:12345/api/v1",
        )
        call = sdk.Call(CALL_ID, CONNECTION, sdk.OperationRef("reamaze", "updateConversation"))
        update = reamaze.UpdateConversationInput(conversation_id="double-charge", status=2)
        with pytest.raises(sdk.RetryError):
            await sdk.run_mutation(client.update_conversation(), call, update)
    assert len(requests) == 1, "an earlier attempt's PUT may have applied, so the safe write repeats"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "status, body, branch",
    [
        (404, b"{}", "notFound"),
        (403, b"{}", "providerRejected"),
        (400, b'{"errors":"' + b"x" * 4096 + b'"}', ""),
        (301, b"{}", ""),
        (409, b"{}", ""),
        (500, b"{}", ""),
        (400, b'{"errors":"reamazeTestToken0123456789"}', ""),
    ],
)
async def test_update_first_read_selects_a_branch_only_for_a_definite_refusal(
    status: int, body: bytes, branch: str
) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, content=body)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = reamaze.Client(
            reamaze.Config("acme", max_response_bytes=1024),
            SwitchingCredentialProvider(),
            transport,
            "http://127.0.0.1:12345/api/v1",
        )
        call = sdk.Call(CALL_ID, CONNECTION, sdk.OperationRef("reamaze", "updateConversation"))
        update = reamaze.UpdateConversationInput(conversation_id="double-charge", status=2)
        if not branch:
            with pytest.raises(sdk.RetryError):
                await sdk.run_mutation(client.update_conversation(), call, update)
        else:
            result = await sdk.run_mutation(client.update_conversation(), call, update)
            assert result.branch == branch
    assert methods(requests) == ["GET"], "nothing is written after an unusable first read"


@pytest.mark.asyncio
async def test_update_terminal_credential_failure_selects_defect_only_on_the_first_attempt() -> None:
    credentials = SwitchingCredentialProvider()
    credentials.error = sdk.ReauthorizationRequiredError("reconnect")
    context = AttemptContext()
    mock = httpx.MockTransport(lambda _: json_response(500, {}))
    async with httpx.AsyncClient(transport=mock) as transport:
        base_url = "http://127.0.0.1:12345/api/v1"
        client = reamaze.Client(reamaze.Config("acme"), credentials, transport, base_url)
        operation = sdk.OperationRef("reamaze", "updateConversation")
        call = sdk.Call(CALL_ID, CONNECTION, operation, context=context)
        update = reamaze.UpdateConversationInput(conversation_id="double-charge", status=2)
        result = await sdk.run_mutation(client.update_conversation(), call, update)
        assert result.branch == "defect"
        context.next_attempt()
        with pytest.raises(sdk.RetryError):
            # An earlier attempt's PUT may have applied, so the safe write repeats.
            await sdk.run_mutation(client.update_conversation(), call, update)


@pytest.mark.asyncio
async def test_refusal_whose_body_is_too_large_to_read_retries() -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        return json_response(422, {"errors": "x" * 4096})

    requests: list[httpx.Request] = []
    context = AttemptContext()
    result = await run("createConversation", respond, context, requests, max_response_bytes=1024)
    assert isinstance(result, sdk.RetryError), "an unread refusal body may carry an acceptance"
    assert context.marker is not None and context.marker.state == "claimed"
