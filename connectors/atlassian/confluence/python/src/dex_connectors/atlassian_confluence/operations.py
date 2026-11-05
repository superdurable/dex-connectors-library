# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Five Confluence operations with Dex retry and branch decisions."""

from __future__ import annotations

import hashlib
import re
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime, timedelta
from typing import Any, cast
from urllib.parse import parse_qs, urlsplit

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    QueryAttempt,
    Receipt,
)

from ._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_PAGE_DEFINITION,
    GET_PAGE_DEFINITION,
    SEARCH_PAGES_DEFINITION,
    UPDATE_PAGE_DEFINITION,
)
from ._generated.timestamps import parse_go_datetime
from .client import (
    Classification,
    Client,
    Exchange,
    Session,
    SessionError,
    decode_object,
    failure,
    path_escape,
)
from .document import adf_text, comparison_text, storage_text, written_body
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreatePageInput,
    CreatePageOutput,
    GetPageInput,
    Page,
    PageSearchFilter,
    PageSummary,
    PageVersion,
    SearchPagesInput,
    SearchPagesOutput,
    UpdatePageInput,
    UpdatePageOutput,
)

_ID = re.compile(r"^[1-9][0-9]{0,18}$")
_SPACE_KEY = re.compile(r"^~?[A-Za-z0-9_-]{1,255}$")
_EMPTY_TIME = datetime.min.replace(tzinfo=UTC)


def _object(value: dict[str, Any], key: str) -> dict[str, Any]:
    field = value.get(key)
    if field is None:
        return {}
    if not isinstance(field, dict):
        raise ValueError(f"{key} has an invalid type")
    return field


def _string(value: dict[str, Any], key: str) -> str:
    field = value.get(key, "")
    if not isinstance(field, str):
        raise ValueError(f"{key} has an invalid type")
    return field


def _number(value: dict[str, Any], key: str) -> int:
    field = value.get(key, 0)
    if type(field) is not int or not -(1 << 63) <= field < (1 << 63):
        raise ValueError(f"{key} has an invalid type")
    return field


def _date(value: str) -> datetime:
    if not value:
        return _EMPTY_TIME
    return parse_go_datetime(value, "Confluence timestamp")


def _safe_date(value: str) -> datetime:
    try:
        return _date(value)
    except ValueError:
        return _EMPTY_TIME


def _id(value: str, name: str) -> str:
    value = value.strip()
    if not _ID.fullmatch(value):
        raise ValueError(f"{name} must be a numeric Confluence content ID")
    return value


def _title(value: str) -> str:
    value = value.strip()
    if not value:
        raise ValueError("title is required")
    if len(value) > 255:
        raise ValueError("title cannot exceed 255 characters")
    if any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise ValueError("title must be one line without control characters")
    return value


def _web_url(base: str, path: str) -> str:
    if not base.startswith("https://") or not re.fullmatch(r"/[\x21-\x7e]*", path):
        return ""
    parsed = urlsplit(base)
    if parsed.username or parsed.password or parsed.query or parsed.fragment:
        return ""
    result = base.rstrip("/") + path
    return result if len(result.encode()) <= 2048 else ""


def _quote_cql(value: str) -> str:
    if len(value) > 1024 or any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise ValueError("CQL value cannot contain control characters")
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def _validate_additional_cql(clause: str) -> None:
    if len(clause) > 4096:
        raise ValueError("filter additionalCql cannot exceed 4096 characters")
    depth = 0
    quote = ""
    escaped = False
    unquoted = []
    for character in clause:
        if ord(character) < 32 or ord(character) == 127:
            raise ValueError("filter additionalCql cannot contain control characters")
        if escaped:
            escaped = False
        elif quote and character == "\\":
            escaped = True
        elif quote and character == quote:
            quote = ""
        elif not quote and character in ('"', "'"):
            quote = character
        elif not quote and character == "(":
            depth += 1
        elif not quote and character == ")":
            depth -= 1
            if depth < 0:
                raise ValueError("filter additionalCql closes a parenthesis it did not open")
        unquoted.append(character if not quote else " ")
    if quote:
        raise ValueError("filter additionalCql has an unterminated string")
    if depth:
        raise ValueError("filter additionalCql has an unclosed parenthesis")
    if re.search(r"\border\s+by\b", "".join(unquoted), re.I):
        raise ValueError("filter additionalCql cannot contain ORDER BY; use order instead")


def _build_cql(raw_filter: PageSearchFilter | dict[str, Any]) -> str:
    if isinstance(raw_filter, dict):
        raw_filter = PageSearchFilter(
            space_keys=tuple(raw_filter.get("spaceKeys", ())),
            title_phrase=raw_filter.get("titlePhrase", ""),
            labels=tuple(raw_filter.get("labels", ())),
            modified_since=raw_filter.get("modifiedSince"),
            additional_cql=raw_filter.get("additionalCql", ""),
            order=raw_filter.get("order"),
        )
    clauses = ["type = page"]
    for field_name, values, pattern in (
        ("space", raw_filter.space_keys, _SPACE_KEY),
        ("label", raw_filter.labels, re.compile(r"^[^ \t\r\n,]+$")),
    ):
        if len(values) > 50:
            raise ValueError(f"filter accepts at most 50 {field_name} values")
        if values:
            quoted = []
            for item in values:
                item = item.strip()
                if not pattern.fullmatch(item) or (field_name == "label" and len(item) > 255):
                    raise ValueError(f"filter {field_name} value is invalid")
                quoted.append(_quote_cql(item))
            clauses.append(f"{field_name} in ({', '.join(quoted)})")
    phrase = raw_filter.title_phrase.strip()
    if phrase:
        clauses.append("title ~ " + _quote_cql(_quote_cql(phrase)))
    if raw_filter.modified_since:
        date = raw_filter.modified_since
        if isinstance(date, str):
            date = _date(date)
        clauses.append(
            'lastmodified >= "' + (date.astimezone(UTC) - timedelta(hours=12)).date().isoformat() + '"'
        )
    additional = raw_filter.additional_cql.strip()
    if additional:
        _validate_additional_cql(additional)
        clauses.append(f"({additional})")
    order = raw_filter.order or "lastModifiedDescending"
    suffix = {
        "lastModifiedDescending": "lastmodified DESC",
        "createdDescending": "created DESC",
        "titleAscending": "title ASC",
    }.get(order)
    if not suffix:
        raise ValueError("filter order must be lastModifiedDescending, createdDescending, or titleAscending")
    return " AND ".join(clauses) + " ORDER BY " + suffix


def _page_summary(resource: object, base: str) -> PageSummary | None:
    if not isinstance(resource, dict):
        raise ValueError("page resource is invalid")
    if _string(resource, "type") != "page":
        return None
    page_id, title = _string(resource, "id"), _string(resource, "title")
    if not _ID.fullmatch(page_id) or not title:
        raise ValueError("page ID or title is missing")
    space, version, links = (
        _object(resource, "space"),
        _object(resource, "version"),
        _object(resource, "_links"),
    )
    space_id = space.get("id", "")
    if type(space_id) is int:
        space_id = str(space_id)
    if not isinstance(space_id, str):
        raise ValueError("space ID has an invalid type")
    return PageSummary(
        id=page_id,
        title=title,
        status=_string(resource, "status"),
        space_id=space_id if _ID.fullmatch(space_id) else "",
        space_key=_string(space, "key"),
        version_number=_number(version, "number"),
        last_modified_at=_safe_date(_string(version, "when")),
        web_url=_web_url(base, _string(links, "webui")),
    )


def _page(resource: dict[str, Any], representation: str, format_name: str, max_chars: int) -> Page:
    page_id, space_id = _string(resource, "id"), _string(resource, "spaceId")
    title, status = _string(resource, "title"), _string(resource, "status")
    if not _ID.fullmatch(page_id) or not _ID.fullmatch(space_id) or not title or not status:
        raise ValueError("page identity is invalid")
    version = _object(resource, "version")
    if _number(version, "number") < 1:
        raise ValueError("page version is invalid")
    raw_body = _string(_object(_object(resource, "body"), representation), "value")
    text = (
        storage_text(raw_body, format_name)
        if representation == "storage"
        else adf_text(raw_body, format_name)
    )
    if format_name == "markdown" and representation != "storage":
        text = text
    truncated = len(text) > max_chars
    text = text[:max_chars]
    links = _object(resource, "_links")
    return Page(
        id=page_id,
        title=title,
        status=status,
        space_id=space_id,
        parent_page_id=_string(resource, "parentId"),
        author_account_id=_string(resource, "authorId"),
        created_at=_date(_string(resource, "createdAt")),
        version=PageVersion(
            number=_number(version, "number"),
            created_at=_date(_string(version, "createdAt")),
            author_account_id=_string(version, "authorId"),
            is_minor_edit=version.get("minorEdit") is True,
        ),
        body=text,
        body_format=format_name,
        is_body_truncated=truncated,
        web_url=_web_url(_string(links, "base"), _string(links, "webui")),
    )


class SearchPagesOperation:
    """Search one bounded Confluence CQL result page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the stable search definition."""
        return SEARCH_PAGES_DEFINITION

    async def invoke(self, call: Call, input: SearchPagesInput) -> QueryAttempt[SearchPagesOutput]:
        """Run the search and classify one provider exchange."""
        operation = "searchPages"
        try:
            cql = _build_cql(input.filter)
            page_size = input.page_size or 25
            if not 1 <= page_size <= 100:
                raise ValueError("pageSize must be between 1 and 100")
            cursor = input.cursor.strip()
            if cursor and (len(cursor.encode()) > 4096 or not re.fullmatch(r"[\x21-\x7e]+", cursor)):
                raise ValueError("cursor must be the printable cursor from a previous search page")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                SearchPagesOutput(),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, str(error)),
            )
        output = SearchPagesOutput(cql=cql)
        try:
            session = await self.client.start_session(call, operation)
        except SessionError as error:
            return (
                QueryAttempt.retry(error.failure, error.retry_after)
                if error.retryable
                else QueryAttempt.branch_result("defect", output, Receipt(), error.failure)
            )
        query = {"cql": cql, "limit": str(page_size), "expand": "space,version"}
        if cursor:
            query["cursor"] = cursor
        response = await self.client.exchange(session, "GET", "search", "/content/search", query)
        classified = self.client.classify_read(operation, "page search", response)
        receipt = self.client.receipt(session, response)
        if classified.outcome == "retry":
            return QueryAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome != "accepted":
            branch = (
                "invalidResponse"
                if classified.outcome == "invalid"
                else "defect"
                if classified.outcome == "defect"
                else "providerRejected"
            )
            return QueryAttempt.branch_result(branch, output, receipt, classified.failure)
        try:
            decoded = decode_object(response)
            links = _object(decoded, "_links")
            next_link = _string(links, "next")
            next_cursor = parse_qs(urlsplit(next_link).query).get("cursor", [""])[0] if next_link else ""
            if next_link and (not next_cursor or not re.fullmatch(r"[\x21-\x7e]+", next_cursor)):
                raise ValueError("invalid search cursor")
            resources = decoded.get("results", [])
            if not isinstance(resources, list):
                raise ValueError("invalid search page")
            raw_modified_since = (
                input.filter.get("modifiedSince")
                if isinstance(input.filter, dict)
                else input.filter.modified_since
            )
            modified_since = (
                _date(raw_modified_since) if isinstance(raw_modified_since, str) else raw_modified_since
            )
            pages = tuple(
                summary
                for resource in resources
                if (summary := _page_summary(resource, _string(links, "base"))) is not None
                and (
                    modified_since is None
                    or summary.last_modified_at == _EMPTY_TIME
                    or summary.last_modified_at >= modified_since
                )
            )
            output = SearchPagesOutput(cql=cql, pages=pages, next_cursor=next_cursor)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                SearchPagesOutput(cql=cql),
                receipt,
                failure(operation, FailureKind.PROTOCOL, "Confluence returned an invalid search page"),
            )
        return QueryAttempt.branch_result("searched", output, receipt)


class GetPageOperation:
    """Read one Confluence page with a bounded body."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the stable read definition."""
        return GET_PAGE_DEFINITION

    async def invoke(self, call: Call, input: GetPageInput) -> QueryAttempt[Page]:
        """Read, decode, and classify a page."""
        operation = "getPage"
        empty = Page(body_format="")
        try:
            page_id = _id(input.page_id, "pageId")
            representation = input.body_representation or "storage"
            if representation not in ("storage", "atlas_doc_format"):
                raise ValueError("bodyRepresentation must be storage or atlas_doc_format")
            format_name = input.body_format or "markdown"
            if format_name not in ("markdown", "plainText"):
                raise ValueError("bodyFormat must be markdown or plainText")
            max_chars = input.max_body_characters or 32768
            if not 1 <= max_chars <= 131072:
                raise ValueError("maxBodyCharacters must be between 1 and 131072")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        try:
            session = await self.client.start_session(call, operation)
        except SessionError as error:
            return (
                QueryAttempt.retry(error.failure, error.retry_after)
                if error.retryable
                else QueryAttempt.branch_result("defect", empty, Receipt(), error.failure)
            )
        response = await self.client.exchange(
            session, "GET", "content", f"/pages/{path_escape(page_id)}", {"body-format": representation}
        )
        classified = self.client.classify_read(operation, "page read", response)
        receipt = self.client.receipt(session, response, page_id)
        if classified.outcome == "retry":
            return QueryAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome != "accepted":
            branch = {"notFound": "notFound", "invalid": "invalidResponse", "defect": "defect"}.get(
                classified.outcome, "providerRejected"
            )
            return QueryAttempt.branch_result(branch, empty, receipt, classified.failure)
        try:
            page = _page(decode_object(response), representation, format_name, max_chars)
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    operation, FailureKind.PROTOCOL, "Confluence returned an unreadable page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result("found", page, receipt)


def _valid_page(resource: dict[str, Any]) -> None:
    if not _ID.fullmatch(_string(resource, "id")) or not _ID.fullmatch(_string(resource, "spaceId")):
        raise ValueError("page ID or space ID is invalid")
    if not _string(resource, "title") or _number(_object(resource, "version"), "number") < 1:
        raise ValueError("page title or version is invalid")


def _create_output(
    requested: CreatePageOutput, resource: dict[str, Any], confirmed: bool = False, base: str = ""
) -> CreatePageOutput:
    _valid_page(resource)
    links = _object(resource, "_links")
    return replace(
        requested,
        page_id=_string(resource, "id"),
        space_id=_string(resource, "spaceId"),
        parent_page_id=_string(resource, "parentId"),
        version_number=_number(_object(resource, "version"), "number"),
        web_url=_web_url(_string(links, "base") or base, _string(links, "webui")),
        is_confirmed_by_title_lookup=confirmed,
    )


def _update_output(
    requested: UpdatePageOutput, resource: dict[str, Any], confirmed: bool = False
) -> UpdatePageOutput:
    _valid_page(resource)
    links = _object(resource, "_links")
    return replace(
        requested,
        version_number=_number(_object(resource, "version"), "number"),
        web_url=_web_url(_string(links, "base"), _string(links, "webui")),
        is_confirmed_by_read_back=confirmed,
    )


def _comment_output(
    requested: AddCommentOutput, resource: dict[str, Any], confirmed: bool = False
) -> AddCommentOutput:
    return replace(
        requested,
        comment_id=_string(resource, "id"),
        created_at=_safe_date(_string(_object(resource, "version"), "createdAt")),
        is_confirmed_by_read_back=confirmed,
    )


@dataclass(frozen=True, slots=True)
class DispatchCheckpoint:
    dispatched_at_unix_milli: int = field(metadata={"dex_json_name": "dispatchedAtUnixMilli"})


def _first_send_at(call: Call, now: datetime) -> datetime:
    first_attempt = getattr(call.context, "first_attempt_at", None)
    if isinstance(first_attempt, datetime) and first_attempt.year > 1:
        return min(first_attempt, now)
    return now


def _checkpoint_time(call: Call, fallback: datetime) -> datetime | None:
    if call.context is None or not call.context.has_last_heartbeat_value():
        return None
    try:
        checkpoint = call.context.get_last_heartbeat_value(DispatchCheckpoint)
        if isinstance(checkpoint, DispatchCheckpoint) and checkpoint.dispatched_at_unix_milli > 0:
            return min(datetime.fromtimestamp(checkpoint.dispatched_at_unix_milli / 1000, UTC), fallback)
    except (ValueError, TypeError, OverflowError):
        pass  # An unreadable checkpoint still proves an earlier dispatch may have happened.
    return fallback


async def _heartbeat(call: Call, when: datetime) -> bool:
    if call.context is None:
        return False
    try:
        await call.context.heartbeat(DispatchCheckpoint(int(when.timestamp() * 1000)))
        return True
    except Exception:
        return False


async def _clear_heartbeat(call: Call) -> None:
    if call.context is None:
        return
    try:
        await call.context.heartbeat()
    except Exception:
        pass  # A missed clear leads to read-back on the next attempt, not a repeated write.


async def _find_page(
    client: Client, session: Session, operation: str, space_id: str, title: str
) -> tuple[Classification, Exchange, dict[str, Any] | None, str]:
    response = await client.exchange(
        session,
        "GET",
        "content",
        "/pages",
        {"space-id": space_id, "title": title, "body-format": "storage", "limit": "5"},
    )
    classified = client.classify_read(operation, "page lookup", response)
    if classified.outcome != "accepted":
        return classified, response, None, ""
    try:
        listing = decode_object(response)
        resources = listing.get("results")
        base = _string(_object(listing, "_links"), "base")
        if not isinstance(resources, list) or len(resources) > 5:
            raise ValueError("invalid page list")
        for resource in resources:
            if not isinstance(resource, dict):
                raise ValueError("invalid page")
            _valid_page(resource)
            if _string(resource, "title") == title and _string(resource, "spaceId") == space_id:
                return classified, response, resource, base
    except (ValueError, TypeError):
        classified = Classification(
            "invalid", failure(operation, FailureKind.PROTOCOL, "Confluence returned an invalid page list")
        )
    return classified, response, None, ""


def _reconcile_creation(
    client: Client,
    session: Session,
    requested: CreatePageOutput,
    comparison: str,
    first_send_at: datetime,
    lookup: tuple[Classification, Exchange, dict[str, Any] | None, str],
) -> MutationAttempt[CreatePageOutput] | None:
    classified, response, page, base = lookup
    receipt = client.receipt(session, response)
    if classified.outcome == "notFound":
        return MutationAttempt.branch_result("notFound", requested, receipt, classified.failure)
    if classified.outcome == "rejected":
        return None
    if classified.outcome != "accepted":
        return MutationAttempt.retry(
            failure(
                "createPage",
                cast(Failure, classified.failure).kind,
                "an earlier create may have reached Confluence and its page could not be looked up: "
                + cast(Failure, classified.failure).message,
            ),
            max(classified.retry_after, 5),
        )
    if page is None:
        return None
    output = _create_output(requested, page, base=base)
    receipt = client.receipt(session, response, output.page_id)
    try:
        created_at = _date(_string(page, "createdAt"))
        storage = _string(_object(_object(page, "body"), "storage"), "value")
        is_ours = (
            created_at >= first_send_at - timedelta(minutes=1)
            and (not requested.parent_page_id or _string(page, "parentId") == requested.parent_page_id)
            and comparison_text(storage) == comparison
        )
    except (ValueError, TypeError):
        is_ours = False
    if is_ours:
        return MutationAttempt.branch_result(
            "created", replace(output, is_confirmed_by_title_lookup=True), receipt
        )
    return MutationAttempt.branch_result(
        "titleConflict",
        output,
        receipt,
        failure(
            "createPage",
            FailureKind.CONFLICT,
            "another page in the space already has this title, so nothing was created",
        ),
    )


class CreatePageOperation:
    """Create a page with title-based duplicate reconciliation."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the stable page create definition."""
        return CREATE_PAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreatePageInput) -> IdempotencyKey:
        """Use the stable Call ID for Dex's mutation identity."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreatePageInput) -> MutationAttempt[CreatePageOutput]:
        """Create a page once and classify its response or title lookup."""
        operation = "createPage"
        space_id, space_key = input.space_id.strip(), input.space_key.strip()
        requested = CreatePageOutput(
            title=input.title.strip(),
            space_id=space_id,
            space_key=space_key,
            parent_page_id=input.parent_page_id.strip(),
        )
        try:
            if bool(space_id) == bool(space_key):
                raise ValueError("set exactly one of spaceId and spaceKey")
            if space_id and not _ID.fullmatch(space_id):
                raise ValueError("spaceId must be a numeric Confluence space ID")
            if space_key and not _SPACE_KEY.fullmatch(space_key):
                raise ValueError("spaceKey must be a space key such as OPS")
            if requested.parent_page_id:
                _id(requested.parent_page_id, "parentPageId")
            requested = replace(requested, title=_title(input.title))
            storage, comparison = written_body(input.body, input.body_format)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        try:
            session = await self.client.start_session(call, operation)
        except SessionError as error:
            return (
                MutationAttempt.retry(error.failure, error.retry_after)
                if error.retryable
                else MutationAttempt.branch_result("defect", requested, Receipt(), error.failure)
            )
        if space_key:
            response = await self.client.exchange(
                session, "GET", "content", "/spaces", {"keys": space_key, "limit": "1"}
            )
            classified = self.client.classify_read(operation, "space", response)
            if classified.outcome == "retry":
                return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
            if classified.outcome != "accepted":
                branch = (
                    "notFound"
                    if classified.outcome == "notFound"
                    else "defect"
                    if classified.outcome == "defect"
                    else "providerRejected"
                )
                return MutationAttempt.branch_result(
                    branch, requested, self.client.receipt(session, response), classified.failure
                )
            try:
                resources = decode_object(response).get("results")
                if not isinstance(resources, list):
                    raise ValueError("invalid space list")
                matched = next(
                    (
                        item
                        for item in resources
                        if isinstance(item, dict)
                        and _string(item, "key").lower() == space_key.lower()
                        and _ID.fullmatch(_string(item, "id"))
                    ),
                    None,
                )
                if matched is None:
                    return MutationAttempt.branch_result(
                        "notFound",
                        requested,
                        self.client.receipt(session, response),
                        failure(
                            operation,
                            FailureKind.NOT_FOUND,
                            f"Confluence space {space_key} was not found or is not visible to the connection",
                        ),
                    )
                space_id = _string(matched, "id")
                requested = replace(requested, space_id=space_id)
            except (ValueError, TypeError):
                return MutationAttempt.branch_result(
                    "providerRejected",
                    requested,
                    self.client.receipt(session, response),
                    failure(operation, FailureKind.PROTOCOL, "Confluence returned an invalid space list"),
                )
        body = {
            "spaceId": space_id,
            "status": "current",
            "title": requested.title,
            **({"parentId": requested.parent_page_id} if requested.parent_page_id else {}),
            "body": {"representation": "storage", "value": storage},
        }
        first_send_at = _first_send_at(call, self.client.now())
        checkpoint_at = _checkpoint_time(call, first_send_at)
        if checkpoint_at is not None:
            first_send_at = min(first_send_at, checkpoint_at)
            previous_lookup = await _find_page(self.client, session, operation, space_id, requested.title)
            previous_decision = _reconcile_creation(
                self.client, session, requested, comparison, first_send_at, previous_lookup
            )
            if previous_decision is not None:
                return previous_decision
        if not await _heartbeat(call, first_send_at):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the create checkpoint could not be recorded, so nothing was sent to Confluence",
                )
            )
        response = await self.client.exchange(session, "POST", "content", "/pages", payload=body)
        classified = self.client.classify_write(operation, "page", response)
        receipt = self.client.receipt(session, response)
        if classified.outcome == "accepted":
            try:
                resource = decode_object(response)
                output = _create_output(requested, resource)
                return MutationAttempt.branch_result(
                    "created", output, self.client.receipt(session, response, output.page_id)
                )
            except (ValueError, TypeError):
                classified = Classification(
                    "ambiguous",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Confluence accepted the page but returned an unusable page",
                    ),
                )
        if classified.outcome == "notApplied":
            await _clear_heartbeat(call)
            return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome in ("notFound", "defect"):
            if classified.outcome == "defect":
                await _clear_heartbeat(call)
            return MutationAttempt.branch_result(
                "notFound" if classified.outcome == "notFound" else "defect",
                requested,
                receipt,
                classified.failure,
            )
        if classified.outcome == "rejected" and response.status not in (400, 409):
            return MutationAttempt.branch_result("providerRejected", requested, receipt, classified.failure)
        lookup = await _find_page(self.client, session, operation, space_id, requested.title)
        if lookup[2] is not None or classified.outcome == "rejected":
            decided = _reconcile_creation(self.client, session, requested, comparison, first_send_at, lookup)
            if decided is not None:
                return decided
        if classified.outcome == "rejected":
            return MutationAttempt.branch_result("providerRejected", requested, receipt, classified.failure)
        return MutationAttempt.retry(cast(Failure, classified.failure), max(classified.retry_after, 5))


class UpdatePageOperation:
    """Replace a page at a named version with a durable write marker."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the stable update definition."""
        return UPDATE_PAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdatePageInput) -> IdempotencyKey:
        """Use the stable Call ID for Dex's mutation identity."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdatePageInput) -> MutationAttempt[UpdatePageOutput]:
        """Write the next page version and confirm ambiguous responses by reading it."""
        operation = "updatePage"
        requested = UpdatePageOutput(page_id=input.page_id.strip(), title=input.title.strip())
        try:
            page_id = _id(input.page_id, "pageId")
            requested = replace(requested, title=_title(input.title))
            if type(input.next_version_number) is not int or input.next_version_number < 2:
                raise ValueError(
                    "nextVersionNumber must be the page's current version number plus one, at least 2"
                )
            message = input.version_message.strip()
            if len(message) > 200:
                raise ValueError("versionMessage cannot exceed 200 characters")
            if any(ord(char) < 32 or ord(char) == 127 for char in message):
                raise ValueError("versionMessage must be one line without control characters")
            storage, _ = written_body(input.body, input.body_format)
            marker = "dex:" + hashlib.sha256(str(call.idempotency_key or call.id).encode()).hexdigest()[:16]
            body = {
                "id": page_id,
                "status": "current",
                "title": requested.title,
                "body": {"representation": "storage", "value": storage},
                "version": {
                    "number": input.next_version_number,
                    "message": (message + " " if message else "") + f"[{marker}]",
                },
            }
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        try:
            session = await self.client.start_session(call, operation)
        except SessionError as error:
            return (
                MutationAttempt.retry(error.failure, error.retry_after)
                if error.retryable
                else MutationAttempt.branch_result("defect", requested, Receipt(), error.failure)
            )
        response = await self.client.exchange(
            session, "PUT", "content", f"/pages/{path_escape(page_id)}", payload=body
        )
        classified = self.client.classify_write(operation, "page update", response)
        receipt = self.client.receipt(session, response, page_id)
        if classified.outcome == "accepted":
            try:
                resource = decode_object(response)
                output = _update_output(requested, resource)
                if output.version_number != input.next_version_number:
                    raise ValueError("wrong version")
                return MutationAttempt.branch_result("updated", output, receipt)
            except (ValueError, TypeError):
                classified = Classification(
                    "ambiguous",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Confluence accepted the update but returned an unusable page",
                    ),
                )
        if classified.outcome == "notApplied":
            return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome in ("notFound", "defect"):
            return MutationAttempt.branch_result(classified.outcome, requested, receipt, classified.failure)
        if classified.outcome == "rejected" and response.status not in (400, 409):
            return MutationAttempt.branch_result("providerRejected", requested, receipt, classified.failure)
        lookup_response = await self.client.exchange(
            session, "GET", "content", f"/pages/{path_escape(page_id)}", {"body-format": "storage"}
        )
        lookup_state = self.client.classify_read(operation, "page", lookup_response)
        lookup_receipt = self.client.receipt(session, lookup_response, page_id)
        if lookup_state.outcome == "retry":
            return MutationAttempt.retry(cast(Failure, lookup_state.failure), lookup_state.retry_after)
        if lookup_state.outcome == "notFound":
            return MutationAttempt.branch_result("notFound", requested, lookup_receipt, lookup_state.failure)
        if lookup_state.outcome != "accepted":
            return MutationAttempt.retry(
                cast(Failure, lookup_state.failure), max(lookup_state.retry_after, 5)
            )
        try:
            page = decode_object(lookup_response)
            output = _update_output(requested, page)
            current = output.version_number
            version = _object(page, "version")
            if current == input.next_version_number and marker in _string(version, "message"):
                return MutationAttempt.branch_result(
                    "updated", replace(output, is_confirmed_by_read_back=True), lookup_receipt
                )
            if current > input.next_version_number:
                old_response = await self.client.exchange(
                    session,
                    "GET",
                    "content",
                    f"/pages/{path_escape(page_id)}/versions/{input.next_version_number}",
                )
                old_state = self.client.classify_read(operation, "page version", old_response)
                if old_state.outcome == "retry":
                    return MutationAttempt.retry(cast(Failure, old_state.failure), old_state.retry_after)
                if old_state.outcome == "accepted":
                    old = decode_object(old_response)
                    if _number(old, "number") == input.next_version_number and marker in _string(
                        old, "message"
                    ):
                        return MutationAttempt.branch_result(
                            "updated",
                            replace(
                                output,
                                version_number=input.next_version_number,
                                is_confirmed_by_read_back=True,
                            ),
                            lookup_receipt,
                        )
            if current != input.next_version_number - 1:
                return MutationAttempt.branch_result(
                    "versionConflict",
                    output,
                    lookup_receipt,
                    failure(
                        operation,
                        FailureKind.CONFLICT,
                        (
                            "the page is no longer at the version before nextVersionNumber, "
                            "so nothing was changed"
                        ),
                    ),
                )
        except (ValueError, TypeError):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "the update outcome is unknown and the page could not be read back",
                ),
                5,
            )
        if classified.outcome == "rejected":
            return MutationAttempt.branch_result(
                "providerRejected", requested, lookup_receipt, classified.failure
            )
        return MutationAttempt.retry(cast(Failure, classified.failure), max(classified.retry_after, 5))


class AddCommentOperation:
    """Add one footer comment, reconciling an unconfirmed prior dispatch."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the stable comment definition."""
        return ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Use the stable Call ID for Dex's mutation identity."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Send once or read back an earlier attempt before declaring uncertainty."""
        operation = "addComment"
        requested = AddCommentOutput(page_id=input.page_id.strip())
        try:
            page_id = _id(input.page_id, "pageId")
            storage, comparison = written_body(input.body, input.body_format)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        try:
            session = await self.client.start_session(call, operation)
        except SessionError as error:
            return (
                MutationAttempt.retry(error.failure, error.retry_after)
                if error.retryable
                else MutationAttempt.branch_result("defect", requested, Receipt(), error.failure)
            )
        first_send_at = _first_send_at(call, self.client.now())
        checkpoint_at = _checkpoint_time(call, first_send_at)
        if checkpoint_at is not None:
            first_send_at = min(first_send_at, checkpoint_at)
            response = await self.client.exchange(
                session,
                "GET",
                "content",
                f"/pages/{path_escape(page_id)}/footer-comments",
                {"body-format": "storage", "sort": "-created-date", "limit": "25"},
            )
            classified = self.client.classify_read(operation, "page comments", response)
            receipt = self.client.receipt(session, response)
            if classified.outcome == "retry":
                return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
            if classified.outcome == "notFound":
                return MutationAttempt.branch_result("notFound", requested, receipt, classified.failure)
            if classified.outcome != "accepted":
                return MutationAttempt.uncertain(requested, cast(Failure, classified.failure), receipt)
            try:
                results = decode_object(response).get("results")
                if not isinstance(results, list):
                    raise ValueError("invalid comment list")
                for item in results:
                    if not isinstance(item, dict):
                        continue
                    try:
                        comment_id = _string(item, "id")
                        body = _string(_object(_object(item, "body"), "storage"), "value")
                        created_at = _date(_string(_object(item, "version"), "createdAt"))
                    except (ValueError, TypeError):
                        continue
                    if (
                        _ID.fullmatch(comment_id)
                        and created_at >= first_send_at - timedelta(minutes=1)
                        and comparison_text(body) == comparison
                    ):
                        output = _comment_output(requested, item, True)
                        return MutationAttempt.branch_result(
                            "added", output, self.client.receipt(session, response, comment_id)
                        )
            except (ValueError, TypeError):
                return MutationAttempt.uncertain(
                    requested,
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        (
                            "an earlier attempt of this Step sent the comment, and the page's "
                            "comments read back are invalid"
                        ),
                    ),
                    receipt,
                )
            return MutationAttempt.uncertain(
                requested,
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    (
                        "an earlier attempt of this Step sent the comment without a confirmed outcome, "
                        "and the page shows no matching comment, so it is not sent again"
                    ),
                ),
                receipt,
            )
        if not await _heartbeat(call, first_send_at):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the comment checkpoint could not be recorded, so nothing was sent to Confluence",
                )
            )
        response = await self.client.exchange(
            session,
            "POST",
            "content",
            "/footer-comments",
            payload={"pageId": page_id, "body": {"representation": "storage", "value": storage}},
        )
        classified = self.client.classify_write(operation, "comment", response)
        receipt = self.client.receipt(session, response)
        if classified.outcome == "accepted":
            try:
                resource = decode_object(response)
                comment_id = _string(resource, "id")
                if not _ID.fullmatch(comment_id):
                    raise ValueError("invalid comment ID")
                output = _comment_output(requested, resource)
                return MutationAttempt.branch_result(
                    "added", output, self.client.receipt(session, response, comment_id)
                )
            except (ValueError, TypeError):
                classified = Classification(
                    "ambiguous",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Confluence accepted the comment but returned an unusable comment",
                    ),
                )
        if classified.outcome == "notApplied":
            await _clear_heartbeat(call)
            return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome in ("notFound", "rejected", "defect"):
            if classified.outcome == "defect":
                await _clear_heartbeat(call)
            branch = "providerRejected" if classified.outcome == "rejected" else classified.outcome
            return MutationAttempt.branch_result(branch, requested, receipt, classified.failure)
        return MutationAttempt.retry(cast(Failure, classified.failure), max(classified.retry_after, 5))
