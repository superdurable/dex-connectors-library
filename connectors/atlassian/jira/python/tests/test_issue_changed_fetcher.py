# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Port of the Go issueChanged fetcher tests, with cursor tokens checked against Go recordings."""

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import ChangePage, Cursor, RateLimitedError, is_cursor_invalid

from dex_connectors.atlassian_jira import (
    ISSUE_CHANGE_CREATED,
    ISSUE_CHANGE_UPDATED,
    Client,
    Config,
    Credentials,
    IssueChangedEvent,
    IssueChangedTriggerConfiguration,
)
from dex_connectors.atlassian_jira.issue_change_cursor import (
    decode_issue_change_cursor,
    encode_issue_change_cursor,
)
from dex_connectors.atlassian_jira.issue_change_fetcher import IssueChangeFetcher

from .jira_search_fake import CLOUD_ID, HUMAN_RESOURCES, OPERATIONS, JiraSearchSite

CONNECTION = ConnectionRef("atlassian", "jira-trigger-test")
GO_CASES = json.loads((Path(__file__).parent / "fixtures/go-issue-change-cases.json").read_text())
Page = ChangePage[TriggerEvent[IssueChangedEvent]]


class TestClock:
    """A settable clock shared by the client and the fake site."""

    __test__ = False

    def __init__(self) -> None:
        self.now = datetime(2026, 11, 2, 9, 0, 0, tzinfo=UTC)

    def read(self) -> datetime:
        return self.now

    def advance(self, duration: timedelta) -> None:
        self.now += duration


class Environment:
    """One fake site, its client, and a recorder of the tokens and changes the Go fixtures hold."""

    def __init__(self, local_clock: Callable[[TestClock], Callable[[], datetime]] | None = None) -> None:
        self.clock = TestClock()
        self.site = JiraSearchSite(self.clock.read)
        credentials = Credentials(
            "client-id",
            SecretString("client-secret"),
            SecretString("jira-trigger-token"),
            SecretString("refresh"),
        )
        self.client = Client(
            Config(cloud_id=CLOUD_ID, endpoint="http://127.0.0.1:12345", poll_interval_seconds=5),
            StaticCredentialProvider({CONNECTION: credentials}),
            httpx.AsyncClient(transport=self.site.transport()),
            clock=local_clock(self.clock) if local_clock else self.clock.read,
        )
        self.context = TriggerContext()
        self.recorded: dict[str, list[Any]] = {
            "tokens": [],
            "eventIds": [],
            "kinds": [],
            "keys": [],
            "hasMore": [],
        }

    def fetcher(self, configuration: IssueChangedTriggerConfiguration | None = None) -> IssueChangeFetcher:
        return IssueChangeFetcher(
            self.client,
            CONNECTION,
            configuration or IssueChangedTriggerConfiguration(project_id=OPERATIONS.id),
        )

    async def seed(self, fetcher: IssueChangeFetcher) -> Cursor:
        cursor = await fetcher.seed_cursor(self.context)
        self.recorded["tokens"].append(cursor.token)
        return cursor

    async def fetch(self, fetcher: IssueChangeFetcher, cursor: Cursor) -> Page:
        page = await fetcher.fetch_changes(self.context, cursor)
        self.recorded["tokens"].append(page.next_cursor.token)
        self.recorded["hasMore"].append(page.has_more_pages)
        for change in page.changes:
            self.recorded["eventIds"].append(change.id)
            self.recorded["kinds"].append(change.payload.change_kind)
            self.recorded["keys"].append(change.payload.issue.key)
        return page

    def assert_matches_go(self, scenario: str) -> None:
        expected = GO_CASES["scenarios"][scenario]
        for name, values in self.recorded.items():
            assert values == (expected[name] or []), f"{scenario} {name} differs from the Go connector"
        requests = [
            {"jql": request.jql, "nextPageToken": request.next_page_token, "status": request.status}
            for request in self.site.requests()
        ]
        assert requests == expected["requests"]


def changed_keys(page: Page) -> list[str]:
    return [change.payload.issue.key for change in page.changes]


async def test_seed_marks_recent_issues_seen_and_delivers_no_baseline() -> None:
    environment = Environment()
    environment.site.create(OPERATIONS, "Old issue")
    environment.clock.advance(timedelta(hours=1))
    recent = environment.site.create(OPERATIONS, "Recent issue")
    fetcher = environment.fetcher()

    cursor = await fetcher.seed_cursor(environment.context)

    position = decode_issue_change_cursor(cursor)
    now_millis = int(environment.clock.read().timestamp() * 1000)
    assert (position.cloud_id, position.project, position.updated_through) == (
        CLOUD_ID,
        "id:10000",
        now_millis,
    )
    assert position.seen_versions == [f"{environment.site.issue_id(recent)}@{now_millis}"]
    assert environment.site.requests()[0].jql == "project = 10000 AND updated >= -4m ORDER BY updated DESC"
    environment.clock.advance(timedelta(minutes=1))
    page = await fetcher.fetch_changes(environment.context, cursor)
    assert not page.changes, "neither the old nor the recent baseline issue is delivered"
    assert page.next_cursor.token == cursor.token, "an empty poll leaves the cursor unchanged"


async def test_fetch_delivers_created_and_updated_issues_once_with_go_identical_cursors() -> None:
    environment = Environment()
    existing = environment.site.create(OPERATIONS, "Existing")
    environment.clock.advance(timedelta(minutes=10))
    fetcher = environment.fetcher()
    cursor = await environment.seed(fetcher)
    environment.clock.advance(timedelta(seconds=1))
    created = environment.site.create(OPERATIONS, "Loading dock light out")
    environment.site.create(HUMAN_RESOURCES, "Other project")
    environment.clock.advance(timedelta(seconds=1))
    environment.site.update(existing, "Existing, edited")
    environment.clock.advance(timedelta(seconds=1))

    page = await environment.fetch(fetcher, cursor)
    assert changed_keys(page) == [created, existing], "ascending updated order, other projects excluded"
    assert [change.payload.change_kind for change in page.changes] == [
        ISSUE_CHANGE_CREATED,
        ISSUE_CHANGE_UPDATED,
    ]
    assert page.changes[1].payload.issue.summary == "Existing, edited"
    assert page.changes[0].payload.cloud_id == CLOUD_ID
    assert environment.site.requests()[-1].max_results == 100
    refetched = await fetcher.fetch_changes(environment.context, cursor)
    assert [change.id for change in refetched.changes] == [change.id for change in page.changes]

    environment.clock.advance(timedelta(seconds=30))
    after = await environment.fetch(fetcher, page.next_cursor)
    assert not after.changes, "re-read issues within the lookback are not delivered again"
    environment.site.update(created, "Loading dock light replaced")
    again = await environment.fetch(fetcher, after.next_cursor)
    assert changed_keys(again) == [created]
    assert again.changes[0].id != page.changes[0].id, "a new version has a new event ID"
    expected = GO_CASES["scenarios"]["seedAndFetch"]
    assert environment.recorded["tokens"] == expected["tokens"]
    assert environment.recorded["eventIds"] == expected["eventIds"]
    assert environment.recorded["kinds"] == expected["kinds"]


async def test_fetch_delivers_an_update_indexed_late_within_the_lookback() -> None:
    environment = Environment()
    fetcher = environment.fetcher()
    cursor = await fetcher.seed_cursor(environment.context)
    environment.clock.advance(timedelta(seconds=1))
    lagging = environment.site.create(OPERATIONS, "Indexed late")
    environment.site.set_indexed(lagging, False)
    environment.clock.advance(timedelta(seconds=1))
    prompt = environment.site.create(OPERATIONS, "Indexed at once")
    first = await fetcher.fetch_changes(environment.context, cursor)
    assert changed_keys(first) == [prompt]
    environment.site.set_indexed(lagging, True)
    environment.clock.advance(timedelta(seconds=30))
    second = await fetcher.fetch_changes(environment.context, first.next_cursor)
    assert changed_keys(second) == [lagging], "an update older than the newest one read is still delivered"


async def test_fetch_pages_and_restarts_after_a_rejected_page_token() -> None:
    environment = Environment()
    fetcher = environment.fetcher()
    cursor = await environment.seed(fetcher)
    for index in range(150):
        environment.clock.advance(timedelta(milliseconds=1))
        environment.site.create(OPERATIONS, f"Bulk {index}")
    first = await environment.fetch(fetcher, cursor)
    assert len(first.changes) == 100 and first.has_more_pages
    assert decode_issue_change_cursor(first.next_cursor).page_token
    environment.site.reject_page_tokens(True)
    second = await environment.fetch(fetcher, first.next_cursor)
    assert not second.changes and second.has_more_pages
    requests = environment.site.requests()
    assert requests[-2].next_page_token and requests[-2].status == 400 and not requests[-1].next_page_token
    environment.site.reject_page_tokens(False)
    third = await environment.fetch(fetcher, second.next_cursor)
    assert len(third.changes) == 50 and not third.has_more_pages, "nothing is lost across the restart"
    environment.assert_matches_go("rejectedPageToken")


async def test_fetch_restarts_a_page_sequence_older_than_the_lookback() -> None:
    environment = Environment()
    fetcher = environment.fetcher()
    cursor = await environment.seed(fetcher)
    for index in range(120):
        environment.clock.advance(timedelta(milliseconds=1))
        environment.site.create(OPERATIONS, f"Bulk {index}")
    first = await environment.fetch(fetcher, cursor)
    assert first.has_more_pages
    environment.clock.advance(timedelta(minutes=2, seconds=1))
    second = await environment.fetch(fetcher, first.next_cursor)
    last = environment.site.requests()[-1]
    assert not last.next_page_token, "a stale page token is not reused"
    assert last.jql == "project = 10000 AND updated >= -6m ORDER BY updated ASC"
    assert not second.changes
    third = await environment.fetch(fetcher, second.next_cursor)
    assert len(third.changes) == 20
    environment.assert_matches_go("staleSequence")


async def test_fetch_loses_no_issue_when_an_update_shifts_a_paged_backlog() -> None:
    environment = Environment()
    fetcher = environment.fetcher()
    cursor = await environment.seed(fetcher)
    keys = []
    for index in range(150):
        environment.clock.advance(timedelta(seconds=12))
        keys.append(environment.site.create(OPERATIONS, f"Backlog {index}"))
    first = await environment.fetch(fetcher, cursor)
    assert len(first.changes) == 100 and first.has_more_pages
    environment.site.update(keys[10], "Backlog 10, edited")
    delivered = set(changed_keys(first))
    is_edit_delivered = False
    page = first
    while page.has_more_pages:
        page = await environment.fetch(fetcher, page.next_cursor)
        delivered.update(changed_keys(page))
        is_edit_delivered |= any(
            change.payload.issue.summary == "Backlog 10, edited" for change in page.changes
        )
    assert set(keys) <= delivered and is_edit_delivered
    assert not decode_issue_change_cursor(first.next_cursor).page_token, (
        "a page that moves the floor restarts"
    )
    environment.assert_matches_go("backlogShift")


async def test_fetch_uses_jira_clock_when_the_local_clock_runs_ahead() -> None:
    environment = Environment(lambda clock: lambda: clock.read() + timedelta(minutes=10, milliseconds=250))
    fetcher = environment.fetcher(IssueChangedTriggerConfiguration())
    cursor = await environment.seed(fetcher)
    environment.clock.advance(timedelta(seconds=1))
    created = environment.site.create(OPERATIONS, "Created right after the baseline")
    environment.site.create(HUMAN_RESOURCES, "Other project")
    page = await environment.fetch(fetcher, cursor)
    assert changed_keys(page) == [created, "HR-2"], "a skewed worker clock does not hide a new issue"
    await environment.seed(environment.fetcher(IssueChangedTriggerConfiguration(project_key="HR")))
    environment.assert_matches_go("skewedClock")


@pytest.mark.parametrize(("schedule", "retry_after"), [("rate_limit", 7), ("overload", 11)])
async def test_fetch_pauses_the_connection_on_rate_limit(schedule: str, retry_after: int) -> None:
    environment = Environment()
    fetcher = environment.fetcher()
    cursor = await fetcher.seed_cursor(environment.context)
    if schedule == "rate_limit":
        environment.site.rate_limit_next_requests(1, str(retry_after))
    else:
        environment.site.overload_next_requests(1, str(retry_after))
    with pytest.raises(RateLimitedError) as raised:
        await fetcher.fetch_changes(environment.context, cursor)
    assert raised.value.retry_after == timedelta(seconds=retry_after)
    assert not is_cursor_invalid(raised.value)


async def test_fetch_marks_foreign_or_undecodable_cursors_invalid() -> None:
    environment = Environment()
    cursor = await environment.fetcher().seed_cursor(environment.context)
    hr_fetcher = environment.fetcher(IssueChangedTriggerConfiguration(project_key="HR"))
    with pytest.raises(Exception) as raised:
        await hr_fetcher.fetch_changes(environment.context, cursor)
    assert is_cursor_invalid(raised.value), "a changed project records a new baseline"
    position = decode_issue_change_cursor(cursor)
    position.cloud_id = "99999999-0000-4000-8000-000000000000"
    with pytest.raises(Exception) as raised:
        await environment.fetcher().fetch_changes(environment.context, encode_issue_change_cursor(position))
    assert is_cursor_invalid(raised.value), "a changed site records a new baseline"
    with pytest.raises(Exception) as raised:
        await environment.fetcher().fetch_changes(environment.context, Cursor("not json"))
    assert is_cursor_invalid(raised.value)


async def test_fetch_reports_rejections_and_unavailable_credentials() -> None:
    environment = Environment()
    cursor = await environment.fetcher().seed_cursor(environment.context)
    rejecting = Client(
        Config(cloud_id=CLOUD_ID, endpoint="http://127.0.0.1:12345", poll_interval_seconds=5),
        environment.client.credentials,
        httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(403, json={}))),
    )
    with pytest.raises(RuntimeError, match="Jira rejected the issue search with HTTP 403"):
        await IssueChangeFetcher(rejecting, CONNECTION, IssueChangedTriggerConfiguration()).fetch_changes(
            environment.context, Cursor(cursor.token.replace(',"project":"id:10000"', ""))
        )
    missing = Client(
        Config(cloud_id=CLOUD_ID, endpoint="http://127.0.0.1:12345"),
        StaticCredentialProvider({}),
        httpx.AsyncClient(transport=environment.site.transport()),
    )
    with pytest.raises(RuntimeError, match="credentials are unavailable"):
        await IssueChangeFetcher(missing, CONNECTION, IssueChangedTriggerConfiguration()).seed_cursor(
            environment.context
        )


def test_client_rejects_a_poll_interval_under_five_seconds() -> None:
    with pytest.raises(ValueError, match="pollInterval must be at least 5s"):
        Client(Config(cloud_id=CLOUD_ID, poll_interval_seconds=4), StaticCredentialProvider({}))


def test_client_keeps_the_default_poll_interval_for_zero() -> None:
    client = Client(Config(cloud_id=CLOUD_ID, poll_interval_seconds=0), StaticCredentialProvider({}))
    assert client.config.poll_interval_seconds == 30
