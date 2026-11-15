# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""itemChanged fetcher behavior, ported from the Go connector's item_changed_fetcher tests."""

import json
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import KeyedSnapshot

from dex_connectors.monday import (
    ITEM_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    ItemChangedEvent,
    ItemChangedTriggerConfiguration,
    ItemChangeKind,
    validate_item_changed_trigger_configuration,
)
from dex_connectors.monday._item_changed_fetcher import (
    ITEM_POLL_PAGE_SIZE,
    MAXIMUM_ITEM_CHANGE_BATCH_BYTES,
    MAXIMUM_ITEM_CHANGES_PER_PAGE,
    MAXIMUM_ITEM_SCAN_PAGES,
    ItemChange,
    ItemChangeFetcher,
    ItemScanExceededError,
    item_change_batch_size,
)
from dex_connectors.monday._item_snapshot import PolledItem
from dex_connectors.monday.models import Item
from tests.monday_board_fake import TOKEN, MondayBoardFake

BOARD_ID = "1234567890"
REFERENCE = ConnectionRef("monday", "monday-board-watch")
CONTEXT = TriggerContext(TriggerCancellation())


def new_client(http_client: httpx.AsyncClient, now: datetime | None = None) -> Client:
    """A client with the minimum poll interval whose requests the given HTTP client sends."""
    return Client(
        Config(poll_interval_seconds=30),
        StaticCredentialProvider({REFERENCE: Credentials("personal-api-token", SecretString(TOKEN))}),
        http_client,
        now=(lambda: now) if now is not None else None,
        endpoint="https://monday.example/v2",
    )


@asynccontextmanager
async def fetcher_for(
    board: MondayBoardFake, now: datetime | None = None
) -> AsyncIterator[ItemChangeFetcher]:
    """A fetcher for BOARD_ID whose requests the board answers."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(board.handle)) as http_client:
        yield ItemChangeFetcher(
            new_client(http_client, now),
            REFERENCE,
            ITEM_CHANGED_TRIGGER_DEFINITION.trigger,
            ItemChangedTriggerConfiguration(BOARD_ID),
        )


async def fetch_all(
    fetcher: ItemChangeFetcher, cursor: Cursor
) -> tuple[list[TriggerEvent[ItemChangedEvent]], Cursor]:
    """Follow has_more_pages from cursor, committing each page as the kit does."""
    events: list[TriggerEvent[ItemChangedEvent]] = []
    while True:
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        events += page.changes
        cursor = page.next_cursor
        if not page.has_more_pages:
            return events, cursor


def watermark_token(instant: datetime) -> str:
    return json.dumps(
        {"boardId": BOARD_ID, "watermark": instant.strftime("%Y-%m-%dT%H:%M:%SZ")}, separators=(",", ":")
    )


async def test_seeds_without_backlog_and_delivers_changes_oldest_first() -> None:
    board = MondayBoardFake(BOARD_ID)
    board.create("5550001", "Existing request")
    board.create("5550002", "Second existing request")
    async with fetcher_for(board) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert cursor.token == watermark_token(board.updated_at("5550002"))
        events, cursor = await fetch_all(fetcher, cursor)
        assert events == [], "existing items are the baseline"

        board.create("5550003", "New request")
        board.set_status("5550001", "Done")
        events, cursor = await fetch_all(fetcher, cursor)
        assert [event.payload.item.id for event in events] == ["5550003", "5550001"]
        assert [event.payload.change_kind for event in events] == [
            ItemChangeKind.CREATED,
            ItemChangeKind.UPDATED,
        ]
        assert events[1].payload.item.column_values[0].text == "Done"
        assert events[1].payload.item.column_values[0].value == {"index": 1}
        assert events[1].occurred_at == board.updated_at("5550001")

        again, _ = await fetch_all(fetcher, cursor)
        assert again == [], "the snapshot keeps items inside the lookback window from arriving twice"
    async with fetcher_for(board) as fresh:
        refetched, _ = await fetch_all(fresh, Cursor(watermark_token(board.updated_at("5550002"))))
    refetched_ids = {event.payload.item.id: event.id for event in refetched}
    assert refetched_ids["5550003"] == events[0].id, "a refetch yields the same Trigger event ID"
    assert refetched_ids["5550001"] == events[1].id


async def test_delivers_a_second_change_within_the_same_second() -> None:
    board = MondayBoardFake(BOARD_ID)
    board.create("5550001", "Request")
    async with fetcher_for(board) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        board.set_status("5550001", "Stuck")
        first, cursor = await fetch_all(fetcher, cursor)
        assert len(first) == 1
        board.set_clock(board.updated_at("5550001"))
        board.set_status("5550001", "Done")
        second, _ = await fetch_all(fetcher, cursor)
    assert len(second) == 1
    assert second[0].payload.item.column_values[0].text == "Done"
    assert second[0].id != first[0].id


async def test_treats_every_item_of_an_empty_baseline_board_as_created() -> None:
    board = MondayBoardFake(BOARD_ID)
    async with fetcher_for(board) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert cursor.token == '{"boardId":"1234567890","watermark":"0001-01-01T00:00:00Z"}'
        board.create("5550001", "First request")
        events, _ = await fetch_all(fetcher, cursor)
    assert [event.payload.change_kind for event in events] == [ItemChangeKind.CREATED]


async def test_splits_a_burst_into_batches_from_one_scan() -> None:
    board = MondayBoardFake(BOARD_ID)
    board.create("5550000", "Existing")
    async with fetcher_for(board) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        for index in range(1, 151):
            board.create(str(5550000 + index), f"Imported {index}")
        requests_before = len(board.requests)
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        assert len(page.changes) == MAXIMUM_ITEM_CHANGES_PER_PAGE and page.has_more_pages
        assert page.changes[0].payload.item.id == "5550001"
        scan_requests = len(board.requests) - requests_before
        assert scan_requests == 2, "two pages reach the item before the cursor"
        rest = await fetcher.fetch_changes(CONTEXT, page.next_cursor)
    assert len(rest.changes) == 50 and not rest.has_more_pages
    assert rest.changes[49].payload.item.id == "5550150"
    assert len(board.requests) == requests_before + scan_requests, (
        "the rest of the burst comes from the same scan"
    )


async def test_pauses_the_connection_for_rate_and_daily_limits() -> None:
    board = MondayBoardFake(BOARD_ID)
    now = datetime(2026, 3, 2, 21, 30, tzinfo=UTC)
    async with fetcher_for(board, now) as fetcher:
        board.rate_limit_next_requests(1, 7)
        with pytest.raises(RateLimitedError) as rate_limited:
            await fetcher.seed_cursor(CONTEXT)
        assert rate_limited.value.retry_after == timedelta(seconds=7)

        board.exhaust_daily_limit_for_next_requests(1)
        with pytest.raises(RateLimitedError) as daily_limited:
            await fetcher.fetch_changes(
                CONTEXT, Cursor('{"boardId":"1234567890","watermark":"2026-03-02T09:00:00Z"}')
            )
    assert daily_limited.value.retry_after == timedelta(minutes=150), (
        "the daily call limit resets at midnight UTC"
    )


async def test_reports_an_unknown_board_as_a_binding_failure() -> None:
    board = MondayBoardFake("999")
    async with fetcher_for(board) as fetcher:
        with pytest.raises(RuntimeError, match="found no board") as failure:
            await fetcher.seed_cursor(CONTEXT)
    assert not is_cursor_invalid(failure.value) and not isinstance(failure.value, RateLimitedError)


async def test_marks_foreign_undecodable_and_oversized_cursors_invalid() -> None:
    board = MondayBoardFake(BOARD_ID)
    async with fetcher_for(board) as fetcher:
        for token in (
            "not json",
            "[]",
            '{"boardId":"999","watermark":"2026-03-02T09:00:00Z"}',
            '{"boardId":"1234567890","watermark":"yesterday"}',
            "null",
        ):
            with pytest.raises(Exception) as invalid:
                await fetcher.fetch_changes(CONTEXT, Cursor(token))
            assert is_cursor_invalid(invalid.value), token

        for index in range(1, MAXIMUM_ITEM_SCAN_PAGES * ITEM_POLL_PAGE_SIZE + 2):
            board.create(str(6000000 + index), "Bulk import")
        with pytest.raises(Exception) as oversized:
            await fetcher.fetch_changes(
                CONTEXT, Cursor('{"boardId":"1234567890","watermark":"2026-03-02T09:00:00Z"}')
            )
    assert is_cursor_invalid(oversized.value), "more changes than one scan reads record a new baseline"
    assert isinstance(oversized.value.__cause__, ItemScanExceededError)


async def test_accepts_a_cursor_in_any_key_case_and_another_offset() -> None:
    board = MondayBoardFake(BOARD_ID)
    board.create("5550001", "Request")
    async with fetcher_for(board) as fetcher:
        page = await fetcher.fetch_changes(
            CONTEXT,
            Cursor('{"BoardID":"1234567890","Watermark":"2026-03-02T11:00:01+02:00"}', KeyedSnapshot()),
        )
    assert [event.payload.change_kind for event in page.changes] == [ItemChangeKind.CREATED], (
        "an empty snapshot delivers the item inside the lookback window"
    )
    assert page.next_cursor.token == '{"boardId":"1234567890","watermark":"2026-03-02T09:00:01Z"}'


def test_item_changed_configuration_requires_a_numeric_board_id() -> None:
    validate_item_changed_trigger_configuration(ItemChangedTriggerConfiguration("1234567890"))
    for board_id in ("", " 1234567890", "board", "0123"):
        with pytest.raises(ValueError):
            validate_item_changed_trigger_configuration(ItemChangedTriggerConfiguration(board_id))


def test_item_change_batches_stop_at_the_count_or_byte_bound() -> None:
    def changes(count: int, encoded_bytes: int) -> list[ItemChange]:
        polled = PolledItem(Item("1"), None, 0, b"x" * encoded_bytes)
        return [ItemChange(polled, ItemChangeKind.UPDATED)] * count

    assert item_change_batch_size(changes(40, 1024)) == 40
    assert item_change_batch_size(changes(150, 1024)) == MAXIMUM_ITEM_CHANGES_PER_PAGE
    assert item_change_batch_size(changes(10, MAXIMUM_ITEM_CHANGE_BATCH_BYTES // 4)) == 4, (
        "a raised response limit cannot overfill the inbox"
    )
    assert item_change_batch_size(changes(3, MAXIMUM_ITEM_CHANGE_BATCH_BYTES + 1)) == 1, (
        "an oversized item still moves alone"
    )


def test_client_refuses_a_poll_interval_under_thirty_seconds() -> None:
    credentials = StaticCredentialProvider(
        {REFERENCE: Credentials("personal-api-token", SecretString(TOKEN))}
    )
    for seconds in (-30, 29.5, True):
        with pytest.raises(ValueError, match="at least 30s"):
            Client(Config(poll_interval_seconds=seconds), credentials)
    assert Client(Config(), credentials).poll_interval == timedelta(minutes=5)
    # A stored zero pollInterval uses the default, as Go's withConfigDefaults does.
    assert Client(Config(poll_interval_seconds=0), credentials).poll_interval == timedelta(minutes=5)
