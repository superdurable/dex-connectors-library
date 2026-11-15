# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay a recording of the Go conversationReceived fetcher and match its cursors byte for byte.

The Go connector wrote tests/fixtures/go-conversation-received-cases.json against its hiverfake: tokens,
event IDs, and a SHA-256 over every snapshot entry's key, hash, and seen counter after each commit.
"""

import hashlib
import json
from pathlib import Path

import httpx
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerBindingRef,
    TriggerContext,
)

from dex_connectors.hiver import (
    CONVERSATION_RECEIVED_TRIGGER_DEFINITION,
    Client,
    Config,
    ConversationReceivedTriggerConfiguration,
    Credentials,
)
from dex_connectors.hiver._conversation_received_fetcher import ConversationReceivedFetcher
from tests.hiver_conversations_fake import API_BASE_URL, API_KEY, HiverConversationsFake
from tests.test_conversation_received_fetcher import CursorHarness

CASES = Path(__file__).parent / "fixtures" / "go-conversation-received-cases.json"


async def test_cursor_documents_match_the_go_fetcher_byte_for_byte() -> None:
    recording = json.loads(CASES.read_text())
    inbox_id = recording["inboxId"]
    provider = HiverConversationsFake(inbox_id, "support@acme.example.com")
    first = recording["initialFirstId"]
    provider.add_conversations(inbox_id, *(str(first + index) for index in range(recording["initialCount"])))
    reference = ConnectionRef("hiver", "hiver-support")
    client = Client(
        Config(request_interval_milliseconds=1, poll_pages_per_poll=recording["pollPagesPerPoll"]),
        StaticCredentialProvider({reference: Credentials(SecretString(API_KEY))}),
        httpx.AsyncClient(transport=provider.transport),
        API_BASE_URL,
    )
    binding = TriggerBindingRef(
        reference, CONVERSATION_RECEIVED_TRIGGER_DEFINITION.trigger, "new-conversations"
    )
    fetcher = ConversationReceivedFetcher(
        client, binding, ConversationReceivedTriggerConfiguration(inbox_id=inbox_id)
    )
    harness = CursorHarness(fetcher)
    await harness.seed()
    steps = recording["steps"]
    assert harness.cursor.token == steps[0]["token"]
    for index, step in enumerate(steps[1:], start=1):
        action, ids = step["action"], step.get("ids") or []
        if action == "add":
            provider.add_conversations(inbox_id, *ids)
        elif action == "delete":
            provider.delete_conversation(inbox_id, ids[0])
        elif action == "expire":
            provider.expire_page_tokens()
        elif action == "oldestFirst":
            provider.list_oldest_first()
        else:
            page = await fetcher.fetch_changes(TriggerContext(), harness.cursor)
            harness.commit(page.next_cursor)
            snapshot = harness.cursor.snapshot
            assert snapshot is not None
            lines = [f"{entry.key}:{entry.hash}:{entry.seen}" for entry in snapshot.entries]
            assert harness.cursor.token == step["token"], f"step {index} token"
            assert [event.payload.conversation.id for event in page.changes] == step["delivered"], index
            assert [event.id for event in page.changes] == step["eventIds"], index
            assert page.has_more_pages == step["hasMorePages"], index
            assert len(lines) == step["entryCount"], index
            assert lines[:3] == step["firstEntries"], index
            digest = hashlib.sha256("\n".join(lines).encode()).hexdigest()
            assert digest == step["entriesSha256"], f"step {index} snapshot"
