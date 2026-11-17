# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fence behavior of addPlaylistItem that the Go-recorded scenarios cannot express."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import httpx

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
    context = HeartbeatContext()
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
    assert context.heartbeat_payload == {"attempt": 3, "callId": CALL_ID, "sendCount": 3, "state": "claimed"}
