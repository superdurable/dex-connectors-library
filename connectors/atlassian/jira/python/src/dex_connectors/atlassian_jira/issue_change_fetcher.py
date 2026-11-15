# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll Jira's enhanced JQL search on the updated time for the issueChanged Trigger."""

import email.utils
import hashlib
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import ConnectionRef, FailureKind, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)

from ._generated.definitions import ISSUE_CHANGED_TRIGGER_DEFINITION
from .client import Client, SessionError, rejected_fields, rejection_failure
from .issue_change_cursor import (
    ISSUE_CHANGE_LOOKBACK_MILLISECONDS,
    IssueChangeCursor,
    decode_issue_change_cursor,
    encode_issue_change_cursor,
    issue_version_key,
    recent_seen_versions,
)
from .models import Issue, IssueChangedEvent, IssueChangedTriggerConfiguration
from .operations import (
    _FIELDS,
    _NUMERIC,
    _array,
    _field_string,
    _issue_json,
    _issue_resource,
    _object,
    _timestamp,
)

ISSUE_CHANGE_CREATED: Final = "created"
"""A new issue: Jira's updated time is within one second of its created time."""
ISSUE_CHANGE_UPDATED: Final = "updated"
"""Any other change that moved the issue's updated time, such as an edit, transition, or comment."""

_OPERATION: Final = "issueChanged"
_SEARCH_PATH: Final = "/search/jql"
_SEARCH_SUBJECT: Final = "issue search"
# Jira caps enhanced search pages with standard fields at 100.
_PAGE_SIZE: Final = 100
_MAXIMUM_SEED_PAGES: Final = 10
# Pages follow one token only this far; Jira does not promise tokens survive reordering.
_PAGE_CHAIN_PROGRESS_MILLISECONDS: Final = 60_000
_MAXIMUM_PAGE_TOKEN_BYTES: Final = 4096
_CREATED_UPDATED_TOLERANCE: Final = timedelta(seconds=1)
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_GO_ZERO_TIME: Final = datetime.min.replace(tzinfo=UTC)


@dataclass(frozen=True, slots=True)
class _SearchPage:
    issues: list[Any]
    next_page_token: str = ""
    is_page_token_rejected: bool = False


@dataclass(frozen=True, slots=True)
class _PollSession:
    context: TriggerContext
    credentials: Any
    cloud_id: str


def validate_issue_changed_trigger_configuration(configuration: IssueChangedTriggerConfiguration) -> None:
    """Reject a project ID that is not numeric and a project key that is not an uppercase Jira key.

    Blank values are valid; blank project_id and project_key watch every project the account can browse.
    Raises ValueError.
    """
    if configuration.project_id and not _NUMERIC.fullmatch(configuration.project_id):
        raise ValueError("issueChanged projectId must be a numeric Jira project ID such as 10000")
    if configuration.project_key and not _is_project_key(configuration.project_key):
        raise ValueError("issueChanged projectKey must be an uppercase Jira project key such as OPS")


def _is_project_key(value: str) -> bool:
    return (
        0 < len(value) <= 255
        and value.isascii()
        and "A" <= value[0] <= "Z"
        and all(character.isdigit() or "A" <= character <= "Z" or character == "_" for character in value)
    )


def _watched_project(configuration: IssueChangedTriggerConfiguration) -> tuple[str, str]:
    """Return the JQL clause value and the cursor identity; both are empty for every browsable project."""
    if configuration.project_id:
        return configuration.project_id, "id:" + configuration.project_id
    if configuration.project_key:
        return '"' + configuration.project_key + '"', "key:" + configuration.project_key
    return "", ""


def issue_change_event_id(cloud_id: str, issue: Issue) -> str:
    """Hash the site, the issue ID, and the updated instant, which identify one issue version.

    A refetch after a crash yields the same ID, so the Dex Flow target starts one Flow per version.
    """
    version = "\x00".join((cloud_id, issue.id, str(_unix_milliseconds(issue.updated_at))))
    return "jira-" + hashlib.sha256(version.encode()).digest()[:16].hex()


def _issue_change_kind(issue: Issue) -> str:
    if (
        issue.created_at != _GO_ZERO_TIME
        and abs(issue.updated_at - issue.created_at) <= _CREATED_UPDATED_TOLERANCE
    ):
        return ISSUE_CHANGE_CREATED
    return ISSUE_CHANGE_UPDATED


def _new_issue_changed_event(cloud_id: str, issue: Issue) -> TriggerEvent[IssueChangedEvent]:
    return TriggerEvent(
        issue_change_event_id(cloud_id, issue),
        issue.updated_at.astimezone(UTC),
        IssueChangedEvent(cloud_id, change_kind=_issue_change_kind(issue), issue=issue),
    )


def _unix_milliseconds(moment: datetime) -> int:
    return (moment - _UNIX_EPOCH) // timedelta(milliseconds=1)


def _from_unix_milliseconds(milliseconds: int) -> datetime:
    return _UNIX_EPOCH + timedelta(milliseconds=milliseconds)


class IssueChangeFetcher:
    """Reads one binding's issue changes from Jira's enhanced search for the poll Trigger kit.

    Owned by one poller, which calls it from one task at a time. It estimates Jira's clock from each
    search response's Date header, so a skewed host clock cannot hide a change.
    """

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: IssueChangedTriggerConfiguration
    ) -> None:
        """Bind the client and the binding's watched project; validate configuration beforehand."""
        clause_value, self._project_identity = _watched_project(configuration)
        self._project_clause = f"project = {clause_value} AND " if clause_value else ""
        self._client = client
        self._call = new_trigger_credential_call(connection, ISSUE_CHANGED_TRIGGER_DEFINITION.trigger)
        self._jira_clock_offset = timedelta(0)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Record Jira's current time, or a later updated time Jira reports, as the baseline.

        Issues updated within the lookback are marked seen, so neither they nor older issues are delivered.
        """
        session = await self._start_poll_session(context)
        minutes = ISSUE_CHANGE_LOOKBACK_MILLISECONDS // 60_000 + 2
        jql = f"{self._project_clause}updated >= -{minutes}m ORDER BY updated DESC"
        versions: list[tuple[str, int]] = []
        page_token = ""
        for _ in range(_MAXIMUM_SEED_PAGES):
            listed = await self._search_page(session, jql, ["updated"], page_token)
            if listed.is_page_token_rejected:
                raise RuntimeError("Jira rejected the baseline search page token")
            versions.extend(_decode_issue_version(resource) for resource in listed.issues)
            if not listed.next_page_token or listed.next_page_token == page_token:
                break
            page_token = listed.next_page_token
        # Read after the first response, so the baseline is on Jira's clock rather than the local one.
        updated_through = max([_unix_milliseconds(self._jira_now())] + [millis for _, millis in versions])
        position = IssueChangeCursor(
            cloud_id=session.cloud_id,
            project=self._project_identity,
            updated_through=updated_through,
            seen_versions=recent_seen_versions([], versions, updated_through),
        )
        return encode_issue_change_cursor(position)

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[IssueChangedEvent]]:
        """Read one ascending page of issues updated since the cursor's lookback; return unseen versions.

        An undecodable cursor, or one for another site or project, is marked invalid, so the kit records a
        new baseline. A rejected page token restarts the search from the committed position.
        """
        try:
            position = decode_issue_change_cursor(cursor)
        except ValueError as error:
            raise mark_cursor_invalid(error) from None
        if position.project != self._project_identity:
            raise mark_cursor_invalid(ValueError("Jira issue cursor belongs to another project"))
        session = await self._start_poll_session(context)
        if position.cloud_id != session.cloud_id:
            raise mark_cursor_invalid(ValueError("Jira issue cursor belongs to another site"))
        now = self._jira_now()
        lookback = timedelta(milliseconds=ISSUE_CHANGE_LOOKBACK_MILLISECONDS)
        if position.page_token and now - _from_unix_milliseconds(position.search_started_at) > lookback:
            # A relative JQL bound moves with Jira's clock; restart before it passes the committed position.
            position.page_token = ""
        listed = await self._search_changed_issues(session, position, now)
        if listed.is_page_token_rejected:
            position.page_token = ""
            listed = await self._search_changed_issues(session, position, now)
            if listed.is_page_token_rejected:
                raise RuntimeError("Jira rejected the issue search without a page token")
        if len(listed.issues) > MAXIMUM_CHANGES_PER_PAGE:
            count, maximum = len(listed.issues), MAXIMUM_CHANGES_PER_PAGE
            raise RuntimeError(f"Jira issue search page has {count} issues, more than {maximum}")
        seen = set(position.seen_versions)
        threshold = position.updated_through - ISSUE_CHANGE_LOOKBACK_MILLISECONDS
        delivered: list[tuple[str, int]] = []
        changes: list[TriggerEvent[IssueChangedEvent]] = []
        updated_through = position.updated_through
        for resource in listed.issues:
            try:
                issue = _issue_resource(resource, (), False)
            except (ValueError, TypeError, KeyError) as error:
                raise RuntimeError(f"Jira issue search page has an invalid issue: {error}") from None
            updated_millis = _unix_milliseconds(issue.updated_at)
            key = issue_version_key(issue.id, updated_millis)
            if updated_millis < threshold or key in seen:
                continue
            seen.add(key)
            delivered.append((issue.id, updated_millis))
            updated_through = max(updated_through, updated_millis)
            changes.append(_new_issue_changed_event(session.cloud_id, issue))
        next_position = IssueChangeCursor(
            cloud_id=position.cloud_id,
            project=position.project,
            updated_through=updated_through,
            seen_versions=recent_seen_versions(position.seen_versions, delivered, updated_through),
        )
        has_more_pages = False
        if listed.next_page_token and listed.next_page_token != position.page_token:
            has_more_pages = True
            # A fresh search from the new floor re-reads any issue an update moved across the page boundary.
            if (
                updated_through - ISSUE_CHANGE_LOOKBACK_MILLISECONDS
                < position.search_floor + _PAGE_CHAIN_PROGRESS_MILLISECONDS
            ):
                next_position = replace(
                    next_position,
                    page_token=listed.next_page_token,
                    search_minutes=position.search_minutes,
                    search_started_at=position.search_started_at,
                    search_floor=position.search_floor,
                )
        return ChangePage(changes, encode_issue_change_cursor(next_position), has_more_pages)

    async def _search_changed_issues(
        self, session: _PollSession, position: IssueChangeCursor, now: datetime
    ) -> _SearchPage:
        """Read the cursor's page, or start a search covering the lookback plus one minute of JQL precision.

        A relative bound keeps the query independent of the account time zone Jira applies to absolute dates.
        """
        if not position.page_token:
            since = _from_unix_milliseconds(position.updated_through - ISSUE_CHANGE_LOOKBACK_MILLISECONDS)
            position.search_minutes = max(now - since, timedelta(0)) // timedelta(minutes=1) + 2
            position.search_started_at = _unix_milliseconds(now)
            position.search_floor = _unix_milliseconds(since)
        jql = f"{self._project_clause}updated >= -{position.search_minutes}m ORDER BY updated ASC"
        return await self._search_page(session, jql, list(_FIELDS), position.page_token)

    async def _start_poll_session(self, context: TriggerContext) -> _PollSession:
        await wait_for_poll_request(context)
        try:
            credentials = await self._client.resolve_credentials(self._call, _OPERATION)
        except SessionError:
            raise RuntimeError(
                "Jira Trigger credentials are unavailable; reconnect the connection if this persists"
            ) from None
        try:
            cloud_id = await self._client.resolve_site_cloud_id(self._call, credentials, _OPERATION)
        except SessionError as error:
            site_error = RuntimeError(error.failure.message)
            if error.failure.kind == FailureKind.RATE_LIMIT:
                raise mark_rate_limited(site_error, timedelta(seconds=error.retry_after)) from None
            raise site_error from None
        return _PollSession(context, credentials, cloud_id)

    async def _search_page(
        self, session: _PollSession, jql: str, fields: list[str], page_token: str
    ) -> _SearchPage:
        """Send one search request; a 429 pauses the connection, a 400 for a page token rejects the token."""
        await wait_for_poll_request(session.context)
        body: dict[str, Any] = {"jql": jql, "fields": fields, "maxResults": _PAGE_SIZE}
        if page_token:
            body["nextPageToken"] = page_token
        response = await self._client.request(
            self._call, session.credentials, "POST", _SEARCH_PATH, cloud_id=session.cloud_id, payload=body
        )
        if response.request_defect or response.transport_error:
            raise RuntimeError("Jira issue search is temporarily unavailable")
        self._record_jira_clock(response.headers.get("Date", ""))
        status = response.status
        if status == 429:
            # The hourly points quota answers 429 with Retry-After until the window resets.
            raise mark_rate_limited(
                RuntimeError("Jira rate limited the issue search"),
                timedelta(seconds=self._client.retry_after(response)),
            )
        if status == 503 and response.headers.get("Retry-After", ""):
            # Jira documents Retry-After on some transient 503 responses; honor it like a rate limit.
            raise mark_rate_limited(
                RuntimeError("Jira asked the issue search to wait"),
                timedelta(seconds=self._client.retry_after(response)),
            )
        if status == 400 and page_token:
            return _SearchPage([], is_page_token_rejected=True)
        if not 200 <= status < 300:
            message = rejection_failure(
                _OPERATION, _SEARCH_SUBJECT, status, rejected_fields(response.body)
            ).message
            raise RuntimeError(message)
        if response.body_failed:
            raise RuntimeError("Jira issue search response was interrupted")
        if response.too_large:
            raise RuntimeError("Jira issue search response exceeds maxResponseBytes")
        if response.reflects_secret:
            raise RuntimeError("Jira issue search response contains credential material")
        try:
            page = _object(_issue_json(response.body))
            raw_issues = page.get("issues")
            issues = _array(raw_issues) if raw_issues is not None else []
            if any(not isinstance(issue, dict) for issue in issues):
                raise ValueError("issue is not an object")
            next_page_token = _field_string(page, "nextPageToken")
        except (ValueError, TypeError, UnicodeDecodeError, RecursionError):
            raise RuntimeError("Jira returned an invalid issue search page") from None
        if len(next_page_token.encode()) > _MAXIMUM_PAGE_TOKEN_BYTES or not all(
            "!" <= character <= "~" for character in next_page_token
        ):
            raise RuntimeError("Jira returned an invalid issue search page")
        return _SearchPage(issues, next_page_token)

    def _record_jira_clock(self, date_header: str) -> None:
        if not date_header:
            return
        try:
            jira_date = email.utils.parsedate_to_datetime(date_header)
        except (TypeError, ValueError, IndexError):
            return
        if jira_date.tzinfo is None:
            jira_date = jira_date.replace(tzinfo=UTC)
        self._jira_clock_offset = jira_date - self._client.clock()

    def _jira_now(self) -> datetime:
        """Estimate Jira's clock, which evaluates relative JQL and stamps updated times."""
        return self._client.clock() + self._jira_clock_offset


def _decode_issue_version(resource: Any) -> tuple[str, int]:
    try:
        item = _object(resource)
        issue_id = _field_string(item, "id")
        fields = _object(item.get("fields") or {})
        updated = _timestamp(_field_string(fields, "updated"))
    except (ValueError, TypeError):
        updated, issue_id = _GO_ZERO_TIME, ""
    if updated == _GO_ZERO_TIME or not _NUMERIC.fullmatch(issue_id):
        raise RuntimeError("Jira baseline search returned an issue without a valid ID and updated time")
    return issue_id, _unix_milliseconds(updated)
