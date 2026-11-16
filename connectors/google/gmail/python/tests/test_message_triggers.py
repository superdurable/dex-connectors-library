# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Gmail message Trigger poller, at parity with the Go connector's trigger_runner tests."""

import asyncio
import json
from collections.abc import AsyncIterator
from dataclasses import dataclass, field
from datetime import UTC, datetime
from uuid import NAMESPACE_URL, uuid5

import httpx
import pytest
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerEvent,
    mark_trigger_undeliverable,
)

from dex_connectors.google_gmail import MessageEvent, MessageMatcher
from dex_connectors.google_gmail._trigger_source import MessagePollingTriggerSource
from tests.gmail_inbox_fake import (
    CONNECTION_REFERENCE,
    FakeGmail,
    RecordingLogger,
    fake_http_client,
    fake_provider,
    reply_message,
    root_message,
)


@dataclass
class RecordingTarget:
    """Records delivered event IDs and fails or rejects events as scripted."""

    order: list[str] | None = None
    delivered: list[TriggerEvent[MessageEvent]] = field(default_factory=list)
    attempts: list[str] = field(default_factory=list)
    failures: dict[str, int] = field(default_factory=dict)
    undeliverable: set[str] = field(default_factory=set)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[MessageEvent]) -> None:
        self.attempts.append(event.id)
        remaining = self.failures.get(event.id, 0)
        if remaining > 0:
            self.failures[event.id] = remaining - 1
            raise ConnectionError("Dex is unavailable")
        if event.id in self.undeliverable:
            raise mark_trigger_undeliverable(LookupError("Flow is closed"))
        self.delivered.append(event)
        if self.order is not None:
            self.order.append(event.id)

    def ids(self) -> list[str]:
        return [event.id for event in self.delivered]


@pytest.fixture
async def gmail_http() -> AsyncIterator[tuple[FakeGmail, httpx.AsyncClient]]:
    gmail = FakeGmail()
    async with fake_http_client(gmail) as http_client:
        yield gmail, http_client


def new_source(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
    logger: RecordingLogger,
    *,
    requires_reply: bool = False,
    search_query: str = "",
    matcher: MessageMatcher | None = None,
    binding_name: str = "binding-1",
) -> MessagePollingTriggerSource:
    gmail, http_client = gmail_http
    return MessagePollingTriggerSource(
        fake_provider(gmail, http_client, logger),
        CONNECTION_REFERENCE,
        "replyReceived" if requires_reply else "messageReceived",
        binding_name,
        search_query,
        matcher,
        requires_reply,
    )


async def scan_quietly(
    source: MessagePollingTriggerSource, target: RecordingTarget, context: TriggerContext | None = None
) -> None:
    try:
        await source.scan(context or TriggerContext(), target)
    except Exception:  # noqa: BLE001
        # The next scan retries.
        return


async def test_lists_the_inbox_like_go_and_delivers_matching_roots_oldest_first(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, _ = gmail_http
    gmail.receive(root_message("m1", "t1", internal_date="1767312000120"))
    gmail.receive(root_message("m2", "t2", snippet=""))
    target = RecordingTarget()
    await new_source(gmail_http, RecordingLogger(), search_query="label:inbox is:unread").scan(
        TriggerContext(), target
    )
    assert target.ids() == ["m1", "m2"]
    first = target.delivered[0]
    assert first.occurred_at == datetime(2026, 1, 2, 0, 0, 0, 120000, tzinfo=UTC)
    assert first.payload == MessageEvent(
        primary_email="owner@example.com",
        message_id="m1",
        thread_id="t1",
        from_="Ada <ada@example.com>",
        subject="Order question",
        snippet="Please check order 1042",
        received_at=datetime(2026, 1, 2, 0, 0, 0, 120000, tzinfo=UTC),
        is_reply=False,
    )
    assert target.delivered[1].payload.snippet == ""
    assert str(gmail.requests[0].url) == (
        "https://gmail.example/gmail/v1/users/me/messages?labelIds=INBOX&maxResults=25"
        "&q=label%3Ainbox+is%3Aunread"
    )
    assert str(gmail.requests[1].url) == (
        "https://gmail.example/gmail/v1/users/me/messages/m1?format=metadata&metadataHeaders=Message-ID"
        "&metadataHeaders=In-Reply-To&metadataHeaders=References&metadataHeaders=From"
        "&metadataHeaders=Reply-To&metadataHeaders=To&metadataHeaders=Subject"
    )
    assert all(
        request.method == "GET" and request.headers["Authorization"] == "Bearer SENTINEL-ACCESS"
        for request in gmail.requests
    )


async def test_reads_each_listed_message_once_and_ignores_with_a_reason(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, _ = gmail_http
    gmail.receive(root_message("root", "thread-1"))
    gmail.receive(reply_message("reply-other", "thread-1", sender="Eve <eve@example.com>"))
    gmail.receive(reply_message("reply-text", "thread-1", snippet="not yet"))
    gmail.receive(reply_message("reply-match", "thread-1", sender="ADA@EXAMPLE.COM"))
    logger = RecordingLogger()
    target = RecordingTarget()
    source = new_source(
        gmail_http,
        logger,
        requires_reply=True,
        matcher=MessageMatcher(message_contains="APPROVED", sender_emails=("Ada <ada@example.com>",)),
    )
    await source.scan(TriggerContext(), target)
    await source.scan(TriggerContext(), target)
    assert target.ids() == ["reply-match"]
    assert gmail.reads() == ["root", "reply-other", "reply-text", "reply-match"]
    ignored = logger.find("trigger event ignored")
    assert [(record.attributes["event_id"], record.attributes["reason"]) for record in ignored] == [
        ("root", "not_a_reply"),
        ("reply-other", "matcher_mismatch"),
        ("reply-text", "matcher_mismatch"),
    ]
    assert ignored[0].attributes == {
        "connector": "gmail",
        "connection": "gmail-inbox",
        "trigger": "replyReceived",
        "binding": "binding-1",
        "thread_id": "thread-1",
        "event_id": "root",
        "reason": "not_a_reply",
    }


async def test_root_source_ignores_replies(gmail_http: tuple[FakeGmail, httpx.AsyncClient]) -> None:
    gmail, _ = gmail_http
    gmail.receive(root_message("root", "t1"))
    gmail.receive(reply_message("reply", "t1", in_reply_to="", references="<t1@mail.example.com>"))
    logger = RecordingLogger()
    target = RecordingTarget()
    await new_source(gmail_http, logger).scan(TriggerContext(), target)
    assert target.ids() == ["root"]
    assert logger.find("trigger event ignored", event_id="reply", reason="not_a_root")


async def test_consumes_an_undeliverable_reply_and_delivers_the_rest_of_the_page(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, _ = gmail_http
    for index in (1, 2, 3):
        gmail.receive(reply_message(f"r{index}", f"t{index}"))
    logger = RecordingLogger()
    target = RecordingTarget(undeliverable={"r2"})
    source = new_source(gmail_http, logger, requires_reply=True)
    await source.scan(TriggerContext(), target)
    await source.scan(TriggerContext(), target)
    assert target.ids() == ["r1", "r3"]
    assert target.attempts == ["r1", "r2", "r3"]
    assert len(logger.find("trigger event skipped: undeliverable", event_id="r2", thread_id="t2")) == 1


async def test_retries_a_failed_message_first_after_newer_mail_pushes_it_off_the_page(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, _ = gmail_http
    logger = RecordingLogger()
    source = new_source(gmail_http, logger, binding_name="")
    gmail.receive(root_message("old", "t-old"))
    target = RecordingTarget(failures={"old": 1})
    with pytest.raises(ConnectionError, match="Dex is unavailable"):
        await source.scan(TriggerContext(), target)
    for index in range(25):
        gmail.receive(root_message(f"new-{index}", f"t-{index}"))
    await source.scan(TriggerContext(), target)
    assert target.ids()[0] == "old"
    assert len(target.ids()) == 26
    assert logger.find(
        "trigger delivery failed; retrying",
        event_id="old",
        attempt=1,
        delay_ms=1000,
        error_type="ConnectionError",
    )
    assert logger.find("trigger delivered after retry", event_id="old", attempts=2, thread_id="t-old")
    assert all("binding" not in record.attributes for record in logger.records)


async def test_does_not_report_an_event_a_runner_consumed_as_undeliverable_as_delivered_after_retry(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    from dex_connectors.sdk import TriggerBindingRef, TriggerConfig, new_trigger

    from dex_connectors.google_gmail import MESSAGE_RECEIVED_TRIGGER_DEFINITION

    gmail, _ = gmail_http
    gmail.receive(root_message("m1", "t1"))
    logger = RecordingLogger()
    target = RecordingTarget(failures={"m1": 1})
    source = new_source(gmail_http, logger)

    class TwoScans:
        async def run(self, context: TriggerContext, wrapped: RecordingTarget) -> None:
            await scan_quietly(source, wrapped, context)
            target.undeliverable.add("m1")
            await source.scan(context, wrapped)

    runner = new_trigger(
        TriggerConfig(
            MESSAGE_RECEIVED_TRIGGER_DEFINITION,
            TriggerBindingRef(CONNECTION_REFERENCE, MESSAGE_RECEIVED_TRIGGER_DEFINITION.trigger, "b"),
            TwoScans(),  # type: ignore[arg-type]
            target,
            logger,
        )
    )
    await runner.run(TriggerContext())
    assert len(logger.find("trigger event skipped: undeliverable", event_id="m1")) == 1
    assert not logger.find("trigger delivered after retry")


async def test_counts_consecutive_failed_polls_and_logs_safe_errors(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    gmail, _ = gmail_http
    gmail.receive(root_message("m1", "t1"))
    gmail.list_failures.extend([500, 503])
    gmail.read_failures["m1"] = 500
    logger = RecordingLogger()
    target = RecordingTarget()
    source = new_source(gmail_http, logger, search_query="from:SENTINEL-QUERY")
    for _ in range(3):
        await scan_quietly(source, target)
    assert target.ids() == []
    failures = logger.find("gmail poll failed; retrying")
    assert [
        (record.attributes["attempt"], record.attributes["error"], record.attributes.get("event_id", ""))
        for record in failures
    ] == [
        (1, "Gmail message list is unavailable", ""),
        (2, "Gmail message list is unavailable", ""),
        (3, "Gmail message lookup failed", "m1"),
    ]
    assert failures[0].attributes["delay_ms"] == 1000
    await source.scan(TriggerContext(), target)
    gmail.list_failures.append(500)
    await scan_quietly(source, target)
    assert logger.find("gmail poll failed; retrying")[-1].attributes["attempt"] == 1
    assert target.ids() == ["m1"]
    recorded = json.dumps([record.attributes for record in logger.records])
    assert "SENTINEL" not in recorded and "Order question" not in recorded and "ada@example" not in recorded


async def test_a_cancelled_poll_raises_the_abort_without_logging_a_failure(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    from dex_connectors.sdk import TriggerDeliveryAbortedError

    gmail, _ = gmail_http
    gmail.list_failures.append(500)
    logger = RecordingLogger()
    cancellation = TriggerCancellation()
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await new_source(gmail_http, logger).scan(TriggerContext(cancellation), RecordingTarget())
    assert not logger.find("gmail poll failed; retrying")


def test_reads_credentials_under_the_go_connector_stable_call_id() -> None:
    identity = (
        "https://superdurable.dev/dex-connectors/trigger-credential/v1/google/gmail-inbox/replyReceived"
    )
    assert str(uuid5(NAMESPACE_URL, identity)) == "87132e3f-e8be-557c-a0f9-f3e95b2f70ad"


async def test_credential_calls_use_the_stable_call_id(
    gmail_http: tuple[FakeGmail, httpx.AsyncClient],
) -> None:
    source = new_source(gmail_http, RecordingLogger(), requires_reply=True)
    assert str(source._call.id) == "87132e3f-e8be-557c-a0f9-f3e95b2f70ad"
    assert source._call.operation.operation_id == "replyReceived"
    assert source._call.operation.connector_id == "gmail"


async def test_wait_for_poll_interval_returns_when_cancelled() -> None:
    from datetime import timedelta

    from dex_connectors.sdk import TriggerDeliveryAbortedError

    from dex_connectors.google_gmail._trigger_source import wait_for_poll_interval

    cancellation = TriggerCancellation()
    waiting = asyncio.create_task(wait_for_poll_interval(cancellation, timedelta(hours=1)))
    await asyncio.sleep(0)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await asyncio.wait_for(waiting, 5)
