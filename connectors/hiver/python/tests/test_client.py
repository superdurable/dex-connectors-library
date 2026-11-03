# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Hiver transport status ordering and strict provider decoding."""

import json

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.hiver import (
    Client,
    Config,
    Credentials,
    GetConversationInput,
    ListConversationsInput,
    UpdateConversationInput,
)
from dex_connectors.hiver.client import decode_inbox, decode_note, list_page


@pytest.mark.parametrize(
    ("status", "outcome"), [(429, "rateLimited"), (503, "unavailable"), (200, "invalid")]
)
async def test_error_status_precedes_response_limit(status: int, outcome: str) -> None:
    """Bound only successful bodies; a large 429 or 503 keeps its status outcome."""
    reference = ConnectionRef("hiver", "test")
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(status, content=b"x" * 50))
    ) as transport:
        client = Client(
            Config(max_response_bytes=20, request_interval_milliseconds=1),
            StaticCredentialProvider({reference: Credentials(SecretString("inertKey"))}),
            transport,
            "http://127.0.0.1:12345/v1",
        )
        call = Call(CallID("call-id"), reference, OperationRef("hiver", "listInboxes"))
        exchange = await client.exchange(call, "inertKey", "listInboxes", "GET", "/inboxes")
    assert exchange.outcome == outcome


def test_wrong_typed_provider_fields_are_not_coerced() -> None:
    """Reject a numeric page token and wrong-typed inbox fields as Go does."""
    with pytest.raises(ValueError):
        list_page(json.dumps({"data": {"results": [], "pagination": {"next_page": 0}}}).encode())
    with pytest.raises(ValueError):
        decode_inbox({"id": 101, "display_name": 42})
    assert decode_inbox({"id": 101, "is_authorised": None}).id == "101"


def test_note_submicrosecond_precision_is_rejected_without_rounding() -> None:
    """Leave an accepted note unconfirmed when its timestamp cannot be represented exactly."""
    body = b'{"data":{"id":101,"created_at":"2026-11-03T15:00:00.123456789Z"}}'
    with pytest.raises(ValueError, match="created_at has precision beyond Python microseconds"):
        decode_note(body)


async def test_zero_config_uses_go_defaults() -> None:
    """Treat zero config as an omitted manifest default, matching Go construction."""
    reference = ConnectionRef("hiver", "test")
    client = Client(
        Config(max_response_bytes=0, request_interval_milliseconds=0),
        StaticCredentialProvider({reference: Credentials(SecretString("inertKey"))}),
    )
    assert client.max_response_bytes == 4_194_304
    assert client.request_interval == 1
    await client.aclose()


async def test_failure_receipts_preserve_go_resource_ids() -> None:
    """Keep the inbox or conversation ID on each provider failure receipt."""
    reference = ConnectionRef("hiver", "test")
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(404))) as transport:
        client = Client(
            Config(request_interval_milliseconds=1),
            StaticCredentialProvider({reference: Credentials(SecretString("inertKey"))}),
            transport,
            "http://127.0.0.1:12345/v1",
        )
        outcomes = [
            await client.list_conversations().invoke(
                Call(CallID("list"), reference, OperationRef("hiver", "listConversations")),
                ListConversationsInput("105902"),
            ),
            await client.get_conversation().invoke(
                Call(CallID("get"), reference, OperationRef("hiver", "getConversation")),
                GetConversationInput("105902", "573741352"),
            ),
            await client.update_conversation().invoke(
                Call(CallID("update"), reference, OperationRef("hiver", "updateConversation")),
                UpdateConversationInput("105902", "573741352", status="pending"),
            ),
        ]
    assert [outcome.receipt.provider_object_id for outcome in outcomes] == [
        "105902",
        "573741352",
        "573741352",
    ]
