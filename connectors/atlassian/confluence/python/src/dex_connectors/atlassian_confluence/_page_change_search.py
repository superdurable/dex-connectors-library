# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One pageChanged CQL content search request and its decoding, at parity with the Go connector."""

import json
import re
from dataclasses import dataclass, field
from datetime import timedelta
from typing import Any, Final

from dex_connectors.sdk import FailureKind, TriggerContext
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    mark_rate_limited,
    wait_for_poll_request,
)

from ._go_time import GoInstant, parse_go_time_layout
from ._go_url import read_next_cursor
from .client import Client, Session, SessionError
from .models import PageSummary
from .operations import _web_url

PAGE_CHANGED_TRIGGER_ID: Final = "pageChanged"
PAGE_CHANGE_SEARCH_PAGE_SIZE: Final = 100
"""Confluence's largest content search page; it may return fewer."""
_CONTENT_ID: Final = re.compile(r"[1-9][0-9]{0,18}\Z", re.ASCII)
_SPACE_KEY: Final = re.compile(r"~?[A-Za-z0-9_-]{1,255}\Z", re.ASCII)
_JSON_NUMBER: Final = re.compile(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\Z", re.ASCII)


def is_space_key(value: str) -> bool:
    """Report whether value is a Confluence space key, such as OPS or ~5b10ac8d82e05b22cc7d4ef5."""
    return _SPACE_KEY.match(value) is not None


class SearchCursorRejectedError(Exception):
    """Confluence answered HTTP 400 for a stored search cursor, such as an expired one."""


class _InvalidSearchPageError(ValueError):
    pass


@dataclass(frozen=True, slots=True)
class ChangedPage:
    """One page version of a search page: its summary and its exact Go instant."""

    summary: PageSummary
    last_modified_at: GoInstant


@dataclass(slots=True)
class SearchPageResult:
    """One decoded search page, newest modification first."""

    pages: list[ChangedPage] = field(default_factory=list)
    search_cursor: str = ""


async def start_trigger_session(client: Client, context: TriggerContext, session_call: Any) -> Session:
    """Resolve the credential and site for one poll; a 429 from the site lookup pauses the connection."""
    if not (client.cloud_id or client.resolved_cloud_id):
        await wait_for_poll_request(context)
    try:
        return await client.start_session(session_call, PAGE_CHANGED_TRIGGER_ID)
    except SessionError as error:
        message = error.failure.message
        if error.failure.kind == FailureKind.RATE_LIMIT:
            raise mark_rate_limited(RuntimeError(message), timedelta(seconds=error.retry_after)) from None
        if error.failure.kind == FailureKind.AUTHENTICATION:
            message = "Confluence Trigger credentials are unavailable; reconnect the connection"
        raise RuntimeError(message) from None


async def search_page_changes(
    client: Client, context: TriggerContext, session: Session, cql: str, search_cursor: str
) -> SearchPageResult:
    """Read one CQL content search page; a 429, or a 503 with Retry-After, pauses the connection."""
    await wait_for_poll_request(context)
    query = {"cql": cql, "limit": str(PAGE_CHANGE_SEARCH_PAGE_SIZE), "expand": "space,version"}
    if search_cursor:
        query["cursor"] = search_cursor
    response = await client.exchange(session, "GET", "search", "/content/search", query)
    context.cancellation.raise_if_cancelled()
    if response.error:
        raise RuntimeError("Confluence page search is temporarily unavailable")
    retry_after = timedelta(seconds=client.retry_after(response))
    if response.status == 429:
        raise mark_rate_limited(
            RuntimeError("Confluence rate limited the page search (HTTP 429)"), retry_after
        )
    if response.status == 503 and retry_after > timedelta(0):
        raise mark_rate_limited(RuntimeError("Confluence page search is unavailable (HTTP 503)"), retry_after)
    if response.status == 400 and search_cursor:
        raise SearchCursorRejectedError("Confluence rejected the stored search cursor")
    if not 200 <= response.status < 300:
        raise RuntimeError(f"Confluence rejected the page search with HTTP {response.status}")
    if response.body_failed:
        raise RuntimeError("Confluence page search response was interrupted")
    if response.is_too_large:
        raise RuntimeError("Confluence page search response exceeds maxResponseBytes")
    if response.reflects_secret:
        raise RuntimeError("Confluence page search response contains credential material")
    try:
        return decode_page_change_search(response.body)
    except _InvalidSearchPageError as error:
        raise RuntimeError(str(error)) from None


def decode_page_change_search(body: bytes) -> SearchPageResult:
    """Keep pages only and require each page's version and time, which order the scan."""
    try:
        document = json.loads(body)
        page = _optional_object(document)
        resources = page.get("results")
        if resources is not None and not isinstance(resources, list):
            raise ValueError("results is not a list")
        links = _optional_object(page.get("_links"))
        base, next_link = _string(links.get("base")), _string(links.get("next"))
        decoded = [_decode_resource(resource) for resource in resources or []]
    except (ValueError, TypeError, RecursionError):
        raise _InvalidSearchPageError("Confluence returned an invalid search page") from None
    if len(decoded) > MAXIMUM_CHANGES_PER_PAGE:
        raise _InvalidSearchPageError(
            f"Confluence search page has {len(decoded)} results, more than {MAXIMUM_CHANGES_PER_PAGE}"
        )
    try:
        search_cursor = read_next_cursor(next_link)
    except ValueError:
        raise _InvalidSearchPageError("Confluence returned an invalid search cursor") from None
    result = SearchPageResult(search_cursor=search_cursor)
    for resource in decoded:
        if resource["type"] != "page":
            continue
        if not _CONTENT_ID.match(resource["id"]) or not resource["title"]:
            raise _InvalidSearchPageError(
                "Confluence returned an invalid page in the search results: page ID or title is missing"
            )
        try:
            changed = _changed_page(resource, base)
        except ValueError:
            raise _InvalidSearchPageError(
                "Confluence returned an invalid page in the search results"
            ) from None
        if changed.summary.version_number < 1 or changed.last_modified_at.is_zero():
            raise _InvalidSearchPageError(
                f"Confluence search result for page {changed.summary.id} has no version number or time"
            )
        result.pages.append(changed)
    return result


def _decode_resource(resource: object) -> dict[str, Any]:
    """Read the fields Go's searchContentResource holds, with its type checks."""
    value = _optional_object(resource)
    space = value.get("space")
    version = value.get("version")
    decoded: dict[str, Any] = {
        "id": _string(value.get("id")),
        "type": _string(value.get("type")),
        "status": _string(value.get("status")),
        "title": _string(value.get("title")),
        "webui": _string(_optional_object(value.get("_links")).get("webui")),
        "space": None,
        "version": None,
    }
    if space is not None:
        space_value = _optional_object(space)
        decoded["space"] = (_json_number_text(space_value.get("id")), _string(space_value.get("key")))
    if version is not None:
        version_value = _optional_object(version)
        number = version_value.get("number")
        if number is not None and (type(number) is not int or not -(2**63) <= number < 2**63):
            raise ValueError("version number is not an integer")
        decoded["version"] = (number or 0, _string(version_value.get("when")))
    return decoded


def _changed_page(resource: dict[str, Any], base: str) -> ChangedPage:
    space_id = space_key = ""
    if resource["space"] is not None:
        space_id_text, key = resource["space"]
        space_id = space_id_text if _CONTENT_ID.match(space_id_text) else ""
        space_key = key if _SPACE_KEY.match(key) else ""
    version_number, last_modified_at = 0, GoInstant()
    if resource["version"] is not None:
        version_number, when = resource["version"]
        try:
            last_modified_at = parse_go_time_layout(when)
        except ValueError:
            last_modified_at = GoInstant()  # An unparseable time is rejected by the caller.
    summary = PageSummary(
        id=resource["id"],
        title=resource["title"],
        status=resource["status"],
        space_id=space_id,
        space_key=space_key,
        version_number=version_number,
        last_modified_at=(
            PageSummary().last_modified_at if last_modified_at.is_zero() else last_modified_at.to_datetime()
        ),
        web_url=_web_url(base, resource["webui"]),
    )
    return ChangedPage(summary, last_modified_at)


def _optional_object(value: object) -> dict[str, Any]:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError("value is not an object")
    return value


def _string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("value is not a string")
    return value


def _json_number_text(value: object) -> str:
    """Return the literal text Go's json.Number keeps; a string must hold a valid number."""
    if value is None:
        return ""
    if isinstance(value, bool):
        raise ValueError("space ID is not a number")
    if isinstance(value, int | float):
        return str(value)
    if isinstance(value, str) and _JSON_NUMBER.match(value):
        return value
    raise ValueError("space ID is not a number")
