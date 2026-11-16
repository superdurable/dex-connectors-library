# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go profileChanged fetcher's recorded outputs and require identical cursors and events.

The fixture was recorded from the Go connector's profileFetcher: each step serves one raw UserInfo
response, and each step's request, cursor token, event IDs, payloads, and poll signal must match Go
byte for byte, so Go and Python replicas share one poll cursor document and Trigger inbox.
"""

import base64
import json
from datetime import datetime
from pathlib import Path
from typing import Any

import httpx
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import encode_go_json_value

from dex_connectors.linkedin import PROFILE_CHANGED_TRIGGER_DEFINITION, Client, Config, Credentials
from dex_connectors.linkedin._profile_change import profile_claims_from_user_info, profile_hash
from dex_connectors.linkedin._profile_cursor import encode_profile_claims
from dex_connectors.linkedin._profile_fetcher import ProfileFetcher
from dex_connectors.linkedin.client import _decode_user_info

FIXTURE = json.loads(
    (Path(__file__).parent / "fixtures" / "profile-changed-go-fetcher-cases.json").read_text()
)
REFERENCE = ConnectionRef("linkedin", "linkedin-watcher")
ACCESS_TOKEN = "linkedin-" + "profile-" + "trigger"


class RecordedUserInfo:
    """Answers each request with the current step's recorded response and records the request."""

    def __init__(self) -> None:
        self.step: dict[str, Any] = {}
        self.requests: list[dict[str, str]] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(
            {
                "Method": request.method,
                "Path": request.url.path,
                "Authorization": request.headers.get("Authorization", "").replace(
                    ACCESS_TOKEN, "${ACCESS_TOKEN}"
                ),
                "Accept": request.headers.get("Accept", ""),
                "UserAgent": request.headers.get("User-Agent", ""),
            }
        )
        headers = {"X-LI-UUID": "fake-request"}
        if self.step.get("retryAfter"):
            headers["Retry-After"] = self.step["retryAfter"]
        return httpx.Response(self.step["status"], headers=headers, content=self.step["body"].encode())


async def _run_step(fetcher: ProfileFetcher, step: dict[str, Any], cursor: str) -> dict[str, Any]:
    context = TriggerContext(TriggerCancellation())
    result: dict[str, Any] = {}
    try:
        if step["operation"] == "seed":
            result["next"] = (await fetcher.seed_cursor(context)).token
        else:
            result["input"] = cursor
            page = await fetcher.fetch_changes(context, Cursor(cursor))
            result["next"] = page.next_cursor.token
            result["events"] = [
                {
                    "id": event.id,
                    "occurredAt": event.occurred_at.isoformat().replace("+00:00", "Z"),
                    "payload": json.loads(encode_go_json_value(event.payload)),
                }
                for event in page.changes
            ]
    except RateLimitedError as error:
        result["error"] = {
            "kind": "rateLimited",
            "message": str(error.__cause__),
            "retryAfterSeconds": error.retry_after.total_seconds(),
        }
    except Exception as error:
        if is_cursor_invalid(error):
            result["error"] = {"kind": "cursorInvalid"}
        else:
            result["error"] = {"kind": "failed", "message": str(error)}
    return result


async def test_python_fetcher_replays_the_go_recording_byte_for_byte() -> None:
    provider = RecordedUserInfo()
    observed_at = datetime.fromisoformat(FIXTURE["observedAt"])
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        empty = SecretString("")
        credentials = Credentials("", empty, SecretString(ACCESS_TOKEN), empty)
        client = Client(
            Config(user_info_url="https://linkedin.example/v2/userinfo", poll_interval_seconds=60),
            StaticCredentialProvider({REFERENCE: credentials}),
            http_client,
            now=lambda: observed_at,
        )
        fetcher = ProfileFetcher(client, REFERENCE, PROFILE_CHANGED_TRIGGER_DEFINITION.trigger)
        committed: list[str] = []
        last = ""
        for recorded in FIXTURE["steps"]:
            step, expected = recorded["step"], recorded["result"]
            provider.step, provider.requests = step, []
            cursor = last
            if "cursorFromStep" in step:
                cursor = committed[step["cursorFromStep"]]
            cursor = step.get("cursor") or cursor
            actual = await _run_step(fetcher, step, cursor)
            expected_requests = expected.pop("requests") or []
            assert provider.requests == expected_requests, step["name"]
            if "error" in expected and expected["error"]["kind"] == "rateLimited":
                expected["error"]["retryAfterSeconds"] = float(expected["error"]["retryAfterSeconds"])
            for event in expected.get("events", []):
                event["payload"] = json.loads(event["payload"])
            assert actual == expected, step["name"]
            last = actual.get("next", last)
            committed.append(last)


CLAIMS_FIXTURE = json.loads((Path(__file__).parent / "fixtures" / "profile-claims-go-cases.json").read_text())


def test_python_claims_decoding_matches_the_go_recording_byte_for_byte() -> None:
    for index, recorded in enumerate(CLAIMS_FIXTURE["claims"]):
        body = base64.b64decode(recorded["body"])
        try:
            claims = _decode_user_info(body)
        except ValueError:
            assert recorded["outcome"] == "invalid", (index, body)
            continue
        profile = profile_claims_from_user_info(claims)
        if profile is None:
            assert recorded["outcome"] == "noSubject", (index, body)
            continue
        assert (recorded["outcome"], recorded["profile"], recorded["hash"]) == (
            "profile",
            encode_profile_claims(profile),
            profile_hash(profile),
        ), (index, body)
