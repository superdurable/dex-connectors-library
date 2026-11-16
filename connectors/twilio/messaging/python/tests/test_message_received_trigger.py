# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fetcher parity with the Go messageReceived Trigger: baseline, filtering, holds, paging, and resets."""

from collections.abc import AsyncIterator
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    Cursor,
    RateLimitedError,
    is_cursor_invalid,
)

from dex_connectors.twilio_messaging import (
    Client,
    Config,
    Credentials,
    Message,
    MessageReceivedEvent,
    MessageReceivedTriggerConfiguration,
    validate_message_received_trigger_configuration,
)
from dex_connectors.twilio_messaging._inbound_message_fetcher import (
    InboundMessageFetcher,
    _InboundSnapshotKey,
    new_message_received_trigger_event,
)
from dex_connectors.twilio_messaging.triggers import _decode_message_received_payload
from tests.twilio_messages_fake import TwilioMessagesFake

# Split literals keep synthetic SIDs from matching repository secret scanning.
ACCOUNT_SID = "AC" + "00112233445566778899aabbccddeeff"
AUTH_TOKEN = "fetcher-auth-token-0123456789abcd"
NUMBER = "+14155550100"
OTHER_NUMBER = "+14155550199"
CUSTOMER = "+14155550123"
CONNECTION = ConnectionRef("twilio", "twilio-messaging")
START = datetime(2026, 11, 15, 12, 0, tzinfo=UTC)


@dataclass
class FetcherFixture:
    provider: TwilioMessagesFake
    client: Client
    now: datetime

    def fetcher(self, to: str) -> InboundMessageFetcher:
        return InboundMessageFetcher(self.client, CONNECTION, to)


def _new_client(
    provider: TwilioMessagesFake, http_client: httpx.AsyncClient, token: str, fixture: list[FetcherFixture]
) -> Client:
    return Client(
        Config(account_sid=ACCOUNT_SID, endpoint="http://127.0.0.1:24210/2010-04-01"),
        StaticCredentialProvider({CONNECTION: Credentials(auth_token=SecretString(token))}),
        http_client,
        now=lambda: fixture[0].now,
    )


@pytest.fixture
async def fixture() -> AsyncIterator[FetcherFixture]:
    provider = TwilioMessagesFake(ACCOUNT_SID, ACCOUNT_SID, AUTH_TOKEN)
    holder: list[FetcherFixture] = []
    async with httpx.AsyncClient(transport=provider.transport()) as http_client:
        holder.append(FetcherFixture(provider, _new_client(provider, http_client, AUTH_TOKEN, holder), START))
        yield holder[0]


async def fetch_all(
    fetcher: InboundMessageFetcher, cursor: Cursor
) -> tuple[list[TriggerEvent[MessageReceivedEvent]], Cursor]:
    events: list[TriggerEvent[MessageReceivedEvent]] = []
    for _ in range(20):
        page = await fetcher.fetch_changes(TriggerContext(), cursor)
        events.extend(page.changes)
        cursor = page.next_cursor
        if not page.has_more_pages:
            return events, cursor
    raise AssertionError("fetcher kept reporting more pages")


def event_ids(events: list[TriggerEvent[MessageReceivedEvent]]) -> list[str]:
    return [event.id for event in events]


async def test_seed_records_a_baseline_and_later_inbound_messages_arrive_once_oldest_first(
    fixture: FetcherFixture,
) -> None:
    fixture.provider.receive(CUSTOMER, NUMBER, "before the baseline", fixture.now - timedelta(seconds=30))
    fetcher = fixture.fetcher(NUMBER)
    cursor = await fetcher.seed_cursor(TriggerContext())
    assert (
        cursor.token
        == f'{{"accountSid":"{ACCOUNT_SID}","to":"{NUMBER}","watermark":{int(START.timestamp())}}}'
    )
    assert cursor.snapshot is not None and len(cursor.snapshot.entries) == 1

    fixture.now += timedelta(minutes=1)
    second = fixture.provider.receive(CUSTOMER, NUMBER, "STOP", fixture.now - timedelta(seconds=10))
    first = fixture.provider.receive(CUSTOMER, NUMBER, "hello", fixture.now - timedelta(seconds=20))
    fixture.provider.send(NUMBER, CUSTOMER, "outbound reply", fixture.now - timedelta(seconds=5))
    fixture.provider.receive(CUSTOMER, OTHER_NUMBER, "to another number", fixture.now - timedelta(seconds=5))
    events, cursor = await fetch_all(fetcher, cursor)
    assert event_ids(events) == ["twilio-message-" + first, "twilio-message-" + second]
    assert events[0].payload.body == "hello"
    assert events[0].payload.message.from_ == CUSTOMER
    assert events[0].payload.message.status == "received"
    assert events[0].occurred_at == fixture.now - timedelta(seconds=20)

    page = await fetcher.fetch_changes(TriggerContext(), cursor)
    assert list(page.changes) == [], "delivered messages in the lookback are never repeated"
    assert page.next_cursor == cursor, "an empty poll leaves the cursor unchanged, so the kit writes nothing"
    for query in fixture.provider.list_requests:
        assert query["To"] == NUMBER
        assert query["PageSize"] == "200"
        assert query["DateSent>"]


async def test_receiving_message_waits_until_received_and_holds_the_watermark(
    fixture: FetcherFixture,
) -> None:
    fetcher = fixture.fetcher("")
    cursor = await fetcher.seed_cursor(TriggerContext())
    fixture.now += timedelta(minutes=1)
    mms = fixture.provider.receive(
        CUSTOMER, NUMBER, "photo", fixture.now - timedelta(seconds=50), "receiving"
    )
    text = fixture.provider.receive(CUSTOMER, OTHER_NUMBER, "text", fixture.now - timedelta(seconds=5))
    events, cursor = await fetch_all(fetcher, cursor)
    assert event_ids(events) == ["twilio-message-" + text]

    fixture.now += timedelta(minutes=5)
    fixture.provider.set_status(mms, "received")
    events, _ = await fetch_all(fetcher, cursor)
    assert event_ids(events) == ["twilio-message-" + mms], "the held watermark still lists the finished MMS"


async def test_receiving_message_is_delivered_as_is_after_the_hold_limit(fixture: FetcherFixture) -> None:
    fetcher = fixture.fetcher("")
    cursor = await fetcher.seed_cursor(TriggerContext())
    fixture.now += timedelta(minutes=1)
    mms = fixture.provider.receive(
        CUSTOMER, NUMBER, "photo", fixture.now - timedelta(seconds=50), "receiving"
    )
    events, cursor = await fetch_all(fetcher, cursor)
    assert events == []
    fixture.now += timedelta(minutes=10)
    events, _ = await fetch_all(fetcher, cursor)
    assert event_ids(events) == ["twilio-message-" + mms]
    assert events[0].payload.message.status == "receiving"


async def test_many_messages_span_provider_pages_and_change_pages(fixture: FetcherFixture) -> None:
    fetcher = fixture.fetcher(NUMBER)
    cursor = await fetcher.seed_cursor(TriggerContext())
    fixture.now += timedelta(minutes=1)
    for index in range(650):
        fixture.provider.receive(CUSTOMER, NUMBER, "bulk", fixture.now - timedelta(seconds=index % 60))
    page = await fetcher.fetch_changes(TriggerContext(), cursor)
    assert len(page.changes) == MAXIMUM_CHANGES_PER_PAGE
    assert page.has_more_pages
    rest, _ = await fetch_all(fetcher, page.next_cursor)
    assert len(rest) == 150
    identifiers = event_ids([*page.changes, *rest])
    assert len(set(identifiers)) == 650, "no message is delivered twice"


async def test_date_only_filters_stop_at_the_first_message_before_the_lookback(
    fixture: FetcherFixture,
) -> None:
    fixture.provider.use_date_only_filters()
    for index in range(2500):
        sent_at = fixture.now - timedelta(hours=1, seconds=index)
        fixture.provider.send(NUMBER, CUSTOMER, "earlier today", sent_at)
    fetcher = fixture.fetcher("")
    cursor = await fetcher.seed_cursor(TriggerContext())
    fixture.now += timedelta(minutes=1)
    reply = fixture.provider.receive(CUSTOMER, NUMBER, "reply", fixture.now - timedelta(seconds=10))
    before = len(fixture.provider.list_requests)
    events, _ = await fetch_all(fetcher, cursor)
    assert event_ids(events) == ["twilio-message-" + reply]
    assert len(fixture.provider.list_requests) - before == 1, (
        "one page reaches messages older than the lookback"
    )


async def test_busy_window_narrows_toward_the_watermark_and_a_held_window_waits_for_the_next_poll(
    fixture: FetcherFixture,
) -> None:
    fetcher = fixture.fetcher("")
    cursor = await fetcher.seed_cursor(TriggerContext())
    baseline = fixture.now
    fixture.now += timedelta(minutes=5)
    held = fixture.provider.receive(CUSTOMER, NUMBER, "photo", baseline + timedelta(seconds=10), "receiving")
    early = fixture.provider.receive(CUSTOMER, NUMBER, "early", baseline + timedelta(seconds=5))
    for index in range(2100):
        fixture.provider.send(NUMBER, CUSTOMER, "campaign", fixture.now - timedelta(seconds=index % 300))
    page = await fetcher.fetch_changes(TriggerContext(), cursor)
    assert event_ids(list(page.changes)) == ["twilio-message-" + early], "a busy window narrows"
    assert not page.has_more_pages, "relisting a held window would return the same messages"

    fixture.provider.set_status(held, "received")
    events, _ = await fetch_all(fetcher, page.next_cursor)
    assert event_ids(events) == ["twilio-message-" + held]


async def test_rate_limit_invalid_cursor_and_rejected_credentials(fixture: FetcherFixture) -> None:
    fetcher = fixture.fetcher(NUMBER)
    cursor = await fetcher.seed_cursor(TriggerContext())

    fixture.provider.throttle_next_list(7)
    with pytest.raises(RateLimitedError) as rate_limited:
        await fetcher.fetch_changes(TriggerContext(), cursor)
    assert rate_limited.value.retry_after == timedelta(seconds=7)
    assert "20429" in str(rate_limited.value)

    with pytest.raises(Exception) as changed_address:
        await fixture.fetcher(OTHER_NUMBER).fetch_changes(TriggerContext(), cursor)
    assert is_cursor_invalid(changed_address.value), "a changed receiving address records a new baseline"
    for token in ("not json", "null", "[]", '{"watermark":1.5}', '{"watermark":0}', '{"watermark":"1"}'):
        with pytest.raises(Exception) as undecodable:
            await fetcher.fetch_changes(TriggerContext(), Cursor(token))
        assert is_cursor_invalid(undecodable.value), token

    async with httpx.AsyncClient(transport=fixture.provider.transport()) as http_client:
        rejected = _new_client(fixture.provider, http_client, "wrong-token-0123456789abcdef0123", [fixture])
        with pytest.raises(RuntimeError) as error:
            await InboundMessageFetcher(rejected, CONNECTION, "").seed_cursor(TriggerContext())
    assert "HTTP 401" in str(error.value)
    assert "wrong-token" not in str(error.value)


async def test_cursor_decoding_follows_go_json_unmarshal(fixture: FetcherFixture) -> None:
    fetcher = fixture.fetcher(NUMBER)
    token = (
        f'{{"ACCOUNTSID":"{ACCOUNT_SID}","to":"{OTHER_NUMBER}","To":"{NUMBER}",'
        f'"extra":1,"watermark":{int(START.timestamp())}}}'
    )
    page = await fetcher.fetch_changes(TriggerContext(), Cursor(token))
    assert list(page.changes) == []
    assert (
        page.next_cursor.token
        == f'{{"accountSid":"{ACCOUNT_SID}","to":"{NUMBER}","watermark":{int(START.timestamp())}}}'
    )


def test_message_received_configuration_validation() -> None:
    for valid in ("", NUMBER, "whatsapp:" + NUMBER):
        validate_message_received_trigger_configuration(MessageReceivedTriggerConfiguration(to=valid))
    for invalid in (" +14155550100", "4155550100", "MG0123", "whatsapp:4155550100"):
        with pytest.raises(ValueError):
            validate_message_received_trigger_configuration(MessageReceivedTriggerConfiguration(to=invalid))
    with pytest.raises(ValueError, match="pollInterval must be at least 5s"):
        Client(Config(account_sid=ACCOUNT_SID, poll_interval_seconds=1), StaticCredentialProvider({}))


# Printed by the Go connector for the same inputs, so Go and Python replicas share cursors and inboxes.
# Split SID literals keep the fixtures from matching repository secret scanning.
GO_SEED_TOKEN = (
    '{"accountSid":"AC'
    + '00112233445566778899aabbccddeeff","to":"whatsapp:+14155550100","watermark":1794744000}'
)
GO_NEXT_TOKEN = (
    '{"accountSid":"AC'
    + '00112233445566778899aabbccddeeff","to":"whatsapp:+14155550100","watermark":1794744060}'
)
GO_SNAPSHOT_KEY = "01794744000-SM0123456789abcdef0123456789abcdef"
GO_SNAPSHOT_HASH = "5c9cf9e4e827f98f"
GO_FIRST_QUERY = "DateSent%3E=2026-11-15T11%3A58%3A00Z&PageSize=200&To=whatsapp%3A%2B14155550100"
GO_EVENT_PAYLOAD = (
    '{"message":{"sid":"SM0123456789abcdef0123456789abcdef","accountSid":"AC'
    + '00112233445566778899aabbccddeeff",'
    '"from":"+14155550123","to":"+14155550100","status":"received","direction":"inbound","segmentCount":1,'
    '"createdAt":"2026-11-15T12:00:00Z","sentAt":"2026-11-15T12:00:00Z","updatedAt":"2026-11-15T12:00:00Z"},'
    '"body":"Hi \\u003c\\u0026\\u003e"}'
)


async def test_cursor_tokens_snapshot_keys_and_requests_match_go(fixture: FetcherFixture) -> None:
    fetcher = fixture.fetcher("whatsapp:" + NUMBER)
    seeded = await fetcher.seed_cursor(TriggerContext())
    assert seeded.token == GO_SEED_TOKEN
    assert seeded.snapshot is not None and seeded.snapshot.entries == ()
    request = fixture.provider.list_requests[0]
    assert request == {"DateSent>": "2026-11-15T11:58:00Z", "PageSize": "200", "To": "whatsapp:" + NUMBER}
    item = _InboundSnapshotKey(GO_SNAPSHOT_KEY)
    assert item.snapshot_hash() == GO_SNAPSHOT_HASH
    following = fetcher._encode_cursor(1794744060, None, [item])
    assert following.token == GO_NEXT_TOKEN
    assert following.snapshot is not None
    assert [(entry.key, entry.hash, entry.seen) for entry in following.snapshot.entries] == [
        (GO_SNAPSHOT_KEY, GO_SNAPSHOT_HASH, 0)
    ]
    pruned = fetcher._encode_cursor(1794744000 + 121, following.snapshot, [])
    assert pruned.snapshot is not None and pruned.snapshot.entries == (), "keys before the lookback expire"


def test_go_query_encoding_matches() -> None:
    from dex_connectors.twilio_messaging._inbound_message_fetcher import _encode_go_query

    query = {"PageSize": "200", "DateSent>": "2026-11-15T11:58:00Z", "To": "whatsapp:+14155550100"}
    assert _encode_go_query(query) == GO_FIRST_QUERY


def test_go_recorded_event_replays_in_python() -> None:
    sent_at = datetime(2026, 11, 15, 12, 0, tzinfo=UTC)
    message = Message(
        sid="SM0123456789abcdef0123456789abcdef",
        account_sid=ACCOUNT_SID,
        from_=CUSTOMER,
        to=NUMBER,
        status="received",
        direction="inbound",
        segment_count=1,
        created_at=sent_at,
        sent_at=sent_at,
        updated_at=sent_at,
    )
    event = new_message_received_trigger_event(message, "Hi <&>", START + timedelta(hours=1))
    assert event.id == "twilio-message-SM0123456789abcdef0123456789abcdef"
    assert event.occurred_at == sent_at
    assert _decode_message_received_payload(GO_EVENT_PAYLOAD, GO_EVENT_PAYLOAD.encode()) == event.payload
    encoded = CodecRegistry().resolve(MessageReceivedEvent).encode(event.payload)
    assert isinstance(encoded, Value) and encoded.kind == WireKind.JSON
    undated = new_message_received_trigger_event(Message(sid=message.sid), "", START)
    assert undated.occurred_at == START, "a message without dates occurs at the poll time"
