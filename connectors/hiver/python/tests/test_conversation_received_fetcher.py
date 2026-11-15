# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The conversationReceived fetcher's baseline, sweep, restart, and cursor rules, as Go checks them."""

import dataclasses
from collections.abc import AsyncIterator
from datetime import timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerBindingRef,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import ChangePage, Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import KeyedSnapshot

from dex_connectors.hiver import (
    CONVERSATION_RECEIVED_TRIGGER_DEFINITION,
    Client,
    Config,
    ConversationReceivedEvent,
    ConversationReceivedTriggerConfiguration,
    Credentials,
)
from dex_connectors.hiver._conversation_received_cursor import (
    ConversationReceivedCursorToken,
    decode_conversation_received_cursor_token,
)
from dex_connectors.hiver._conversation_received_fetcher import ConversationReceivedFetcher
from tests.hiver_conversations_fake import API_BASE_URL, API_KEY, HiverConversationsFake

INBOX_ID = "105902"
REFERENCE = ConnectionRef("hiver", "hiver-support")
BINDING = TriggerBindingRef(REFERENCE, CONVERSATION_RECEIVED_TRIGGER_DEFINITION.trigger, "new-conversations")


def conversation_ids(first: int, count: int) -> list[str]:
    """Return count numeric IDs from first, such as 573741000."""
    return [str(first + index) for index in range(count)]


class CursorHarness:
    """Stands in for the kit's commits: it stamps seen zero with the next commit count."""

    def __init__(self, fetcher: ConversationReceivedFetcher) -> None:
        self.fetcher = fetcher
        self.cursor = Cursor()
        self.commits = 0

    async def seed(self) -> None:
        self.commit(await self.fetcher.seed_cursor(TriggerContext()))

    def commit(self, cursor: Cursor) -> None:
        self.commits += 1
        snapshot = cursor.snapshot
        if snapshot is not None:
            entries = tuple(
                entry if entry.seen else dataclasses.replace(entry, seen=self.commits)
                for entry in snapshot.entries
            )
            snapshot = KeyedSnapshot(entries)
        self.cursor = Cursor(cursor.token, snapshot)

    async def fetch(self) -> tuple[list[str], ChangePage[TriggerEvent[ConversationReceivedEvent]]]:
        page = await self.fetcher.fetch_changes(TriggerContext(), self.cursor)
        self.commit(page.next_cursor)
        delivered = []
        for event in page.changes:
            assert event.id == f"conversation-{INBOX_ID}-{event.payload.conversation.id}"
            assert event.payload.inbox_id == INBOX_ID
            delivered.append(event.payload.conversation.id)
        return delivered, page

    async def complete_baseline(self) -> None:
        while True:
            delivered, page = await self.fetch()
            assert delivered == [], "the baseline delivers no existing conversation"
            if not page.has_more_pages:
                return

    def token(self) -> ConversationReceivedCursorToken:
        return decode_conversation_received_cursor_token(self.cursor.token, INBOX_ID)

    def tracked_keys(self) -> list[str]:
        assert self.cursor.snapshot is not None
        return [entry.key for entry in self.cursor.snapshot.entries]


@pytest.fixture
async def provider() -> AsyncIterator[HiverConversationsFake]:
    yield HiverConversationsFake(INBOX_ID, "support@acme.example.com")


async def new_harness(
    provider: HiverConversationsFake, pages_per_poll: int = 0, inbox_id: str = " " + INBOX_ID + " "
) -> CursorHarness:
    client = Client(
        Config(request_interval_milliseconds=1, poll_pages_per_poll=pages_per_poll),
        StaticCredentialProvider({REFERENCE: Credentials(SecretString(API_KEY))}),
        httpx.AsyncClient(transport=provider.transport),
        API_BASE_URL,
    )
    configuration = ConversationReceivedTriggerConfiguration(inbox_id=inbox_id)
    harness = CursorHarness(ConversationReceivedFetcher(client, BINDING, configuration))
    await harness.seed()
    return harness


async def test_baseline_records_existing_conversations_without_events(
    provider: HiverConversationsFake,
) -> None:
    provider.add_conversations(INBOX_ID, *conversation_ids(573741000, 1250))
    harness = await new_harness(provider)
    assert provider.conversation_list_requests == 0, "seeding sends no request"

    _, first = await harness.fetch()
    assert first.has_more_pages, "a baseline of 13 pages continues after 10"
    assert provider.conversation_list_requests == 10
    await harness.complete_baseline()
    assert provider.conversation_list_requests == 13
    assert len(harness.tracked_keys()) == 1250
    assert harness.token() == ConversationReceivedCursorToken(INBOX_ID, is_baseline_complete=True)
    assert harness.cursor.token == '{"v":1,"inboxId":"105902","baselineComplete":true}'


async def test_delivers_new_conversations_from_the_first_page_in_list_order(
    provider: HiverConversationsFake,
) -> None:
    provider.add_conversations(INBOX_ID, *conversation_ids(573741000, 250))
    harness = await new_harness(provider)
    await harness.complete_baseline()

    provider.add_conversations(INBOX_ID, "573742001", "573742002")
    before = provider.conversation_list_requests
    delivered, page = await harness.fetch()
    assert delivered == ["573742002", "573742001"], "newest-first list order"
    assert not page.has_more_pages, "a poll never continues at once after the baseline"
    assert provider.conversation_list_requests - before == 3, "the first page plus two sweep pages"
    assert page.changes[0].payload.conversation.status == "open"

    delivered, _ = await harness.fetch()
    assert delivered == [], "a tracked conversation is never delivered again"


async def test_sweep_finds_conversations_listed_on_later_pages(provider: HiverConversationsFake) -> None:
    provider.add_conversations(INBOX_ID, *conversation_ids(573741000, 450))
    provider.list_oldest_first()
    harness = await new_harness(provider)
    await harness.complete_baseline()

    provider.add_conversations(INBOX_ID, "573749999")
    delivered, _ = await harness.fetch()
    assert delivered == [], "pages 1 to 3 of 5 do not list it"
    delivered, _ = await harness.fetch()
    assert delivered == ["573749999"], "the sweep reaches the last page"
    assert harness.token().sweep_page_token == "", "the sweep finished and restarts from the first page"
    for _ in range(3):
        delivered, _ = await harness.fetch()
        assert delivered == []


async def test_stops_tracking_conversations_two_whole_sweeps_never_listed(
    provider: HiverConversationsFake,
) -> None:
    provider.add_conversations(INBOX_ID, "573741001", "573741002", "573741003")
    harness = await new_harness(provider)
    await harness.complete_baseline()

    provider.delete_conversation(INBOX_ID, "573741002")
    delivered, _ = await harness.fetch()
    assert delivered == []
    assert len(harness.tracked_keys()) == 3, "one complete sweep without the ID keeps tracking it"
    delivered, _ = await harness.fetch()
    assert delivered == []
    assert harness.tracked_keys() == ["573741001", "573741003"], "two complete sweeps never listed it"


async def test_never_redelivers_a_conversation_shifted_past_a_sweep_page(
    provider: HiverConversationsFake,
) -> None:
    provider.add_conversations(INBOX_ID, *conversation_ids(573741000, 250))
    harness = await new_harness(provider, pages_per_poll=2, inbox_id=INBOX_ID)
    await harness.complete_baseline()

    delivered, _ = await harness.fetch()
    assert delivered == [], "the sweep reads offsets 0 to 199"
    # Newest first, offset 200 lists 573741049 until a deletion above it shifts it to offset 199.
    provider.delete_conversation(INBOX_ID, "573741249")
    delivered, _ = await harness.fetch()
    assert delivered == [], "the sweep's last page starts at offset 200 and skips 573741049"
    assert harness.token().sweep_page_token == "", "the sweep completed"
    assert "573741049" in harness.tracked_keys(), "one sweep that skipped it keeps tracking it"
    for _ in range(4):
        delivered, _ = await harness.fetch()
        assert delivered == [], "a conversation that was there all along is never delivered"
    assert "573741249" not in harness.tracked_keys(), "the deleted conversation stops being tracked"
    assert len(harness.tracked_keys()) == 249


async def test_restarts_the_sweep_when_hiver_rejects_its_page_token(provider: HiverConversationsFake) -> None:
    provider.add_conversations(INBOX_ID, *conversation_ids(573741000, 450))
    provider.list_oldest_first()
    harness = await new_harness(provider)
    await harness.complete_baseline()
    await harness.fetch()
    assert harness.token().sweep_page_token != ""

    provider.expire_page_tokens()
    provider.add_conversations(INBOX_ID, "573749999")
    delivered, _ = await harness.fetch()
    assert delivered == []
    assert harness.token().sweep_page_token == "", "the rejected token restarts the sweep"
    assert len(harness.tracked_keys()) == 450, "a restart keeps every tracked conversation"
    delivered, _ = await harness.fetch()
    assert delivered == []
    delivered, _ = await harness.fetch()
    assert delivered == ["573749999"], "the restarted sweep finds the conversation without a gap"


async def test_restarts_the_baseline_when_hiver_rejects_its_page_token(
    provider: HiverConversationsFake,
) -> None:
    provider.add_conversations(INBOX_ID, *conversation_ids(573741000, 1250))
    harness = await new_harness(provider)
    await harness.fetch()
    provider.expire_page_tokens()
    _, page = await harness.fetch()
    assert page.has_more_pages
    assert harness.token() == ConversationReceivedCursorToken(INBOX_ID)
    await harness.complete_baseline()
    assert len(harness.tracked_keys()) == 1250


async def test_maps_rate_limits_and_invalid_cursors(provider: HiverConversationsFake) -> None:
    provider.add_conversations(INBOX_ID, "573741001")
    harness = await new_harness(provider)
    await harness.complete_baseline()

    provider.rate_limit_next(2)
    with pytest.raises(RateLimitedError) as limited:
        await harness.fetcher.fetch_changes(TriggerContext(), harness.cursor)
    assert limited.value.retry_after == timedelta(seconds=2)
    assert API_KEY not in str(limited.value)

    provider.rate_limit_next(0)
    with pytest.raises(RateLimitedError) as limited:
        await harness.fetcher.fetch_changes(TriggerContext(), harness.cursor)
    assert limited.value.retry_after == timedelta(0), "without Retry-After the runner backs off exponentially"

    for name, token in {
        "not JSON": "row-12",
        "other version": '{"v":2,"inboxId":"105902"}',
        "other inbox": '{"v":1,"inboxId":"202","baselineComplete":true}',
        "unknown field": '{"v":1,"inboxId":"105902","cursor":"x"}',
        "bad page": '{"v":1,"inboxId":"105902","sweepPageToken":"a b"}',
        "fractional seen": '{"v":1,"inboxId":"105902","sweepStartSeen":1.5}',
        "previous after start": '{"v":1,"inboxId":"105902","sweepStartSeen":1,"previousSweepStartSeen":2}',
    }.items():
        with pytest.raises(Exception) as invalid:
            await harness.fetcher.fetch_changes(TriggerContext(), Cursor(token, KeyedSnapshot()))
        assert is_cursor_invalid(invalid.value), name


def test_cursor_token_decoding_follows_go_json_decoder() -> None:
    token = decode_conversation_received_cursor_token(
        ' {"V":1,"INBOXID":"105902","baselineComplete":null,"sweepPageToken":"e0.o100",'
        '"sweepStartSeen":7,"previousSweepStartSeen":3,"v":1} trailing',
        INBOX_ID,
    )
    assert token == ConversationReceivedCursorToken(INBOX_ID, False, "e0.o100", 7, 3)
    assert token.encode() == (
        '{"v":1,"inboxId":"105902","baselineComplete":false,"sweepPageToken":"e0.o100",'
        '"sweepStartSeen":7,"previousSweepStartSeen":3}'
    )


async def test_reports_a_missing_inbox_as_a_retried_failure() -> None:
    provider = HiverConversationsFake("999", "other@acme.example.com")
    harness = await new_harness(provider)
    with pytest.raises(Exception) as missing:
        await harness.fetcher.fetch_changes(TriggerContext(), harness.cursor)
    assert "HTTP 404" in str(missing.value)
    assert not is_cursor_invalid(missing.value), "a missing inbox is retried, never rebaselined"
