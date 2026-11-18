# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fence behavior of addPlaylistItem that the Go-recorded scenarios cannot express."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.google_youtube as youtube

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import HeartbeatContext  # noqa: E402

CONNECTION = sdk.ConnectionRef("google", "youtube")
CALL_ID = "dbf06fa9-bbc5-529b-9f94-181dc26e11c1"


class ManageRefreshingCredentials:
    """Authorize the manage method and replace the token after one provider rejection."""

    def __init__(self) -> None:
        self.forced_refreshes = 0

    async def resolve(self, call: sdk.Call) -> youtube.Credentials:
        return youtube.Credentials("google-oauth-manage", access_token=sdk.SecretString("rejected-token"))

    async def resolve_after_rejection(self, call: sdk.Call) -> youtube.Credentials:
        self.forced_refreshes += 1
        return youtube.Credentials("google-oauth-manage", access_token=sdk.SecretString("replacement-token"))


async def test_ambiguous_resend_after_refresh_counts_toward_the_send_cap() -> None:
    inserts: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json={"pageInfo": {"totalResults": 0}, "items": []})
        inserts.append(request.headers["Authorization"].removeprefix("Bearer "))
        # The refreshed insert may have been applied before this server error.
        return httpx.Response(401 if len(inserts) == 1 else 500)

    credentials = ManageRefreshingCredentials()
    context = HeartbeatContext(call_id=CALL_ID)
    input_value = youtube.AddPlaylistItemInput("PLexample123", "dQw4w9WgXcQ")
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        config = youtube.Config(endpoint="http://127.0.0.1/youtube/v3")
        client = youtube.Client(config, credentials, http_client)
        attempts = []
        for attempt in (1, 2, 3):
            context.attempt = attempt
            call = sdk.Call(
                sdk.CallID(CALL_ID), CONNECTION, sdk.OperationRef("google-youtube", "addPlaylistItem"),
                sdk.IdempotencyKey(CALL_ID), context=context, max_sends=3,
            )
            attempts.append(await client.add_playlist_item().invoke(call, input_value))
    assert [attempt.kind for attempt in attempts] == ["retry", "retry", "uncertain"]
    # The refreshed resend shares one fence claim with the rejected insert.
    assert inserts == ["rejected-token", "replacement-token", "rejected-token", "rejected-token"]
    assert credentials.forced_refreshes == 1
    marker = context.get_last_heartbeat_value(Marker)
    assert isinstance(marker, Marker)
    assert marker.to_wire() == {"attempt": 3, "callId": CALL_ID, "sendCount": 3, "state": "claimed"}


class StaticManageCredentials:
    """Authorize the manage method until is_unavailable is set."""

    def __init__(self) -> None:
        self.is_unavailable = False

    async def resolve(self, call: sdk.Call) -> youtube.Credentials:
        if self.is_unavailable:
            raise RuntimeError("credential store unavailable")
        return youtube.Credentials("google-oauth-manage", access_token=sdk.SecretString("youtube-token"))


class BrokenBodyStream(httpx.AsyncByteStream):
    """Deliver part of a body after 2xx headers, then lose the connection."""

    async def __aiter__(self):  # type: ignore[no-untyped-def]
        yield b'{"id":"UExp'
        raise httpx.ReadError("connection reset")


EMPTY_LISTING = {"pageInfo": {"totalResults": 0}, "items": []}


async def run_attempts(
    answer: object, attempts: int, credentials: object | None = None, context: HeartbeatContext | None = None
) -> tuple[list[sdk.MutationAttempt[youtube.AddedPlaylistItem]], HeartbeatContext]:
    context = context or HeartbeatContext(call_id=CALL_ID)
    # A context restored from a prior heartbeat starts at the attempt after it.
    first_attempt = context.attempt
    input_value = youtube.AddPlaylistItemInput("PLexample123", "dQw4w9WgXcQ")
    results = []
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:  # type: ignore[arg-type]
        config = youtube.Config(endpoint="http://127.0.0.1/youtube/v3")
        client = youtube.Client(config, credentials or StaticManageCredentials(), http_client)  # type: ignore[arg-type]
        for attempt in range(first_attempt, first_attempt + attempts):
            context.attempt = attempt
            call = sdk.Call(
                sdk.CallID(CALL_ID), CONNECTION, sdk.OperationRef("google-youtube", "addPlaylistItem"),
                sdk.IdempotencyKey(CALL_ID), context=context, max_sends=3,
            )
            results.append(await client.add_playlist_item().invoke(call, input_value))
    return results, context


async def test_rate_limited_resend_still_reads_back_on_the_next_attempt() -> None:
    methods: list[str] = []
    statuses = [500, 429, 500]

    def answer(request: httpx.Request) -> httpx.Response:
        methods.append(request.method)
        if request.method == "GET":
            return httpx.Response(200, json=EMPTY_LISTING)
        return httpx.Response(statuses[methods.count("POST") - 1])

    results, _ = await run_attempts(answer, 3)
    assert [result.kind for result in results] == ["retry", "retry", "retry"]
    # Attempt 3 follows a canResend marker yet still lists the playlist before inserting.
    assert methods == ["GET", "POST", "GET", "POST", "GET", "POST"]


async def test_service_unavailable_resends_without_counting() -> None:
    inserts = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal inserts
        if request.method == "GET":
            return httpx.Response(200, json=EMPTY_LISTING)
        inserts += 1
        return httpx.Response(503)

    results, context = await run_attempts(answer, 4)
    assert [result.kind for result in results] == ["retry"] * 4
    assert inserts == 4
    marker = context.get_last_heartbeat_value(Marker)
    assert isinstance(marker, Marker)
    assert marker.state == "canResend"


async def test_body_that_breaks_after_2xx_headers_is_never_resent() -> None:
    inserts = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal inserts
        if request.method == "GET":
            return httpx.Response(200, json=EMPTY_LISTING)
        inserts += 1
        return httpx.Response(200, stream=BrokenBodyStream())

    results, _ = await run_attempts(answer, 1)
    assert results[0].kind == "uncertain"
    assert inserts == 1


async def test_transient_credential_failure_after_possible_insert_retries() -> None:
    inserts = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal inserts
        if request.method == "GET":
            return httpx.Response(200, json=EMPTY_LISTING)
        inserts += 1
        return httpx.Response(500)

    credentials = StaticManageCredentials()
    first, context = await run_attempts(answer, 1, credentials)
    assert first[0].kind == "retry"
    credentials.is_unavailable = True
    context.attempt = 2
    input_value = youtube.AddPlaylistItemInput("PLexample123", "dQw4w9WgXcQ")
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        config = youtube.Config(endpoint="http://127.0.0.1/youtube/v3")
        client = youtube.Client(config, credentials, http_client)  # type: ignore[arg-type]
        call = sdk.Call(
            sdk.CallID(CALL_ID), CONNECTION, sdk.OperationRef("google-youtube", "addPlaylistItem"),
            sdk.IdempotencyKey(CALL_ID), context=context, max_sends=3,
        )
        second = await client.add_playlist_item().invoke(call, input_value)
    assert second.kind == "retry"
    assert inserts == 1


class UnavailableRefreshCredentials:
    """Authorize the manage method; the refresh after a rejection fails transiently."""

    async def resolve(self, call: sdk.Call) -> youtube.Credentials:
        return youtube.Credentials("google-oauth-manage", access_token=sdk.SecretString("youtube-token"))

    async def resolve_after_rejection(self, call: sdk.Call) -> youtube.Credentials:
        raise RuntimeError("token endpoint unavailable")


async def test_transient_refresh_failure_after_a_401_resend_retries_with_can_resend() -> None:
    inserts = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal inserts
        if request.method == "GET":
            return httpx.Response(200, json=EMPTY_LISTING)
        inserts += 1
        return httpx.Response(500 if inserts == 1 else 401)

    results, context = await run_attempts(answer, 2, UnavailableRefreshCredentials())
    assert [result.kind for result in results] == ["retry", "retry"]
    assert inserts == 2
    marker = context.get_last_heartbeat_value(Marker)
    assert isinstance(marker, Marker)
    assert marker.state == "canResend"


async def test_saved_item_lagging_read_back_retries_below_the_cap() -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        return httpx.Response(200, json=EMPTY_LISTING)

    prior = {"attempt": 1, "callId": CALL_ID, "providerId": "PLITEM001", "sendCount": 1, "state": "claimed"}
    context = HeartbeatContext(prior_heartbeat=prior, call_id=CALL_ID)
    results, _ = await run_attempts(answer, 1, context=context)
    assert results[0].kind == "retry"


async def test_failed_listing_below_the_cap_retries() -> None:
    gets = 0

    def answer(request: httpx.Request) -> httpx.Response:
        nonlocal gets
        if request.method == "GET":
            gets += 1
            if gets == 1:
                return httpx.Response(200, json=EMPTY_LISTING)
            return httpx.Response(403, json={"error": {"errors": [{"reason": "quotaExceeded"}]}})
        return httpx.Response(500)

    results, _ = await run_attempts(answer, 2)
    assert [result.kind for result in results] == ["retry", "retry"]
