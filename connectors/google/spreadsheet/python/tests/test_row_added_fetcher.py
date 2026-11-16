# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Port the Go rowAdded fetcher tests: seeding, paging, realignment, and provider failure classification."""

import json
import re
from datetime import UTC, datetime, timedelta
from pathlib import Path

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerBindingRef,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import ChangePage, Cursor, RateLimitedError, is_cursor_invalid

from dex_connectors.google_spreadsheet import (
    ROW_ADDED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    RowAddedEvent,
    RowAddedTriggerConfiguration,
    row_added_poll_schedule,
    validate_row_added_trigger_configuration,
)
from dex_connectors.google_spreadsheet.row_added_cursor import (
    RowAddedCursorToken,
    decode_row_added_cursor_token,
    hash_row_cells,
    row_added_event_id,
)
from dex_connectors.google_spreadsheet.row_added_fetcher import RowAddedFetcher
from examples.new_row_intake.sheets_fake import SHEETS_ENDPOINT, RecordedRequest, ScriptedResponse, SheetsFake

LEADS_HEADER = ["email", "name", "status"]
REFERENCE = ConnectionRef("google", "sheets-leads")
NOW = datetime(2026, 11, 15, 9, 0, tzinfo=UTC)
CONTEXT = TriggerContext()
Page = ChangePage[object]


def credentials() -> StaticCredentialProvider[Credentials]:
    return StaticCredentialProvider(
        {
            REFERENCE: Credentials(
                "client", SecretString(""), SecretString("SENTINEL-ACCESS-TOKEN"), SecretString("")
            )
        }
    )


def new_fetcher(
    sheet: SheetsFake, config: Config | None = None, sheet_name: str = "Leads 2026"
) -> RowAddedFetcher:
    base = config or Config()
    client = Client(
        Config(
            SHEETS_ENDPOINT,
            base.max_response_bytes,
            base.max_rows,
            base.poll_interval_seconds,
            base.poll_page_size,
        ),
        credentials(),
        httpx.AsyncClient(transport=sheet.transport()),
        now=lambda: NOW,
    )
    binding = TriggerBindingRef(REFERENCE, ROW_ADDED_TRIGGER_DEFINITION.trigger, "new-leads")
    return RowAddedFetcher(client, binding, RowAddedTriggerConfiguration(" sheet-1 ", sheet_name))


def assert_cursor_row(cursor: Cursor, last_row: int, cells: list[str] | None) -> None:
    assert decode_row_added_cursor_token(cursor.token) == RowAddedCursorToken(last_row, hash_row_cells(cells))


def row_numbers(page: ChangePage[object]) -> list[int]:
    return [change.payload.row_number for change in page.changes]  # type: ignore[attr-defined]


def scanned_ranges(sheet: SheetsFake) -> list[str]:
    return [value_range for request in sheet.requests() for value_range in request.ranges]


def test_cursor_tokens_hashes_and_event_ids_match_go_bytes() -> None:
    """Fixtures come from Go's encoding/json and crypto/sha256, including HTML-escaped and non-ASCII cells."""
    cases = json.loads((Path(__file__).parent / "fixtures" / "go-row-added-cases.json").read_text())
    for case in cases:
        assert hash_row_cells(case["cells"]) == case["rowHash"]
        assert RowAddedCursorToken(7, case["rowHash"]).encode() == case["token"]
        assert row_added_event_id("sheet-1", "Leads 2026 'q'", 12, case["cells"]) == case["eventId"]
        assert decode_row_added_cursor_token(case["token"]) == RowAddedCursorToken(7, case["rowHash"])
    # Go matches field names case-insensitively, lets a later duplicate win, and ignores trailing text.
    row_hash = cases[0]["rowHash"]
    lenient = f' {{"V":1,"lastRow":3,"LASTROW":2,"rowHash":null,"rowHash":"{row_hash.upper()}"}} trailing'
    assert decode_row_added_cursor_token(lenient) == RowAddedCursorToken(2, row_hash.upper())


async def test_seeds_after_the_last_row_and_delivers_appended_rows_in_order() -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER, ["ada@example.test", "Ada", "new"])
    fetcher = new_fetcher(sheet)
    seed = await fetcher.seed_cursor(CONTEXT)
    assert_cursor_row(seed, 2, ["ada@example.test", "Ada", "new"])
    page = await fetcher.fetch_changes(CONTEXT, seed)
    assert not page.changes, "existing rows are never delivered"
    assert page.next_cursor == seed

    sheet.append_rows(
        ["grace@example.test", "Grace"], [], ["", "", "", ""], ["linus@example.test", "Linus", "new", "extra"]
    )
    page = await fetcher.fetch_changes(CONTEXT, seed)
    assert row_numbers(page) == [3, 6], "blank rows are skipped"
    assert not page.has_more_pages
    assert_cursor_row(page.next_cursor, 6, ["linus@example.test", "Linus", "new", "extra"])
    first = page.changes[0]
    assert first.payload == RowAddedEvent(
        "sheet-1",
        "Leads 2026",
        3,
        {"email": "grace@example.test", "name": "Grace", "status": ""},
        ("grace@example.test", "Grace"),
    )
    assert re.fullmatch(r"row-3-[0-9a-f]{16}", first.id)
    assert first.occurred_at == NOW

    refetched = await fetcher.fetch_changes(CONTEXT, seed)
    assert [change.id for change in refetched.changes] == [change.id for change in page.changes]
    assert sheet.requests()[-1] == RecordedRequest(
        "sheet-1", ("'Leads 2026'!1:1", "'Leads 2026'!2:102"), "Bearer SENTINEL-ACCESS-TOKEN"
    )


async def test_seeds_an_empty_tab_at_the_header_row() -> None:
    sheet = SheetsFake("Leads 2026")
    fetcher = new_fetcher(sheet)
    seed = await fetcher.seed_cursor(CONTEXT)
    assert_cursor_row(seed, 1, None)
    sheet.append_rows(LEADS_HEADER, ["ada@example.test"])
    page = await fetcher.fetch_changes(CONTEXT, seed)
    assert row_numbers(page) == [2], "a header added after seeding is not delivered as a row"
    assert page.changes[0].payload.values == {"email": "ada@example.test", "name": "", "status": ""}


async def test_seed_scans_the_tab_in_max_rows_windows() -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER, *(["row", letter] for letter in "abcde"))
    seed = await new_fetcher(sheet, Config(max_rows=2)).seed_cursor(CONTEXT)
    assert_cursor_row(seed, 6, ["row", "e"])
    assert scanned_ranges(sheet) == [
        "'Leads 2026'!1:2",
        "'Leads 2026'!3:4",
        "'Leads 2026'!5:6",
        "'Leads 2026'!7:8",
    ]


async def test_pages_large_batches_within_poll_page_size() -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER)
    fetcher = new_fetcher(sheet, Config(poll_page_size=2))
    seed = await fetcher.seed_cursor(CONTEXT)
    sheet.append_rows(["a"], ["b"], ["c"])
    page = await fetcher.fetch_changes(CONTEXT, seed)
    assert row_numbers(page) == [2, 3] and page.has_more_pages
    page = await fetcher.fetch_changes(CONTEXT, page.next_cursor)
    assert row_numbers(page) == [4] and not page.has_more_pages


async def test_realigns_after_rows_above_the_cursor_are_deleted_or_inserted() -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER, ["a"], ["b"], ["c"])
    fetcher = new_fetcher(sheet)
    seed = await fetcher.seed_cursor(CONTEXT)
    sheet.delete_row(2)
    sheet.append_rows(["d"])
    page = await fetcher.fetch_changes(CONTEXT, seed)
    assert row_numbers(page) == [4], "row c moved from 4 to 3, so only d is new"
    assert page.changes[0].payload.cells == ("d",)
    assert_cursor_row(page.next_cursor, 4, ["d"])

    sheet.truncate_rows(1)
    sheet.append_rows(["new-1"], ["b"], ["c"], ["d"], ["e"])
    page = await fetcher.fetch_changes(CONTEXT, page.next_cursor)
    assert row_numbers(page) == [6], "row d moved down from 4 to 5 after an insert above it"


async def test_keeps_the_cursor_row_when_it_is_edited_in_place() -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER, ["a", "new"])
    fetcher = new_fetcher(sheet)
    seed = await fetcher.seed_cursor(CONTEXT)
    sheet.replace_row(2, ["a", "contacted"])
    sheet.append_rows(["b", "new"])
    page = await fetcher.fetch_changes(CONTEXT, seed)
    assert row_numbers(page) == [3]
    assert_cursor_row(page.next_cursor, 3, ["b", "new"])


@pytest.mark.parametrize(
    "token",
    [
        "",
        "{}",
        '{"v":1,"lastRow":0,"rowHash":""}',
        '{"v":2,"lastRow":2,"rowHash":"' + hash_row_cells(None) + '"}',
        '{"v":1,"lastRow":2,"rowHash":"zz","extra":1}',
        '{"v":1,"lastRow":2.0,"rowHash":"' + hash_row_cells(None) + '"}',
        '{"v":1,"lastRow":2,"rowHash":"' + "z" * 64 + '"}',
        "[1]",
    ],
)
async def test_reports_an_invalid_cursor_for_tokens_go_refuses(token: str) -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER)
    with pytest.raises(Exception) as raised:
        await new_fetcher(sheet).fetch_changes(CONTEXT, Cursor(token))
    assert is_cursor_invalid(raised.value)


async def test_reports_an_invalid_cursor_when_the_tab_ends_above_it() -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER, ["a"], ["b"], ["c"])
    fetcher = new_fetcher(sheet)
    seed = await fetcher.seed_cursor(CONTEXT)
    sheet.truncate_rows(2)
    with pytest.raises(Exception) as raised:
        await fetcher.fetch_changes(CONTEXT, seed)
    assert is_cursor_invalid(raised.value)


async def test_classifies_provider_failures_without_leaking_secrets() -> None:
    sheet = SheetsFake("Leads 2026", LEADS_HEADER)
    fetcher = new_fetcher(sheet)
    seed = await fetcher.seed_cursor(CONTEXT)

    sheet.script(ScriptedResponse(429, "7"))
    with pytest.raises(RateLimitedError) as rate_limited:
        await fetcher.fetch_changes(CONTEXT, seed)
    assert rate_limited.value.retry_after == timedelta(seconds=7)
    sheet.script(ScriptedResponse(429, "soon"))
    with pytest.raises(RateLimitedError) as rate_limited:
        await fetcher.fetch_changes(CONTEXT, seed)
    assert rate_limited.value.retry_after == timedelta(0), "an unreadable Retry-After falls back to backoff"

    for status in (404, 403, 500):
        sheet.script(ScriptedResponse(status))
        with pytest.raises(Exception) as raised:
            await fetcher.fetch_changes(CONTEXT, seed)
        assert not is_cursor_invalid(raised.value), f"HTTP {status} keeps the cursor"
        assert not isinstance(raised.value, RateLimitedError)
        assert "SENTINEL" not in str(raised.value) and "Leads" not in str(raised.value)


async def test_rejects_invalid_poll_responses() -> None:
    for body in (
        b"[]",
        b'{"valueRanges":[{},{}]}',
        b'{"valueRanges":[{"values":"rows"}]}',
        b'{"valueRanges":[{"range":1}]}',
        b"not json",
    ):
        client = Client(
            Config(SHEETS_ENDPOINT),
            credentials(),
            httpx.AsyncClient(
                transport=httpx.MockTransport(lambda _, body=body: httpx.Response(200, content=body))
            ),
        )
        binding = TriggerBindingRef(REFERENCE, ROW_ADDED_TRIGGER_DEFINITION.trigger, "new-leads")
        fetcher = RowAddedFetcher(client, binding, RowAddedTriggerConfiguration("sheet-1", "Leads"))
        with pytest.raises(RuntimeError, match="invalid rowAdded poll response"):
            await fetcher.seed_cursor(CONTEXT)


def test_configuration_and_poll_settings_are_validated() -> None:
    with pytest.raises(ValueError):
        validate_row_added_trigger_configuration(RowAddedTriggerConfiguration(spreadsheet_id="sheet-1"))
    with pytest.raises(ValueError):
        validate_row_added_trigger_configuration(RowAddedTriggerConfiguration(sheet_name="Leads"))
    validate_row_added_trigger_configuration(RowAddedTriggerConfiguration("sheet-1", "Leads"))
    for config in (Config(poll_interval_seconds=0.5), Config(poll_page_size=501), Config(poll_page_size=-1)):
        with pytest.raises(ValueError):
            Client(config, credentials())
    client = Client(Config(poll_interval_seconds=0, poll_page_size=0), credentials())
    assert (client.config.poll_interval_seconds, client.config.poll_page_size) == (30, 100)
    schedule = row_added_poll_schedule(client)
    assert (schedule.interval, schedule.maximum_backoff) == (timedelta(seconds=30), timedelta(minutes=5))


async def test_uses_the_exact_tab_title_including_outer_spaces() -> None:
    sheet = SheetsFake(" Leads 2026 ", LEADS_HEADER, ["a"])
    fetcher = new_fetcher(sheet, sheet_name=" Leads 2026 ")
    seed = await fetcher.seed_cursor(CONTEXT)
    sheet.append_rows(["b"])
    page = await fetcher.fetch_changes(CONTEXT, seed)
    assert row_numbers(page) == [3]
    assert page.changes[0].payload.sheet_name == " Leads 2026 "
    assert sheet.requests()[-1].ranges == ("' Leads 2026 '!1:1", "' Leads 2026 '!2:102")


async def test_seed_halves_scan_windows_whose_response_is_too_large() -> None:
    rows = [LEADS_HEADER] + [[f"row-{index:03d}-padding-padding"] for index in range(300)]
    sheet = SheetsFake("Leads 2026", *rows)
    # A 400-row window returns all 301 rows, about 10 KB; a 200-row window stays under 7 KB.
    seed = await new_fetcher(sheet, Config(max_rows=400, max_response_bytes=7000)).seed_cursor(CONTEXT)
    assert_cursor_row(seed, 301, rows[300])
    assert scanned_ranges(sheet) == ["'Leads 2026'!1:400", "'Leads 2026'!1:200", "'Leads 2026'!201:400"]
    too_small = new_fetcher(sheet, Config(max_rows=400, max_response_bytes=1000))
    with pytest.raises(RuntimeError, match="provider response exceeds configured limit"):
        await too_small.seed_cursor(CONTEXT)
