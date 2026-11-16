# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The recordChanged fetcher against a local Airtable table, at parity with the Go fetcher tests."""

import hashlib
from collections import Counter
from collections.abc import Iterator
from datetime import UTC, datetime, timedelta

import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerBindingRef,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid

from dex_connectors.airtable import (
    RECORD_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    RecordChangedTriggerConfiguration,
    validate_record_changed_trigger_configuration,
)
from dex_connectors.airtable import client as airtable_client
from dex_connectors.airtable._record_changed_fetcher import RecordChangedFetcher
from tests.airtable_table_fake import (
    ACCESS_TOKEN,
    BASE_ID,
    ENDPOINT,
    FIELD_ID,
    TABLE_ID,
    AirtableTableFake,
    FakeRecord,
    ScriptedResponse,
    fake_record,
)

SEED_TIME = datetime(2026, 11, 15, 9, 0, 0, tzinfo=UTC)
CONNECTION = ConnectionRef("airtable", "airtable-intake")
BINDING = TriggerBindingRef(CONNECTION, RECORD_CHANGED_TRIGGER_DEFINITION.trigger, "lead-changed")
CONFIGURATION = RecordChangedTriggerConfiguration(base_id=BASE_ID, table_id=TABLE_ID, field_id=FIELD_ID)
MAXIMUM_POLL_CURSOR_TOKEN_BYTES = 64 * 1024


@pytest.fixture(autouse=True)
def unpaced_requests(monkeypatch: pytest.MonkeyPatch) -> None:
    """Drop the 5-per-second base pacing, so scans of thousands of records run quickly."""
    monkeypatch.setattr(airtable_client, "_REQUEST_INTERVAL_PER_BASE_SECONDS", 0)


def new_fetcher(table: AirtableTableFake, page_size: int) -> RecordChangedFetcher:
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(ACCESS_TOKEN))})
    config = Config(endpoint=ENDPOINT, poll_interval_seconds=1, poll_page_size=page_size)
    return RecordChangedFetcher(Client(config, credentials, table.http_client()), BINDING, CONFIGURATION)


async def fetch_ids(fetcher: RecordChangedFetcher, cursor: Cursor) -> tuple[list[str], Cursor, bool]:
    page = await fetcher.fetch_changes(TriggerContext(), cursor)
    return [event.payload.record.id for event in page.changes], page.next_cursor, page.has_more_pages


async def drain_ids(
    fetcher: RecordChangedFetcher, cursor: Cursor, maximum_fetches: int
) -> tuple[Counter[str], Cursor]:
    """Fetch until a page reports no more and delivers nothing, counting deliveries per record."""
    delivered: Counter[str] = Counter()
    for _ in range(maximum_fetches):
        ids, cursor, has_more = await fetch_ids(fetcher, cursor)
        assert len(cursor.token.encode()) <= MAXIMUM_POLL_CURSOR_TOKEN_BYTES
        delivered.update(ids)
        if not has_more and not ids:
            return delivered, cursor
    raise AssertionError(f"the feed did not become current within {maximum_fetches} fetches")


def burst_records(prefix: str, count: int, at: datetime) -> Iterator[FakeRecord]:
    for index in range(count):
        yield fake_record(f"rec{prefix}{index:0{14 - len(prefix)}d}", at)


async def test_seed_skips_existing_records_and_delivers_later_changes_oldest_first() -> None:
    table = AirtableTableFake(
        fake_record("recExisting000001", SEED_TIME),
        fake_record("recExisting000002", SEED_TIME),
        fake_record("recOlder000000001", SEED_TIME - timedelta(hours=1)),
    )
    fetcher = new_fetcher(table, 100)
    cursor = await fetcher.seed_cursor(TriggerContext())
    assert cursor.token == (
        '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z",'
        '"seen":["recExisting000002:0","recExisting000001:0"]}'
    )
    ids, cursor, _ = await fetch_ids(fetcher, cursor)
    assert ids == [], "records present at seed never start Flows"

    edited = FakeRecord(
        "recExisting000001",
        SEED_TIME - timedelta(hours=1),
        SEED_TIME + timedelta(seconds=2),
        {"Email": "recExisting000001@example.test"},
    )
    table.put(
        fake_record("recNewLead0000001", SEED_TIME + timedelta(seconds=1)),
        edited,
        fake_record("recSameMilli00001", SEED_TIME),
    )
    page = await fetcher.fetch_changes(TriggerContext(), cursor)
    assert [event.payload.record.id for event in page.changes] == [
        "recSameMilli00001",
        "recNewLead0000001",
        "recExisting000001",
    ], "a record written at the cursor's own millisecond is still delivered"
    created, update = page.changes[1], page.changes[2]
    assert created.payload.is_created_record
    assert not update.payload.is_created_record
    assert update.id == "recExisting000001-20261115T090002000000000Z"
    assert update.occurred_at == SEED_TIME + timedelta(seconds=2)
    assert update.payload.last_modified_time == SEED_TIME + timedelta(seconds=2)
    assert update.payload.base_id == BASE_ID and update.payload.table_id == TABLE_ID
    assert (update.payload.record.fields or {})["Email"] == "recExisting000001@example.test"

    ids, cursor, _ = await fetch_ids(fetcher, page.next_cursor)
    assert ids == [], "a delivered change is not delivered again"
    table.put(fake_record("recExisting000001", SEED_TIME + timedelta(seconds=3)))
    ids, _, _ = await fetch_ids(fetcher, cursor)
    assert ids == ["recExisting000001"], "a later edit of the same record is a new change"
    assert all(request["sort"][0]["direction"] == "asc" for request in table.list_requests[1:])
    assert table.list_requests[0]["sort"] == [{"field": FIELD_ID, "direction": "desc"}]
    assert (
        table.list_requests[-1]["filterByFormula"]
        == 'IS_AFTER({fldLastModified01}, DATETIME_PARSE("2026-11-15T08:59:56.000Z"))'
    ), "the filter reaches the overlap and one more second before the latest change"
    assert table.schema_requests == 1, "the field name is read once"


async def test_pages_and_tie_groups_larger_than_a_page_are_delivered_once() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 2)
    cursor = await fetcher.seed_cursor(TriggerContext())
    assert cursor.token == '{"v":2,"fieldId":"fldLastModified01","after":""}', "an empty table seeds early"
    changed_at = SEED_TIME + timedelta(minutes=1)
    table.put(
        fake_record("recTie00000000001", changed_at),
        fake_record("recTie00000000002", changed_at),
        fake_record("recTie00000000003", changed_at),
        fake_record("recLater000000001", changed_at + timedelta(seconds=1)),
    )
    delivered, _ = await drain_ids(fetcher, cursor, 4)
    assert delivered == Counter(
        ["recTie00000000001", "recTie00000000002", "recTie00000000003", "recLater000000001"]
    ), "no record is delivered twice"
    assert any(request.get("offset") for request in table.list_requests), "ties are skipped by offset"


async def test_a_renamed_field_is_resolved_again_and_a_changed_field_type_fails_the_poll() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 100)
    cursor = await fetcher.seed_cursor(TriggerContext())
    table.rename_field("Updated At")
    table.put(fake_record("recRenamed0000001", SEED_TIME))
    with pytest.raises(Exception, match="field name is read again"):
        await fetcher.fetch_changes(TriggerContext(), cursor)
    ids, _, _ = await fetch_ids(fetcher, cursor)
    assert ids == ["recRenamed0000001"]
    assert table.schema_requests == 2

    table.set_field_type("dateTime")
    table.rename_field("Due")
    with pytest.raises(Exception):  # noqa: B017 - the stale field name fails first, as in Go
        await fetcher.fetch_changes(TriggerContext(), cursor)
    with pytest.raises(Exception, match="not a Last modified time field") as raised:
        await fetcher.fetch_changes(TriggerContext(), cursor)
    assert not is_cursor_invalid(raised.value), "a configuration fault backs off without losing the cursor"


async def test_rate_limits_pause_the_runner_for_airtables_thirty_seconds() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 100)
    table.script(ScriptedResponse(429, "1", "RATE_LIMIT_REACHED"))
    with pytest.raises(RateLimitedError) as raised:
        await fetcher.seed_cursor(TriggerContext())
    assert raised.value.retry_after >= timedelta(seconds=29)
    assert "SENTINEL" not in str(raised.value)

    # The per-base hold makes the next request wait too, without calling Airtable.
    before = table.completed_requests
    with pytest.raises(RateLimitedError):
        await fetcher.seed_cursor(TriggerContext())
    assert table.completed_requests == before


@pytest.mark.parametrize(
    "token",
    [
        "",
        "not-json",
        '{"v":1,"fieldId":"fldLastModified01","after":""}',
        '{"v":2,"fieldId":"fldSomethingElse1","after":""}',
        '{"v":2,"fieldId":"fldLastModified01","after":"yesterday"}',
        '{"v":2,"fieldId":"fldLastModified01","after":"","seen":["recExisting000001:0"]}',
        '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","seen":["Lead:0"]}',
        '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z",'
        '"scan":{"since":"","offset":"itr1/recExisting000001"}}',
        '{"v":2.0,"fieldId":"fldLastModified01","after":""}',
        '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","seen":["recExisting000001:-1"]}',
        '{"v":2,"fieldId":"fldLastModified01","after":"2026-02-30T09:00:00Z"}',
    ],
)
async def test_unusable_cursors_are_reseeded(token: str) -> None:
    fetcher = new_fetcher(AirtableTableFake(), 100)
    with pytest.raises(Exception) as raised:  # noqa: B017 - the kit only checks the invalid mark
        await fetcher.fetch_changes(TriggerContext(), Cursor(token))
    assert is_cursor_invalid(raised.value), token


async def test_cursors_decode_as_go_decodes_them() -> None:
    fetcher = new_fetcher(AirtableTableFake(), 100)
    # Go matches keys ignoring case, lets a later key win, and ignores unknown keys.
    token = '{"V":2,"FIELDID":"fldLastModified01","after":"2026-11-15T10:00:00+01:00","after2":1,"seen":null}'
    page = await fetcher.fetch_changes(TriggerContext(), Cursor(token))
    assert page.next_cursor.token == '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z"}'


async def test_provider_failures_keep_the_cursor() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 100)
    cursor = await fetcher.seed_cursor(TriggerContext())
    table.script(
        ScriptedResponse(503), ScriptedResponse(403, error_type="INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND")
    )
    for _ in range(2):
        with pytest.raises(Exception) as raised:  # noqa: B017 - any failure other than the two marks
            await fetcher.fetch_changes(TriggerContext(), cursor)
        assert not is_cursor_invalid(raised.value)
        assert not isinstance(raised.value, RateLimitedError)


def test_record_changed_configuration_validation() -> None:
    validate_record_changed_trigger_configuration(CONFIGURATION)
    for invalid in (
        RecordChangedTriggerConfiguration(table_id=TABLE_ID, field_id=FIELD_ID),
        RecordChangedTriggerConfiguration(base_id=BASE_ID, table_id="Leads", field_id=FIELD_ID),
        RecordChangedTriggerConfiguration(base_id=BASE_ID, table_id=TABLE_ID, field_id="Last Modified"),
    ):
        with pytest.raises(ValueError):
            validate_record_changed_trigger_configuration(invalid)
    credentials = StaticCredentialProvider[Credentials]({})
    with pytest.raises(ValueError, match="pollInterval"):
        Client(Config(poll_interval_seconds=0.001), credentials)
    with pytest.raises(ValueError, match="pollPageSize"):
        Client(Config(poll_page_size=101), credentials)


async def test_a_burst_larger_than_the_cursor_bound_is_delivered_without_loss_or_repeats() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 100)
    cursor = await fetcher.seed_cursor(TriggerContext())
    burst_at = SEED_TIME + timedelta(minutes=1)
    table.put(*burst_records("Burst", 2600, burst_at))
    delivered, cursor = await drain_ids(fetcher, cursor, 200)
    assert len(delivered) == 2600 and set(delivered.values()) == {1}, "a bulk edit is delivered once"
    table.put(fake_record("recAfterBurst0001", burst_at + timedelta(seconds=1)))
    delivered, _ = await drain_ids(fetcher, cursor, 50)
    assert delivered == Counter(["recAfterBurst0001"]), "the poll is not stuck behind the delivered burst"


async def test_runs_of_delivered_records_longer_than_one_fetch_resume_from_the_stored_offset() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 10)
    cursor = await fetcher.seed_cursor(TriggerContext())
    burst_at = SEED_TIME + timedelta(minutes=1)
    table.put(*burst_records("Run", 250, burst_at))
    delivered, cursor = await drain_ids(fetcher, cursor, 200)
    assert len(delivered) == 250
    # The next poll rescans the overlap: 250 remembered records, 25 pages, more than one fetch's 20.
    table.put(fake_record("recAfterRun000001", burst_at + timedelta(seconds=2)))
    delivered, _ = await drain_ids(fetcher, cursor, 20)
    assert delivered == Counter(["recAfterRun000001"])


async def test_a_change_that_becomes_visible_late_inside_the_overlap_is_delivered() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 100)
    cursor = await fetcher.seed_cursor(TriggerContext())
    table.put(fake_record("recLaterStamp0001", SEED_TIME + timedelta(seconds=4)))
    delivered, cursor = await drain_ids(fetcher, cursor, 10)
    assert delivered == Counter(["recLaterStamp0001"])
    table.put(fake_record("recEarlierStamp01", SEED_TIME + timedelta(milliseconds=500)))
    delivered, cursor = await drain_ids(fetcher, cursor, 10)
    assert delivered == Counter(["recEarlierStamp01"])
    delivered, _ = await drain_ids(fetcher, cursor, 10)
    assert not delivered, "remembered records inside the overlap are not delivered again"


async def test_an_expired_offset_restarts_the_scan_without_loss_or_repeats() -> None:
    table = AirtableTableFake()
    fetcher = new_fetcher(table, 10)
    cursor = await fetcher.seed_cursor(TriggerContext())
    table.put(*burst_records("Expire", 35, SEED_TIME + timedelta(minutes=1)))
    delivered: Counter[str] = Counter()
    for _ in range(30):
        table.expire_iterators()
        ids, cursor, has_more = await fetch_ids(fetcher, cursor)
        delivered.update(ids)
        if not has_more and not ids:
            break
    assert len(delivered) == 35 and set(delivered.values()) == {1}


async def test_seed_hides_existing_records_beyond_its_first_page() -> None:
    table = AirtableTableFake(*burst_records("Existing", 250, SEED_TIME))
    fetcher = new_fetcher(table, 100)
    cursor = await fetcher.seed_cursor(TriggerContext())
    table.put(fake_record("recNewAfterSeed01", SEED_TIME + timedelta(seconds=1)))
    delivered, _ = await drain_ids(fetcher, cursor, 20)
    assert delivered == Counter(["recNewAfterSeed01"]), "records present at seed never start Flows"


# SHA-256 digests of every committed cursor token and every event ID, one per line, that the Go fetcher
# produced for the same scenarios against its internal/airtablefake table.
GO_PARITY_DIGESTS = {
    "burst": (
        33,
        "a21653a4a592406a5bf113fd3c31a9692dc1b28d724c40fe2ccca32ede34b049",
        2601,
        "b5238cc5afbf9c62357fe9d2cd9e8779a5ab54d4742ea36a037da8b2add85646",
    ),
    "run": (
        32,
        "b0f4c0f6028fa0e8ae50e7044710c3262446573cc78affe81e8cafdef2d3d12c",
        251,
        "4db52c52614551b2895a9c05137f3ea0decf82f4b77372462781c5d4a3a080eb",
    ),
    "expire": (
        6,
        "d178370ba86b9c7ac2268c9915dfe7f88a68c94ef7810fc8ed8b29204b4e045b",
        35,
        "f3abe6225501b0583466d4a4163caf751d5c8c4ae0adf4918b43ce8f80e39cee",
    ),
    "seedpage": (
        6,
        "9848e5c08d99c1cb76995743409a1e422d59e193e2e503e4bbdf018a84fca076",
        30,
        "2a64097f148840dee41cfcd9e47cb77409b794a920babb5e46a263d9152f64b7",
    ),
}


class ParityRecorder:
    """Drains a fetcher like the Go parity scenarios and digests its tokens and event IDs."""

    def __init__(self, fetcher: RecordChangedFetcher, table: AirtableTableFake) -> None:
        self.fetcher, self.table = fetcher, table
        self.tokens: list[str] = []
        self.ids: list[str] = []

    async def seed(self) -> Cursor:
        cursor = await self.fetcher.seed_cursor(TriggerContext())
        self.tokens.append(cursor.token)
        return cursor

    async def drain(self, cursor: Cursor, expire_iterators: bool = False) -> Cursor:
        for _ in range(300):
            if expire_iterators:
                self.table.expire_iterators()
            page = await self.fetcher.fetch_changes(TriggerContext(), cursor)
            cursor = page.next_cursor
            self.tokens.append(cursor.token)
            self.ids.extend(event.id for event in page.changes)
            if not page.has_more_pages and not page.changes:
                return cursor
        raise AssertionError("not current")

    def digests(self) -> tuple[int, str, int, str]:
        return (
            len(self.tokens),
            hashlib.sha256("\n".join(self.tokens).encode()).hexdigest(),
            len(self.ids),
            hashlib.sha256("\n".join(self.ids).encode()).hexdigest(),
        )


async def test_cursor_tokens_and_event_ids_match_go_byte_for_byte() -> None:
    table = AirtableTableFake()
    burst = ParityRecorder(new_fetcher(table, 100), table)
    cursor = await burst.seed()
    at = SEED_TIME + timedelta(minutes=1, milliseconds=123)
    table.put(*burst_records("Burst", 2600, at))
    cursor = await burst.drain(cursor)
    table.put(fake_record("recAfterBurst0001", at + timedelta(milliseconds=1500)))
    await burst.drain(cursor)
    assert burst.tokens[-1] == (
        '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:01:01.623Z",'
        '"floor":"2026-11-15T09:01:00.124Z","seen":["recAfterBurst0001:0"]}'
    )
    assert burst.digests() == GO_PARITY_DIGESTS["burst"]

    table = AirtableTableFake()
    run = ParityRecorder(new_fetcher(table, 10), table)
    cursor = await run.seed()
    table.put(*burst_records("Run", 250, SEED_TIME + timedelta(minutes=1)))
    cursor = await run.drain(cursor)
    table.put(fake_record("recAfterRun000001", SEED_TIME + timedelta(minutes=1, seconds=2)))
    await run.drain(cursor)
    assert run.digests() == GO_PARITY_DIGESTS["run"]

    table = AirtableTableFake()
    expire = ParityRecorder(new_fetcher(table, 10), table)
    cursor = await expire.seed()
    table.put(*burst_records("Expire", 35, SEED_TIME + timedelta(minutes=1)))
    await expire.drain(cursor, expire_iterators=True)
    assert expire.digests() == GO_PARITY_DIGESTS["expire"]

    table = AirtableTableFake(
        *(
            fake_record(f"recExisting{index:06d}", SEED_TIME + timedelta(milliseconds=index % 7 * 317))
            for index in range(250)
        )
    )
    seed_page = ParityRecorder(new_fetcher(table, 100), table)
    cursor = await seed_page.seed()
    table.put(
        *(
            fake_record(f"recStagger{index:07d}", SEED_TIME + timedelta(seconds=1, milliseconds=index * 211))
            for index in range(30)
        )
    )
    cursor = await seed_page.drain(cursor)
    table.put(
        fake_record("recStagger0000003", SEED_TIME + timedelta(seconds=9)),
        fake_record("recLateVisible001", SEED_TIME + timedelta(seconds=5, milliseconds=7)),
    )
    await seed_page.drain(cursor)
    assert seed_page.digests() == GO_PARITY_DIGESTS["seedpage"]
