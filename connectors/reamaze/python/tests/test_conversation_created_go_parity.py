# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go conversationCreated fetcher's recorded cases and require identical bytes.

The fixture was recorded from the Go connector's conversationCreatedFetcher against its reamazefake. Each
case replays the same provider actions against tests/reamaze_conversations_fake.py, whose response bodies
must hash the same, and each step's requests, cursor token, outcome, event IDs, occurrence times, and
Trigger inbox payload bytes must match Go's, so Go and Python replicas share one poll cursor document and
one Trigger inbox.
"""

import hashlib
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, CursorInvalidError, RateLimitedError
from dex_connectors.sdk.projectconfig import encode_go_json_value

from dex_connectors.reamaze import (
    Client,
    Config,
    ConversationCreatedFetcher,
    ConversationCreatedTriggerConfiguration,
    Credentials,
)
from tests.reamaze_conversations_fake import ReamazeConversationFake

FIXTURE = json.loads(
    (Path(__file__).parent / "fixtures" / "conversation-created-go-fetcher-cases.json").read_text()
)
EMAIL = "agent@acme.example.com"
TOKEN = "reamazePoll" + "Token0123456789"  # Split so push protection does not read a fixture as a token.
OTHER_TOKEN = "otherToken" + "0123456789"
REFERENCE = ConnectionRef("reamaze", "reamaze-poll")


def _sha256(value: bytes | str) -> str:
    return hashlib.sha256(value.encode() if isinstance(value, str) else value).hexdigest()


def _go_rfc3339_nano(moment: datetime) -> str:
    text = moment.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.%f").rstrip("0").rstrip(".")
    return text + "Z"


def _sorted_query(query: str) -> list[tuple[str, str]]:
    return sorted(parse_qsl(query, keep_blank_values=True))


def _parse_time(text: str) -> datetime:
    return datetime.fromisoformat(text.replace("Z", "+00:00"))


def _apply(provider: ReamazeConversationFake, action: dict[str, Any]) -> None:
    kind = action["action"]
    if kind == "insert":
        provider.insert(
            action["slug"],
            action.get("subject", ""),
            channel=action.get("channel", ""),
            status=action.get("status", 0),
            created_at=_parse_time(action["createdAt"]),
            body=action.get("body", ""),
        )
    elif kind == "insertRange":
        start, spacing = _parse_time(action["createdAt"]), timedelta(milliseconds=action["spacingMillis"])
        prefix = action["prefix"]
        for index in range(action["count"]):
            provider.insert(f"{prefix}-{index}", f"{prefix} {index}", created_at=start + index * spacing)
    elif kind == "hide":
        provider.hide(action["slug"])
    elif kind == "hideRange":
        for index in range(action["count"]):
            provider.hide(f"{action['prefix']}-{index}")
    elif kind == "reveal":
        provider.reveal(action["slug"])
    elif kind == "rateLimitNextList":
        provider.rate_limit_next_list(action.get("retryAfter", ""))
    elif kind == "listOldestFirst":
        provider.list_oldest_first()
    else:
        raise AssertionError(f"unknown recorded action {kind}")


@pytest.mark.parametrize("case", FIXTURE["cases"], ids=[case["name"] for case in FIXTURE["cases"]])
async def test_conversation_created_fetcher_matches_the_go_recording(case: dict[str, Any]) -> None:
    provider = ReamazeConversationFake(EMAIL, OTHER_TOKEN if case.get("token") == "mismatch" else TOKEN)
    served = provider.transport()
    requests: list[list[tuple[str, str]]] = []
    responses: list[dict[str, Any]] = []

    async def observe(request: httpx.Request) -> httpx.Response:
        response = await served.handle_async_request(request)
        body = await response.aread()
        # Parameter order is not part of the contract; Go sorts them and Python keeps insertion order.
        requests.append(_sorted_query(request.url.query.decode()))
        recorded: dict[str, Any] = {"status": response.status_code, "bodySha256": _sha256(body)}
        if response.headers.get("Retry-After"):
            recorded["retryAfter"] = response.headers["Retry-After"]
        responses.append(recorded)
        return httpx.Response(response.status_code, headers=response.headers, content=body)

    credentials = Credentials(EMAIL, SecretString(TOKEN))
    async with httpx.AsyncClient(transport=httpx.MockTransport(observe)) as http_client:
        client = Client(
            Config(brand="acme", max_response_bytes=4194304, poll_interval_seconds=60),
            StaticCredentialProvider({REFERENCE: credentials}),
            http_client,
            api_base_url=provider.base_url,
        )
        fetcher = ConversationCreatedFetcher(
            client,
            REFERENCE,
            "new-conversations",
            ConversationCreatedTriggerConfiguration(channel=case.get("channel", "")),
        )
        context = TriggerContext(TriggerCancellation())
        outputs: list[str] = []
        for index, step in enumerate(case["steps"]):
            name = f"{case['name']} step {index}"
            for action in step.get("actions", []):
                _apply(provider, action)
            requests.clear()
            responses.clear()
            error, output, changes = "", "", []
            try:
                if step["kind"] == "seed":
                    output = (await fetcher.seed_cursor(context)).token
                else:
                    token = step["cursorIn"] if "cursorInStep" not in step else outputs[step["cursorInStep"]]
                    page = await fetcher.fetch_changes(context, Cursor(token=token))
                    assert not page.has_more_pages, name
                    output, changes = page.next_cursor.token, list(page.changes)
            except CursorInvalidError:
                error = "cursorInvalid"
            except RateLimitedError as rate_limited:
                error = "rateLimited"
                retry_after = rate_limited.retry_after
                assert retry_after is not None
                assert retry_after // timedelta(milliseconds=1) == step["retryAfterMillis"], name
            except Exception:  # noqa: BLE001 - Go records every other failure the same way.
                error = "failed"
            outputs.append(output)
            assert error == step.get("error", ""), name
            assert requests == [_sorted_query(query) for query in step["requests"]], name
            assert responses == step["responses"], name
            if "cursorOutSha256" in step:
                assert _sha256(output) == step["cursorOutSha256"], name
            else:
                assert output == step.get("cursorOut", ""), name
            assert len(changes) == step.get("changeCount", len(step.get("changes", []))), name
            if "changeIdsSha256" in step:
                assert _sha256("\n".join(change.id for change in changes)) == step["changeIdsSha256"], name
            elif "changeIds" in step:
                assert [change.id for change in changes] == step["changeIds"], name
            else:
                recorded_changes = step.get("changes", [])
                assert len(changes) == len(recorded_changes), name
                for change, expected in zip(changes, recorded_changes, strict=True):
                    assert change.id == expected["id"], name
                    assert _go_rfc3339_nano(change.occurred_at) == expected["occurredAt"], name
                    # The inbox stores a payload with sorted keys, where Go keeps struct order, so the
                    # recording hashes Go's payload re-encoded with sorted keys as well.
                    payload = encode_go_json_value(change.payload)
                    assert _sha256(payload) == expected["payloadSortedSha256"], name
                    if not expected["payload"].startswith("sha256:"):
                        assert json.loads(payload) == json.loads(expected["payload"]), name
