# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The memberChanged fetcher's baseline, lookback, paging, filter, and failure rules, as Go tests them."""

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
)
from dex_connectors.sdk.pollingtrigger import Cursor, CursorInvalidError, RateLimitedError

from dex_connectors.intuit_mailchimp import (
    MEMBER_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    MemberChangedTriggerConfiguration,
    validate_member_changed_trigger_configuration,
)
from dex_connectors.intuit_mailchimp._member_change_fetcher import MEMBER_POLL_PAGE_SIZE, MemberChangeFetcher
from dex_connectors.intuit_mailchimp._member_snapshot import member_change_id
from tests.mailchimp_audience_fake import API_BASE_URL, API_KEY, Failure, MailchimpAudienceFake

AUDIENCE_ID = "57afe96172"
REFERENCE = ConnectionRef("mailchimp", "mailchimp-fetcher-test")
CONTEXT = TriggerContext(TriggerCancellation())


@asynccontextmanager
async def fetcher_for(audience: MailchimpAudienceFake, *statuses: str) -> AsyncIterator[MemberChangeFetcher]:
    async with httpx.AsyncClient(transport=httpx.MockTransport(audience.handle)) as http_client:
        client = Client(
            Config(),
            StaticCredentialProvider({REFERENCE: Credentials(SecretString(API_KEY))}),
            http_client,
            api_base_url=API_BASE_URL,
        )
        configuration = MemberChangedTriggerConfiguration(audience_id=AUDIENCE_ID, statuses=statuses)
        yield MemberChangeFetcher(client, REFERENCE, MEMBER_CHANGED_TRIGGER_DEFINITION.trigger, configuration)


async def drain(fetcher: MemberChangeFetcher, cursor: Cursor) -> tuple[list[str], Cursor]:
    """Fetch pages until the pass ends and return the delivered emails and the final cursor."""
    emails: list[str] = []
    for _ in range(100):
        page = await fetcher.fetch_changes(CONTEXT, cursor)
        for change in page.changes:
            assert change.id == member_change_id(AUDIENCE_ID, change.payload.member)
            emails.append(change.payload.member.email_address)
        cursor = page.next_cursor
        if not page.has_more_pages:
            return emails, cursor
    raise AssertionError("the pass never ended")


async def test_seed_of_an_empty_audience_commits_no_snapshot_as_go_does() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert cursor.token == '{"audienceId":"57afe96172"}'
        assert cursor.snapshot is None, "Go's cursor document holds a null snapshot"
        audience.upsert("ben@example.com", "subscribed", "Ben")
        emails, cursor = await drain(fetcher, cursor)
        assert emails == ["ben@example.com"]
        assert cursor.snapshot is not None


async def test_seed_records_a_baseline_and_later_changes_arrive_once() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    audience.upsert("ben@example.com", "subscribed", "Ben")
    audience.upsert("cara@example.com", "subscribed", "Cara")
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert '"baseline":"2026-11-01T09:00:02Z"' in cursor.token
        emails, cursor = await drain(fetcher, cursor)
        assert emails == [], "the baseline is never delivered"
        audience.upsert("dana@example.com", "pending", "Dana")
        audience.upsert("ben@example.com", "unsubscribed", "Ben")
        emails, cursor = await drain(fetcher, cursor)
        assert emails == ["dana@example.com", "ben@example.com"]
        emails, _ = await drain(fetcher, cursor)
        assert emails == [], "a pass over the lookback window delivers each version once"


async def test_changes_in_the_baseline_second_after_the_seed_are_delivered() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    seeded_at = datetime(2026, 11, 1, 9, 30, tzinfo=UTC)
    audience.upsert_at("ben@example.com", "subscribed", "Ben", seeded_at)
    audience.upsert_at("cara@example.com", "subscribed", "Cara", seeded_at)
    audience.upsert_at("old@example.com", "subscribed", "Old", seeded_at - timedelta(minutes=1))
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert cursor.snapshot is not None and len(cursor.snapshot.entries) == 2
        audience.upsert_at("dana@example.com", "subscribed", "Dana", seeded_at)
        audience.upsert_at("ben@example.com", "subscribed", "Benjamin", seeded_at)
        emails, cursor = await drain(fetcher, cursor)
        assert emails == ["ben@example.com", "dana@example.com"], "new versions in the baseline second arrive"
        assert (await drain(fetcher, cursor))[0] == []


async def test_seed_of_a_full_page_in_one_second_skips_that_second() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    imported_at = datetime(2026, 11, 1, 10, tzinfo=UTC)
    for index in range(MEMBER_POLL_PAGE_SIZE + 3):
        audience.upsert_at(f"import{index:04d}@example.com", "subscribed", "Import", imported_at)
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        assert '"baseline":"2026-11-01T10:00:01Z"' in cursor.token
        assert (await drain(fetcher, cursor))[0] == [], "no contact of the imported second is delivered"


async def test_late_listed_change_inside_the_lookback_is_delivered() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    audience.upsert("ben@example.com", "subscribed", "Ben")
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        for _ in range(10):
            audience.upsert("cara@example.com", "subscribed", "Cara")
        _, cursor = await drain(fetcher, cursor)
        audience.upsert_at("late@example.com", "subscribed", "Late", audience.clock - timedelta(seconds=3))
        assert (await drain(fetcher, cursor))[0] == ["late@example.com"]


async def test_contacts_sharing_one_second_page_by_offset_and_arrive_once() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        imported_at = datetime(2026, 11, 1, 10, tzinfo=UTC)
        for index in range(2 * MEMBER_POLL_PAGE_SIZE + 7):
            changed_at = imported_at + timedelta(seconds=index // 300)
            audience.upsert_at(f"import{index:04d}@example.com", "subscribed", "Import", changed_at)
        emails, cursor = await drain(fetcher, cursor)
        assert len(emails) == 2 * MEMBER_POLL_PAGE_SIZE + 7
        assert len(set(emails)) == len(emails), "no contact is delivered twice"
        assert '"watermark":"2026-11-01T10:00:01Z"' in cursor.token
        assert "passOffset" not in cursor.token, "a finished pass keeps no offset"


async def test_status_filter_skips_other_statuses_but_passes_them() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    async with fetcher_for(audience, "unsubscribed", "cleaned") as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        audience.upsert("ben@example.com", "subscribed", "Ben")
        audience.upsert("cara@example.com", "unsubscribed", "Cara")
        emails, cursor = await drain(fetcher, cursor)
        assert emails == ["cara@example.com"]
        audience.upsert("ben@example.com", "cleaned", "Ben")
        assert (await drain(fetcher, cursor))[0] == ["ben@example.com"]


async def test_provider_failures_are_classified_for_the_kit() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        audience.fail_next_list(Failure(429, retry_after="7"))
        with pytest.raises(RateLimitedError) as limited:
            await fetcher.fetch_changes(CONTEXT, cursor)
        assert limited.value.retry_after == timedelta(seconds=7)
        audience.fail_next_list(Failure(403, without_document=True))
        with pytest.raises(RateLimitedError) as limited:
            await fetcher.fetch_changes(CONTEXT, cursor)
        assert limited.value.retry_after == timedelta(0), "a document-less 403 is Mailchimp throttling"
        audience.fail_next_list(Failure(400))
        with pytest.raises(CursorInvalidError):
            await fetcher.fetch_changes(CONTEXT, cursor)
        for status in (403, 404, 503):
            audience.fail_next_list(Failure(status))
            with pytest.raises(RuntimeError) as failed:
                await fetcher.fetch_changes(CONTEXT, cursor)
            assert not isinstance(failed.value, CursorInvalidError | RateLimitedError), status
            assert API_KEY not in str(failed.value)


@pytest.mark.parametrize(
    "token",
    [
        "not json",
        '{"audienceId":"0000000000"}',
        '{"audienceId":"57afe96172","watermark":"yesterday"}',
        '{"audienceId":"57afe96172","passOffset":-1}',
    ],
)
async def test_foreign_or_corrupt_cursor_is_invalid(token: str) -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    async with fetcher_for(audience) as fetcher:
        with pytest.raises(CursorInvalidError):
            await fetcher.fetch_changes(CONTEXT, Cursor(token))
    assert audience.requests == [], "an invalid cursor sends no request"


async def test_poll_requests_ask_for_ascending_changes_after_the_lookback() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    audience.upsert("ben@example.com", "subscribed", "Ben")
    async with fetcher_for(audience) as fetcher:
        cursor = await fetcher.seed_cursor(CONTEXT)
        audience.upsert_at("cara@example.com", "subscribed", "Cara", datetime(2026, 11, 1, 9, 10, tzinfo=UTC))
        _, cursor = await drain(fetcher, cursor)
        await drain(fetcher, cursor)
    requests = audience.requests
    assert len(requests) == 3
    assert "sort_dir=DESC" in requests[0]
    assert "since_last_changed=2026-11-01T09%3A00%3A00%2B00%3A00" in requests[1], "lists the baseline second"
    assert "since_last_changed=2026-11-01T09%3A05%3A00%2B00%3A00" in requests[2], "rereads five minutes"
    assert all("sort_dir=ASC" in request and "count=250" in request for request in requests[1:])


def test_configuration_validation() -> None:
    invalid = [
        MemberChangedTriggerConfiguration(),
        MemberChangedTriggerConfiguration(audience_id="57afe96172/x"),
        MemberChangedTriggerConfiguration(audience_id=AUDIENCE_ID, statuses=("gone",)),
        MemberChangedTriggerConfiguration(audience_id=AUDIENCE_ID, statuses=("pending", "pending")),
    ]
    for configuration in invalid:
        with pytest.raises(ValueError):
            validate_member_changed_trigger_configuration(configuration)
    every_status = ("subscribed", "unsubscribed", "cleaned", "pending", "transactional", "archived")
    validate_member_changed_trigger_configuration(
        MemberChangedTriggerConfiguration(audience_id=AUDIENCE_ID, statuses=every_status)
    )


def test_poll_interval_has_a_five_second_floor() -> None:
    credentials = StaticCredentialProvider({REFERENCE: Credentials(SecretString(API_KEY))})
    assert Client(Config(), credentials).poll_interval == timedelta(seconds=30)
    assert Client(Config(poll_interval_seconds=5), credentials).poll_interval == timedelta(seconds=5)
    for seconds in (4.5, -1):
        with pytest.raises(ValueError, match="at least 5s"):
            Client(Config(poll_interval_seconds=seconds), credentials)
