# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check fenced note and draft decisions that the HTTP replay fixtures cannot express."""

import asyncio
import sys
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk import projectconfig

import dex_connectors.hiver as hiver

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import HeartbeatContext  # noqa: E402

CONNECTION = sdk.ConnectionRef("hiver", "hiver-test")
NOTE_INPUT = hiver.AddNoteInput("105902", "573741352", "Triaged.")
DRAFT_INPUT = hiver.CreateSharedDraftInput(inbox_id="105902", body="Thanks.", hiver_message_id="834466048")
API_KEY = "hiverTestKey0123456789"


class BrokenBody(httpx.AsyncByteStream):
    """Send part of a body, then fail as a connection reset would."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"data":'
        raise httpx.ReadError("connection reset")


class StalledBody(httpx.AsyncByteStream):
    """Send part of a body, then never finish it."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b'{"data":'
        await asyncio.sleep(60)


class RevokedCredentials:
    """Resolve no key, as a revoked connection does."""

    async def resolve(self, _call: sdk.Call) -> hiver.Credentials:
        raise sdk.ReauthorizationRequiredError()


def static_credentials(api_key: str = API_KEY) -> sdk.StaticCredentialProvider[hiver.Credentials]:
    return sdk.StaticCredentialProvider({CONNECTION: hiver.Credentials(sdk.SecretString(api_key))})


class Provider:
    """Answer each request with the next reply and count the requests."""

    def __init__(self, *replies: Callable[[], httpx.Response]) -> None:
        self.replies = list(replies)
        self.request_count = 0

    def answer(self, _request: httpx.Request) -> httpx.Response:
        reply = self.replies[min(self.request_count, len(self.replies) - 1)]
        self.request_count += 1
        return reply()


async def invoke(
    provider: Provider,
    context: HeartbeatContext,
    operation_name: str = "addNote",
    credentials: Any = None,
) -> Any:
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.answer)) as http_client:
        client = hiver.Client(
            hiver.Config(request_interval_milliseconds=1),
            credentials or static_credentials(),
            http_client,
            "http://127.0.0.1:12345/v1",
        )
        call = sdk.Call(
            sdk.CallID("53fa38a4-5e96-56e2-81ac-2b8ddb6e38f2"),
            CONNECTION,
            sdk.OperationRef("hiver", operation_name),
            context=context,
            max_sends=3,
        )
        if operation_name == "addNote":
            return await client.add_note().invoke(call, NOTE_INPUT)
        return await client.create_shared_draft().invoke(call, DRAFT_INPUT)


def new_context() -> HeartbeatContext:
    return HeartbeatContext(call_id="53fa38a4-5e96-56e2-81ac-2b8ddb6e38f2")


def advance(context: HeartbeatContext) -> None:
    context.attempt += 1
    context.has_written_heartbeat = False


def marker_state(context: HeartbeatContext) -> str:
    """Read this call's marker state from a flat marker or a per-call checkpoint map."""
    payload = context.heartbeat_payload
    assert isinstance(payload, dict)
    markers = payload.get("singleDispatchMarkers")
    marker = markers.get(context.call_id, payload) if isinstance(markers, dict) else payload
    return str(marker["state"])


async def test_success_headers_then_a_broken_body_is_uncertain_and_never_resent() -> None:
    """A 2xx proves Hiver accepted the note even when its body cannot be read."""
    provider = Provider(lambda: httpx.Response(201, stream=BrokenBody()))
    context = new_context()
    attempt = await invoke(provider, context)
    assert attempt.kind == "uncertain"
    assert marker_state(context) == "claimed"
    assert provider.request_count == 1


async def test_undecodable_success_without_an_id_is_uncertain() -> None:
    provider = Provider(lambda: httpx.Response(201, content=b'{"data":{}}'))
    attempt = await invoke(provider, new_context(), "createSharedDraft")
    assert attempt.kind == "uncertain"
    assert provider.request_count == 1


async def test_oversized_success_is_uncertain() -> None:
    provider = Provider(lambda: httpx.Response(201, content=b'{"data":{"id":"1"}}' + b" " * 5_000_000))
    attempt = await invoke(provider, new_context())
    assert attempt.kind == "uncertain"


async def test_write_window_timeout_after_success_headers_is_uncertain() -> None:
    """The Fence write window cancels the body read; the 2xx still proves acceptance."""
    provider = Provider(lambda: httpx.Response(201, stream=StalledBody()))
    context = new_context()
    context.deadline = datetime.now(UTC) + timedelta(milliseconds=1200)
    attempt = await invoke(provider, context)
    assert attempt.kind == "uncertain"
    assert marker_state(context) == "claimed"


async def test_write_window_timeout_after_draft_success_headers_is_uncertain() -> None:
    provider = Provider(lambda: httpx.Response(200, stream=StalledBody()))
    context = new_context()
    context.deadline = datetime.now(UTC) + timedelta(milliseconds=1200)
    attempt = await invoke(provider, context, "createSharedDraft")
    assert attempt.kind == "uncertain"
    assert marker_state(context) == "claimed"
    assert provider.request_count == 1


def marker_provider_id(context: HeartbeatContext) -> str:
    """Read this call's saved provider ID from a flat marker or a per-call checkpoint map."""
    payload = context.heartbeat_payload
    assert isinstance(payload, dict)
    markers = payload.get("singleDispatchMarkers")
    marker = markers.get(context.call_id, payload) if isinstance(markers, dict) else payload
    return str(marker.get("providerId", ""))


async def test_refusal_with_an_unreadable_body_resends_below_the_cap_and_is_uncertain_at_the_cap() -> None:
    for status in (400, 404):
        provider = Provider(lambda status=status: httpx.Response(status, stream=BrokenBody()))
        context = new_context()
        for _ in range(2):
            assert (await invoke(provider, context)).kind == "retry"
            assert marker_state(context) == "claimed"
            advance(context)
        attempt = await invoke(provider, context)
        assert attempt.kind == "uncertain"
        assert provider.request_count == 3


async def test_refusal_with_an_oversized_body_resends_below_the_cap() -> None:
    provider = Provider(lambda: httpx.Response(422, content=b'{"Message":"' + b"x" * 5_000_000 + b'"}'))
    context = new_context()
    attempt = await invoke(provider, context, "createSharedDraft")
    assert attempt.kind == "retry"
    assert marker_state(context) == "claimed"


async def test_write_window_timeout_after_refusal_headers_resends_below_the_cap() -> None:
    provider = Provider(lambda: httpx.Response(400, stream=StalledBody()))
    context = new_context()
    context.deadline = datetime.now(UTC) + timedelta(milliseconds=1200)
    attempt = await invoke(provider, context)
    assert attempt.kind == "retry"
    assert marker_state(context) == "claimed"


async def test_refusal_whose_body_carries_the_created_id_is_never_resent() -> None:
    for operation_name, body, provider_id in (
        ("addNote", b'{"data":{"id":"35042650"}}', "35042650"),
        ("createSharedDraft", b'{"data":{"id":573741352,"shared_draft_id":901}}', "901"),
    ):
        provider = Provider(lambda body=body: httpx.Response(400, content=body))
        context = new_context()
        attempt = await invoke(provider, context, operation_name)
        assert attempt.kind == "uncertain"
        assert attempt.receipt.provider_object_id == provider_id
        assert marker_provider_id(context) == provider_id
        advance(context)
        assert (await invoke(provider, context, operation_name)).kind == "uncertain"
        assert provider.request_count == 1


async def test_update_conversation_refusal_with_an_unreadable_body_repeats_the_change() -> None:
    update = hiver.UpdateConversationInput("105902", "573741352", status="open")
    provider = Provider(lambda: httpx.Response(404, stream=BrokenBody()))
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.answer)) as http_client:
        client = hiver.Client(
            hiver.Config(request_interval_milliseconds=1), static_credentials(), http_client,
            "http://127.0.0.1:12345/v1",
        )
        call = sdk.Call(
            sdk.CallID("update"),
            CONNECTION,
            sdk.OperationRef("hiver", "updateConversation"),
            context=new_context(),
        )
        attempt = await client.update_conversation().invoke(call, update)
    assert attempt.kind == "retry"
    assert provider.request_count == 1


async def test_write_window_timeout_before_a_response_resends_below_the_cap() -> None:
    async def never_answers(_request: httpx.Request) -> httpx.Response:
        await asyncio.sleep(60)
        raise AssertionError("unreachable")

    context = new_context()
    context.deadline = datetime.now(UTC) + timedelta(milliseconds=1200)
    async with httpx.AsyncClient(transport=httpx.MockTransport(never_answers)) as http_client:
        client = hiver.Client(
            hiver.Config(request_interval_milliseconds=1), static_credentials(), http_client,
            "http://127.0.0.1:12345/v1",
        )
        call = sdk.Call(
            sdk.CallID("53fa38a4-5e96-56e2-81ac-2b8ddb6e38f2"),
            CONNECTION,
            sdk.OperationRef("hiver", "addNote"),
            context=context,
            max_sends=3,
        )
        attempt = await client.add_note().invoke(call, NOTE_INPUT)
    assert attempt.kind == "retry"
    assert marker_state(context) == "claimed"


async def unknown_then_rate_limited(provider: Provider, context: HeartbeatContext) -> None:
    assert (await invoke(provider, context)).kind == "retry"
    advance(context)
    assert (await invoke(provider, context)).kind == "retry"
    assert marker_state(context) == "canResend"
    advance(context)


async def test_refusal_after_a_can_resend_marker_with_a_prior_send_is_uncertain() -> None:
    """Attempt 3 sees SEND_ALLOWED with a prior send, so a 400 cannot prove nothing was added."""
    provider = Provider(
        lambda: httpx.Response(502), lambda: httpx.Response(429), lambda: httpx.Response(400)
    )
    context = new_context()
    await unknown_then_rate_limited(provider, context)
    attempt = await invoke(provider, context)
    assert attempt.kind == "uncertain"
    assert provider.request_count == 3


async def test_credential_failure_after_a_can_resend_marker_with_a_prior_send() -> None:
    for credentials, expected_kind in (
        (sdk.StaticCredentialProvider({}), "retry"),
        (RevokedCredentials(), "uncertain"),
        (static_credentials("bad key"), "uncertain"),
    ):
        provider = Provider(lambda: httpx.Response(502), lambda: httpx.Response(429))
        context = new_context()
        await unknown_then_rate_limited(provider, context)
        attempt = await invoke(provider, context, credentials=credentials)
        assert attempt.kind == expected_kind
        assert not context.has_written_heartbeat, "notSent would hide the earlier possible send"
        assert provider.request_count == 2


async def test_terminal_credential_failure_on_a_first_send_is_defect() -> None:
    provider = Provider(lambda: httpx.Response(201))
    context = new_context()
    attempt = await invoke(provider, context, "createSharedDraft", RevokedCredentials())
    assert attempt.kind == "branch"
    assert attempt.branch == "defect"
    assert marker_state(context) == "notSent"
    assert provider.request_count == 0


async def test_transient_credential_failure_on_a_first_send_records_not_sent_and_retries() -> None:
    provider = Provider(lambda: httpx.Response(201))
    context = new_context()
    attempt = await invoke(provider, context, credentials=sdk.StaticCredentialProvider({}))
    assert attempt.kind == "retry"
    assert marker_state(context) == "notSent"


async def test_update_conversation_credential_failure_is_defect_only_on_a_terminal_first_attempt() -> None:
    update = hiver.UpdateConversationInput("105902", "573741352", status="open")
    for credentials, attempt_number, expected_kind in (
        (RevokedCredentials(), 1, "branch"),
        (RevokedCredentials(), 2, "retry"),
        (sdk.StaticCredentialProvider({}), 1, "retry"),
    ):
        context = new_context()
        context.attempt = attempt_number
        client = hiver.Client(hiver.Config(request_interval_milliseconds=1), credentials)
        call = sdk.Call(
            sdk.CallID("update"), CONNECTION, sdk.OperationRef("hiver", "updateConversation"), context=context
        )
        attempt = await client.update_conversation().invoke(call, update)
        await client.aclose()
        assert attempt.kind == expected_kind


class FailingCredentials:
    """Fail every resolution with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, _call: sdk.Call) -> hiver.Credentials:
        raise self.error


@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (sdk.ReauthorizationRequiredError(), True),
        (projectconfig.ObjectNotFoundError("connection"), True),
        (projectconfig.ConnectionNotSelectedError("connection"), True),
        (projectconfig.CredentialBrokerRejectedError("broker"), True),
        (projectconfig.CredentialBrokerUnavailableError("broker"), False),
        (ValueError("connection is not configured"), False),
        (KeyError("unlisted"), False),
    ],
)
async def test_a_first_send_classifies_every_credential_failure(error: Exception, is_terminal: bool) -> None:
    provider = Provider(lambda: httpx.Response(201, json={}))
    context = new_context()
    attempt = await invoke(provider, context, credentials=FailingCredentials(error))
    assert provider.request_count == 0
    assert marker_state(context) == "notSent"
    if is_terminal:
        assert attempt.kind == "branch" and attempt.branch == "defect"
    else:
        assert attempt.kind == "retry"


@pytest.mark.parametrize(
    ("status", "body", "branch"),
    [
        (404, b"{}", "notFound"),
        (403, b"{}", "providerRejected"),
        (200, b'{"data":"SENTINEL"}', ""),
        (409, b"{}", ""),
        (500, b"{}", ""),
        (301, b"{}", ""),
        (400, b'{"message":"' + API_KEY.encode() + b'"}', ""),
    ],
)
async def test_a_pre_write_lookup_selects_a_branch_only_for_a_definite_answer(
    status: int, body: bytes, branch: str
) -> None:
    for update in (
        hiver.UpdateConversationInput("105902", "573741352", assignee_email="phoebe@acme.example.com"),
        hiver.UpdateConversationInput("105902", "573741352", apply_tag_names=("billing",)),
    ):
        provider = Provider(lambda: httpx.Response(status, content=body))
        async with httpx.AsyncClient(transport=httpx.MockTransport(provider.answer)) as http_client:
            client = hiver.Client(
                hiver.Config(request_interval_milliseconds=1), static_credentials(), http_client,
                "http://127.0.0.1:12345/v1",
            )
            call = sdk.Call(
                sdk.CallID("update"), CONNECTION, sdk.OperationRef("hiver", "updateConversation"),
                context=new_context(),
            )
            attempt = await client.update_conversation().invoke(call, update)
        assert provider.request_count == 1
        if not branch:
            assert attempt.kind == "retry", "an earlier attempt's PATCH may have applied"
        else:
            assert attempt.kind == "branch" and attempt.branch == branch
