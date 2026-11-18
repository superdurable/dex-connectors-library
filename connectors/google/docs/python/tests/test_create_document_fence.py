# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""createDocument resend and read-back behavior that the Go-recorded scenarios do not cover."""

import asyncio
import json
import sys
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.providerhttp import MAX_ERROR_BODY_BYTES

import dex_connectors.google_docs as docs
from dex_connectors.google_docs._generated.connection import CredentialMaterialError, decode_credentials

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import HeartbeatContext  # noqa: E402

CONNECTION = sdk.ConnectionRef("google", "docs")
CALL_ID = "4b0d5a43-7e43-5c5b-9a1e-2f4f1d1f0c11"
EMPTY_LOOKUP = {"files": []}
DOCUMENT = {"id": "newDoc1", "name": "Acme Refund Policy", "mimeType": "application/vnd.google-apps.document"}
INPUT = docs.CreateDocumentInput("Acme Refund Policy")


class StaticCredentials:
    """Resolve one token until is_unavailable is set."""

    def __init__(self) -> None:
        self.is_unavailable = False

    async def resolve(self, call: sdk.Call) -> docs.Credentials:
        if self.is_unavailable:
            raise RuntimeError("credential store unavailable")
        return docs.Credentials(access_token=sdk.SecretString("docs-token"))


class FailingCredentials:
    """Fail every credential resolution with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> docs.Credentials:
        raise self.error


class BrokenBodyStream(httpx.AsyncByteStream):
    """Deliver part of a body after 2xx headers, then lose the connection."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":"newD'
        raise httpx.ReadError("connection reset")


def call_for(context: HeartbeatContext, max_sends: int = 3) -> sdk.Call:
    return sdk.Call(
        sdk.CallID(CALL_ID), CONNECTION, sdk.OperationRef("google-docs", "createDocument"),
        sdk.IdempotencyKey(CALL_ID), context=context, max_sends=max_sends,
    )


async def run_attempts(
    answer: Callable[[httpx.Request], httpx.Response],
    attempts: int,
    credentials: Any = None,
    context: HeartbeatContext | None = None,
    max_sends: int = 3,
    first_attempt: int = 1,
) -> tuple[list[sdk.MutationAttempt[docs.CreateDocumentOutput]], HeartbeatContext]:
    context = context or HeartbeatContext(call_id=CALL_ID)
    results = []
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        config = docs.Config(docs_endpoint="http://127.0.0.1", drive_endpoint="http://127.0.0.1")
        client = docs.Client(config, credentials or StaticCredentials(), http_client)
        for attempt in range(first_attempt, first_attempt + attempts):
            context.attempt = attempt
            results.append(await client.create_document().invoke(call_for(context, max_sends), INPUT))
    return results, context


def is_lookup(request: httpx.Request) -> bool:
    return request.method == "GET"


async def test_unusable_accepted_body_is_looked_up_until_the_cap_and_never_resent() -> None:
    creates = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal creates
        if is_lookup(request):
            return httpx.Response(200, json=EMPTY_LOOKUP)
        creates += 1
        return httpx.Response(200, json={"name": "Missing ID"})

    results, _ = await run_attempts(answer, 4)
    assert [result.kind for result in results] == ["retry", "retry", "retry", "uncertain"]
    assert creates == 1


async def test_accepted_create_without_an_id_is_found_by_a_later_lookup() -> None:
    """The first lookup after acceptance fails transiently, so the next attempt looks again."""
    creates = 0
    lookups = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal creates, lookups
        if is_lookup(request):
            lookups += 1
            if lookups == 2:
                return httpx.Response(503, json={})
            return httpx.Response(200, json={"files": [DOCUMENT]} if lookups >= 3 else EMPTY_LOOKUP)
        creates += 1
        return httpx.Response(200, json={"name": "Missing ID"})

    results, _ = await run_attempts(answer, 2)
    assert [result.kind for result in results] == ["retry", "branch"]
    assert results[1].branch == "created"
    assert creates == 1


async def test_refusal_carrying_a_document_id_is_read_back_and_never_resent() -> None:
    creates = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal creates
        if request.method == "POST":
            creates += 1
            return httpx.Response(400, json={"id": "newDoc1", "mimeType": 7})
        if request.url.path.endswith("/files/newDoc1"):
            return httpx.Response(200, json=DOCUMENT)
        return httpx.Response(200, json=EMPTY_LOOKUP)

    results, _ = await run_attempts(answer, 2)
    assert [result.kind for result in results] == ["retry", "branch"]
    assert results[1].branch == "created"
    assert creates == 1


async def test_oversized_refusal_body_is_an_unknown_outcome() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        if is_lookup(request):
            return httpx.Response(200, json=EMPTY_LOOKUP)
        return httpx.Response(400, content=b"x" * (MAX_ERROR_BODY_BYTES + 1))

    results, _ = await run_attempts(answer, 1)
    assert results[0].kind == "retry"


class StalledBodyStream(httpx.AsyncByteStream):
    """Stall after 2xx headers until the Fence write window ends."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        await asyncio.sleep(5)
        yield b"{}"


async def test_write_deadline_that_cuts_an_accepted_body_is_never_resent() -> None:
    """2xx headers arrived, so the Fence write-window timeout proves the create."""
    creates = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal creates
        if is_lookup(request):
            return httpx.Response(200, json=EMPTY_LOOKUP)
        creates += 1
        return httpx.Response(200, stream=StalledBodyStream())

    context = HeartbeatContext(call_id=CALL_ID)
    context.deadline = datetime.now(UTC) + timedelta(seconds=1.2)
    first, _ = await run_attempts(answer, 1, context=context)
    assert first[0].kind == "retry"
    context.deadline = datetime.now(UTC) + timedelta(seconds=30)
    later, _ = await run_attempts(answer, 3, context=context, first_attempt=2)
    assert [result.kind for result in later] == ["retry", "retry", "uncertain"]
    assert creates == 1


async def test_terminal_credential_failure_is_defect_before_a_send_and_uncertain_after() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    revoked = FailingCredentials(sdk.ReauthorizationRequiredError())
    first, _ = await run_attempts(answer, 1, revoked)
    assert first[0].kind == "branch" and first[0].branch == "defect"
    later, _ = await run_attempts(answer, 1, revoked, first_attempt=2)
    assert later[0].kind == "uncertain"


def sdk_wrapped_material_error() -> ValueError:
    """Build the ValueError the SDK project provider raises after the generated decoder refuses material."""
    try:
        try:
            decode_credentials({"unknown": "value"})
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("malformed material decoded")


@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (sdk_wrapped_material_error(), True),
        (sdk.ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialBrokerUnavailableError(), False),
        (ValueError("connection is not configured"), False),
        (RuntimeError("unlisted"), False),
    ],
)
async def test_credential_resolve_failures_follow_one_classification(
    error: Exception, is_terminal: bool
) -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            raise AssertionError("no create is expected")
        return httpx.Response(200, json=EMPTY_LOOKUP)

    credentials = FailingCredentials(error)
    first, _ = await run_attempts(answer, 1, credentials)
    later, _ = await run_attempts(answer, 1, credentials, first_attempt=2)
    if is_terminal:
        assert first[0].kind == "branch" and first[0].branch == "defect"
        assert later[0].kind == "uncertain"
    else:
        assert first[0].kind == "retry" and later[0].kind == "retry"


async def test_accepted_body_with_an_id_is_read_back_by_id() -> None:
    creates = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal creates
        if request.method == "POST":
            creates += 1
            return httpx.Response(200, json={"id": "newDoc1", "mimeType": 7})
        if request.url.path.endswith("/files/newDoc1"):
            return httpx.Response(200, json=DOCUMENT)
        return httpx.Response(200, json=EMPTY_LOOKUP)

    results, _ = await run_attempts(answer, 2)
    assert [result.kind for result in results] == ["retry", "branch"]
    assert results[1].branch == "created"
    assert creates == 1


async def test_body_that_breaks_after_2xx_headers_is_never_resent() -> None:
    creates = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal creates
        if is_lookup(request):
            return httpx.Response(200, json=EMPTY_LOOKUP)
        creates += 1
        return httpx.Response(200, stream=BrokenBodyStream())

    results, _ = await run_attempts(answer, 4)
    assert [result.kind for result in results] == ["retry", "retry", "retry", "uncertain"]
    assert creates == 1


async def test_rate_limited_resend_still_looks_up_on_the_next_attempt() -> None:
    methods: list[str] = []
    statuses = [500, 429, 500]

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if is_lookup(request):
            return httpx.Response(200, json=EMPTY_LOOKUP)
        return httpx.Response(statuses[methods.count("POST") - 1], json={})

    results, _ = await run_attempts(answer, 3)
    assert [result.kind for result in results] == ["retry", "retry", "retry"]
    assert methods == ["GET", "POST", "GET", "GET", "POST", "GET", "GET", "POST", "GET"]


async def test_rejected_lookup_after_possible_send_retries_below_the_cap_and_is_uncertain_at_it() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        assert is_lookup(request)
        return httpx.Response(403, json={"error": {"errors": [{"reason": "insufficientPermissions"}]}})

    below, _ = await run_attempts(answer, 1, first_attempt=2)
    assert below[0].kind == "retry"
    at_cap, _ = await run_attempts(answer, 1, first_attempt=2, max_sends=1)
    assert at_cap[0].kind == "uncertain"


async def test_incomplete_lookup_at_the_cap_retries() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        assert is_lookup(request)
        return httpx.Response(200, json={"incompleteSearch": True, "files": []})

    results, _ = await run_attempts(answer, 1, first_attempt=2, max_sends=1)
    assert results[0].kind == "retry"


class RejectedRefreshCredentials:
    """Resolve a token Google rejects; the refresh after the rejection raises the given error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> docs.Credentials:
        return docs.Credentials(access_token=sdk.SecretString("docs-token"))

    async def resolve_after_rejection(self, call: sdk.Call) -> docs.Credentials:
        raise self.error


async def test_refresh_failure_after_401_retries_unless_reauthorization_is_required() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        if is_lookup(request):
            return httpx.Response(200, json=EMPTY_LOOKUP)
        return httpx.Response(401, json={})

    unavailable = RejectedRefreshCredentials(RuntimeError("unavailable"))
    transient, context = await run_attempts(answer, 1, unavailable)
    assert transient[0].kind == "retry"
    assert "canResend" in json.dumps(context.heartbeat_payload, default=str)
    revoked = RejectedRefreshCredentials(sdk.ReauthorizationRequiredError())
    terminal, _ = await run_attempts(answer, 1, revoked)
    assert terminal[0].kind == "branch" and terminal[0].branch == "providerRejected"


async def test_guarded_write_retries_a_transient_credential_failure() -> None:
    credentials = StaticCredentials()
    credentials.is_unavailable = True
    append = docs.AppendTextInput("draftDoc1", "rev-1", "Published by Dex.")

    def answer(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        config = docs.Config(docs_endpoint="http://127.0.0.1", drive_endpoint="http://127.0.0.1")
        client = docs.Client(config, credentials, http_client)  # type: ignore[arg-type]
        first = await client.append_text().invoke(call_for(HeartbeatContext(call_id=CALL_ID)), append)
    assert first.kind == "retry"


async def test_guarded_write_retries_missing_credentials_after_a_possible_write() -> None:
    credentials = FailingCredentials(sdk.ReauthorizationRequiredError())
    append = docs.AppendTextInput("draftDoc1", "rev-1", "Published by Dex.")

    def answer(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        config = docs.Config(docs_endpoint="http://127.0.0.1", drive_endpoint="http://127.0.0.1")
        client = docs.Client(config, credentials, http_client)  # type: ignore[arg-type]
        context = HeartbeatContext(call_id=CALL_ID)
        first = await client.append_text().invoke(call_for(context), append)
        context.attempt = 2
        later = await client.append_text().invoke(call_for(context), append)
    assert first.kind == "branch" and first.branch == "defect"
    # A later attempt may follow a write that applied, so defect could hide it.
    assert later.kind == "retry"


async def test_transient_credentials_after_possible_send_retry_without_not_sent() -> None:
    credentials = StaticCredentials()
    credentials.is_unavailable = True

    def answer(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    results, context = await run_attempts(answer, 1, credentials, first_attempt=2)
    assert results[0].kind == "retry"
    # A possible earlier send keeps its count: no notSent marker is written.
    assert not context.has_written_heartbeat
    assert "notSent" not in json.dumps(context.heartbeat_payload, default=str)
