# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hacker News public API reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, Never, TypeVar
from urllib.parse import quote, quote_plus

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk import StepDefaults as StepDefaults
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import RetryPolicy as RetryPolicy

from . import _search
from ._generated import definitions as _generated_definitions
from ._generated.config import Config as _GeneratedConfig
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FOUND as FOUND
from ._generated.definitions import GET_ITEM_OPERATION_ID as GET_ITEM_OPERATION_ID
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import LIST_STORY_IDS_OPERATION_ID as LIST_STORY_IDS_OPERATION_ID
from ._generated.definitions import LISTED as LISTED
from ._generated.definitions import NOT_FOUND as NOT_FOUND
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import (
    ArticleComment,
    ArticleThread,
    GetArticleThreadInput,
    GetItemInput,
    GetUserInput,
    Item,
    ItemSearchPage,
    ListStoryIDsInput,
    SearchItemsInput,
    SearchStoriesInput,
    StoryIDs,
    StorySearchPage,
    User,
)

CONNECTOR_ID = _generated_definitions.CONNECTOR_ID
PROVIDER = _generated_definitions.PROVIDER
FAILURE_PROVIDER = PROVIDER
FEEDS = ("top", "best", "new", "ask", "show")
ITEM_TYPES = ("story", "comment", "job", "poll", "pollopt")
_DEFAULT_BASE_URL = "https://hacker-news.firebaseio.com/v0"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 1_048_576

LIST_STORY_IDS_DEFINITION: QueryDefinition = _generated_definitions.LIST_STORY_IDS_DEFINITION
GET_USER_DEFINITION: QueryDefinition = _generated_definitions.GET_USER_DEFINITION
GET_ARTICLE_THREAD_DEFINITION: QueryDefinition = _generated_definitions.GET_ARTICLE_THREAD_DEFINITION
GET_ITEM_DEFINITION: QueryDefinition = _generated_definitions.GET_ITEM_DEFINITION

SEARCH_ITEMS_DEFINITION: QueryDefinition = _generated_definitions.SEARCH_ITEMS_DEFINITION

SEARCH_STORIES_DEFINITION: QueryDefinition = _generated_definitions.SEARCH_STORIES_DEFINITION

Output = TypeVar("Output")


class Config(_GeneratedConfig):
    """Hacker News HTTP settings; zero or blank values use manifest defaults."""

    __slots__ = ()


class Client:
    """One redirect-safe HTTP client for the public, unauthenticated API."""

    def __init__(self, config: Config, http_client: httpx.AsyncClient | None = None) -> None:
        if config is None:
            raise ValueError("Hacker News configuration is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Hacker News timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.search_base_url = validate_base_url(config.search_base_url or "https://hn.algolia.com/api/v1")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_story_ids(self) -> "ListStoryIDsOperation":
        """Bind the feed snapshot Query to this client."""
        return ListStoryIDsOperation(self)

    def get_item(self) -> "GetItemOperation":
        """Bind the item Query to this client."""
        return GetItemOperation(self)

    def get_article_thread(self) -> "GetArticleThreadOperation":
        """Bind the indexed article and public comment tree Query."""
        return GetArticleThreadOperation(self)

    def search_items(self) -> "SearchItemsOperation":
        """Bind the Query that searches indexed items."""
        return SearchItemsOperation(self)

    def search_stories(self) -> "SearchStoriesOperation":
        """Bind the Query that searches indexed stories."""
        return SearchStoriesOperation(self)

    def get_user(self) -> "GetUserOperation":
        """Bind the public user profile Query to this client."""
        return GetUserOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def get_json(
        self, operation: str, path: str, empty: Output, decode: Callable[[bytes], QueryAttempt[Output]]
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go getJSON helper."""
        try:
            request = self.http_client.build_request(
                "GET",
                (
                    self.search_base_url
                    if operation in ("getArticleThread", "searchItems", "searchStories")
                    else self.base_url
                )
                + path,
                headers={"Accept": "application/json", "User-Agent": "superdurable-hacker-news-daily/0.1"},
            )
        except (httpx.HTTPError, ValueError):
            return _failed(
                operation,
                DEFECT,
                empty,
                FailureKind.VALIDATION,
                "Hacker News request configuration is invalid",
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, "Hacker News request failed", 0)
        try:
            status = response.status_code
            if status in (408, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _retry(operation, kind, "Hacker News is temporarily unavailable", retry_after)
            if status == 404 and operation in (GET_ITEM_OPERATION_ID, "getUser", "getArticleThread"):
                return _failed(
                    operation, NOT_FOUND, empty, FailureKind.NOT_FOUND, "Hacker News resource was not found"
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    "Hacker News rejected the request",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Hacker News response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "Hacker News response was interrupted", 0)
            return decode(body)
        finally:
            await response.aclose()


class ListStoryIDsOperation:
    """Read a bounded snapshot of one Hacker News feed."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Hacker News client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_STORY_IDS_DEFINITION

    async def invoke(self, call: Call, input: ListStoryIDsInput) -> QueryAttempt[StoryIDs]:
        """Validate the feed and limit, read once, then select a branch or safe retry."""
        operation = LIST_STORY_IDS_OPERATION_ID
        if (
            not isinstance(input, ListStoryIDsInput)
            or not isinstance(input.feed, str)
            or type(input.limit) is not int
        ):
            return _failed(
                operation, DEFECT, StoryIDs(), FailureKind.VALIDATION, "feed and limit are invalid"
            )
        feed = input.feed or "top"
        limit = input.limit or 50
        if feed not in FEEDS:
            return _failed(
                operation,
                DEFECT,
                StoryIDs(),
                FailureKind.VALIDATION,
                "feed must be top, best, new, ask, or show",
            )
        if not 1 <= limit <= 500:
            return _failed(
                operation, DEFECT, StoryIDs(), FailureKind.VALIDATION, "limit must be from 1 through 500"
            )
        return await self.client.get_json(
            operation, f"/{feed}stories.json", StoryIDs(), lambda body: _decode_story_ids(body, limit)
        )


class GetItemOperation:
    """Read one public Hacker News item."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Hacker News client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_ITEM_DEFINITION

    async def invoke(self, call: Call, input: GetItemInput) -> QueryAttempt[Item]:
        """Validate the item ID, read once, then select a branch or safe retry."""
        operation = GET_ITEM_OPERATION_ID
        if not isinstance(input, GetItemInput) or type(input.id) is not int or input.id <= 0:
            return _failed(operation, DEFECT, Item(), FailureKind.VALIDATION, "item ID must be positive")
        requested = input.id
        return await self.client.get_json(
            operation, f"/item/{requested}.json", Item(), lambda body: _decode_item(body, requested)
        )


class GetArticleThreadOperation:
    """Read one public Hacker News article with its indexed comments."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Hacker News client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the article Query identity, branches and retry defaults."""
        return GET_ARTICLE_THREAD_DEFINITION

    async def invoke(self, call: Call, input: GetArticleThreadInput) -> QueryAttempt[ArticleThread]:
        """Read one bounded Search API response for a positive signed 64-bit article ID."""
        if (
            not isinstance(input, GetArticleThreadInput)
            or type(input.id) is not int
            or not 0 < input.id < 2**63
        ):
            return _failed(
                "getArticleThread",
                DEFECT,
                ArticleThread(),
                FailureKind.VALIDATION,
                "article ID must be positive",
            )
        return await self.client.get_json(
            "getArticleThread",
            f"/items/{input.id}",
            ArticleThread(),
            lambda body: _decode_article_thread(body, input.id),
        )


class SearchItemsOperation:
    """Read one bounded Search API page of indexed items."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Hacker News client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the search Query identity, branches, and retry defaults."""
        return SEARCH_ITEMS_DEFINITION

    async def invoke(self, call: Call, input: SearchItemsInput) -> QueryAttempt[ItemSearchPage]:
        """Validate the filters, fetch one page and preserve provider result order."""
        try:
            request = _search.item_search_request(input)
        except (ValueError, TypeError) as error:
            return _failed("searchItems", DEFECT, ItemSearchPage(), FailureKind.VALIDATION, str(error))

        def decode(body: bytes) -> QueryAttempt[ItemSearchPage]:
            try:
                page = _search.item_search_page(body, request, input.type)
            except (ValueError, RecursionError):
                return _failed(
                    "searchItems",
                    INVALID_RESPONSE,
                    ItemSearchPage(),
                    FailureKind.PROTOCOL,
                    "Hacker News Search returned a malformed item result page",
                )
            return QueryAttempt.branch_result("searched", page, Receipt())

        return await self.client.get_json("searchItems", request.path, ItemSearchPage(), decode)


class SearchStoriesOperation:
    """Read one bounded Search API page of indexed stories."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Hacker News client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the search Query identity, branches, and retry defaults."""
        return SEARCH_STORIES_DEFINITION

    async def invoke(self, call: Call, input: SearchStoriesInput) -> QueryAttempt[StorySearchPage]:
        """Validate the filters, fetch one page and preserve provider result order."""
        try:
            request = _search.story_search_request(input)
        except (ValueError, TypeError) as error:
            return _failed("searchStories", DEFECT, StorySearchPage(), FailureKind.VALIDATION, str(error))

        def decode(body: bytes) -> QueryAttempt[StorySearchPage]:
            try:
                page = _search.story_search_page(body, request)
            except (ValueError, RecursionError):
                return _failed(
                    "searchStories",
                    INVALID_RESPONSE,
                    StorySearchPage(),
                    FailureKind.PROTOCOL,
                    "Hacker News Search returned a malformed story result page",
                )
            return QueryAttempt.branch_result("searched", page, Receipt())

        return await self.client.get_json("searchStories", request.path, StorySearchPage(), decode)


class GetUserOperation:
    """Read one public Hacker News user profile."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Hacker News client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the profile Query identity, branches and retry defaults."""
        return GET_USER_DEFINITION

    async def invoke(self, call: Call, input: GetUserInput) -> QueryAttempt[User]:
        """Read the case-sensitive username once and classify the public response."""
        if (
            not isinstance(input, GetUserInput)
            or not isinstance(input.id, str)
            or not input.id
            or len(input.id.encode("utf-8", errors="replace")) > 100
            or any(character in input.id for character in "/?#")
        ):
            return _failed("getUser", DEFECT, User(), FailureKind.VALIDATION, "username is invalid")
        try:
            segment = quote(input.id, safe="$&+:=@")
        except UnicodeEncodeError:
            return _failed("getUser", DEFECT, User(), FailureKind.VALIDATION, "username is invalid")
        return await self.client.get_json(
            "getUser", f"/user/{segment}.json", User(), lambda body: _decode_user(body, input.id)
        )


def _reject_non_json_number(value: str) -> Never:
    raise ValueError(f"invalid JSON number {value}")


def _decode_user(body: bytes, requested: str) -> QueryAttempt[User]:
    try:
        document = json.loads(body, parse_constant=_reject_non_json_number)
        if document is None:
            return _failed(
                "getUser", NOT_FOUND, User(), FailureKind.NOT_FOUND, "Hacker News user was not found"
            )
        if not isinstance(document, dict):
            raise ValueError
        fields = {name.lower(): value for name, value in document.items()}
        username = _string(fields, "id")
        about = _string(fields, "about")
        _string(fields, "profileurl")
        created = _integer(fields, "created")
        karma = _integer(fields, "karma")
        submitted = fields.get("submitted")
        if submitted is None:
            submitted = []
        if not isinstance(submitted, list):
            raise ValueError
        ids = tuple(0 if value is None else value for value in submitted)
        for number in (created, karma, *ids):
            if type(number) is not int or not -(1 << 63) <= number < 1 << 63:
                raise ValueError
    except (ValueError, UnicodeDecodeError, RecursionError):
        return _failed(
            "getUser",
            INVALID_RESPONSE,
            User(),
            FailureKind.PROTOCOL,
            "Hacker News returned malformed user JSON",
        )
    if username != requested or created < 0 or karma < 0:
        return _failed(
            "getUser",
            INVALID_RESPONSE,
            User(),
            FailureKind.PROTOCOL,
            "Hacker News returned invalid user data",
        )
    if any(item_id <= 0 for item_id in ids):
        return _failed(
            "getUser",
            INVALID_RESPONSE,
            User(),
            FailureKind.PROTOCOL,
            "Hacker News returned an invalid submitted item ID",
        )
    user = User(
        username, created, karma, about, ids, "https://news.ycombinator.com/user?id=" + quote_plus(username)
    )
    return QueryAttempt.branch_result(FOUND, user, Receipt())


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, FAILURE_PROVIDER, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, FAILURE_PROVIDER, operation, message), after_seconds)


def _decode_story_ids(body: bytes, limit: int) -> QueryAttempt[StoryIDs]:
    operation = LIST_STORY_IDS_OPERATION_ID
    try:
        ids = json.loads(body)
        if not isinstance(ids, list) or any(type(value) is not int for value in ids):
            raise ValueError
    except (ValueError, UnicodeDecodeError):
        return _failed(
            operation,
            INVALID_RESPONSE,
            StoryIDs(),
            FailureKind.PROTOCOL,
            "Hacker News returned an invalid feed",
        )
    if any(value <= 0 for value in ids):
        return _failed(
            operation,
            INVALID_RESPONSE,
            StoryIDs(),
            FailureKind.PROTOCOL,
            "Hacker News returned an invalid item ID",
        )
    return QueryAttempt.branch_result(
        LISTED, StoryIDs(tuple(ids[:limit]), len(ids) > limit, datetime.now(UTC)), Receipt()
    )


def _decode_item(body: bytes, requested: int) -> QueryAttempt[Item]:
    operation = GET_ITEM_OPERATION_ID
    try:
        document = json.loads(body)
        item = None if document is None else _item(document)
    except (ValueError, TypeError, UnicodeDecodeError):
        return _failed(
            operation,
            INVALID_RESPONSE,
            Item(),
            FailureKind.PROTOCOL,
            "Hacker News returned malformed item JSON",
        )
    if item is None or item.deleted or item.dead:
        return _failed(
            operation, NOT_FOUND, Item(), FailureKind.NOT_FOUND, "Hacker News item is deleted or dead"
        )
    if item.id != requested:
        return _failed(
            operation,
            INVALID_RESPONSE,
            Item(),
            FailureKind.PROTOCOL,
            "Hacker News returned a different item ID",
        )
    if item.type not in ITEM_TYPES:
        return _failed(
            operation,
            INVALID_RESPONSE,
            Item(),
            FailureKind.PROTOCOL,
            "Hacker News returned an unknown item type",
        )
    if any(kid <= 0 for kid in item.kids):
        return _failed(
            operation,
            INVALID_RESPONSE,
            Item(),
            FailureKind.PROTOCOL,
            "Hacker News returned an invalid child ID",
        )
    return QueryAttempt.branch_result(FOUND, item, Receipt())


def _item(document: Any) -> Item:
    if not isinstance(document, dict):
        raise ValueError
    kids = document.get("kids")
    if kids is None:
        kids = []
    if not isinstance(kids, list) or any(type(kid) is not int for kid in kids):
        raise ValueError
    item_id = _integer(document, "id")
    return Item(
        id=item_id,
        type=_string(document, "type"),
        discussion_url=f"https://news.ycombinator.com/item?id={item_id}",
        by=_string(document, "by"),
        time=_integer(document, "time"),
        title=_string(document, "title"),
        text_html=_string(document, "text"),
        url=_string(document, "url"),
        score=_integer(document, "score"),
        descendants=_integer(document, "descendants"),
        parent=_integer(document, "parent"),
        kids=tuple(kids),
        deleted=_boolean(document, "deleted"),
        dead=_boolean(document, "dead"),
    )


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


def _integer(document: dict[str, Any], name: str) -> int:
    value = document.get(name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value


def _boolean(document: dict[str, Any], name: str) -> bool:
    value = document.get(name)
    if value is None:
        return False
    if not isinstance(value, bool):
        raise ValueError
    return value


class _InvalidCommentTree(ValueError):
    pass


def _article_node(value: Any) -> dict[str, Any]:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError
    node = {key.lower(): field for key, field in value.items() if field is not None}
    for name in ("title", "text", "url", "author"):
        _string(node, name)
    for name in ("id", "parent_id"):
        if not -(2**63) <= _integer(node, name) < 2**63:
            raise ValueError
    if not isinstance(node.get("children", []), list):
        raise ValueError
    return node


def _decode_article_thread(body: bytes, requested: int) -> QueryAttempt[ArticleThread]:
    operation = "getArticleThread"
    try:
        value = json.loads(body, parse_constant=_reject_non_json_number)
        if value is None:
            return _failed(
                operation,
                NOT_FOUND,
                ArticleThread(),
                FailureKind.NOT_FOUND,
                "Hacker News article was not found",
            )
        node = _article_node(value)
        if node.get("id") != requested or not node.get("title"):
            return _failed(
                operation,
                INVALID_RESPONSE,
                ArticleThread(),
                FailureKind.PROTOCOL,
                "Hacker News Search returned an invalid article",
            )
        comments = _CommentTreeReader().read_children(node, 0)
    except _InvalidCommentTree:
        return _failed(
            operation,
            INVALID_RESPONSE,
            ArticleThread(),
            FailureKind.PROTOCOL,
            "Hacker News Search returned an invalid comment tree",
        )
    except (ValueError, UnicodeError, RecursionError):
        return _failed(
            operation,
            INVALID_RESPONSE,
            ArticleThread(),
            FailureKind.PROTOCOL,
            "Hacker News Search returned malformed article JSON",
        )
    article = ArticleThread(
        id=requested,
        title=node["title"],
        author=_string(node, "author"),
        url=_string(node, "url"),
        text_html=_string(node, "text"),
        discussion_url=f"https://news.ycombinator.com/item?id={requested}",
        comments=comments,
    )
    return QueryAttempt.branch_result(FOUND, article, Receipt())


class _CommentTreeReader:
    def __init__(self) -> None:
        self.remaining = 5000

    def read_children(self, parent: dict[str, Any], depth: int) -> tuple[ArticleComment, ...]:
        children = parent.get("children", [])
        if depth >= 64 or len(children) > self.remaining:
            raise _InvalidCommentTree
        self.remaining -= len(children)
        comments = []
        for child in children:
            node = _article_node(child)
            comment_id = _integer(node, "id")
            if comment_id <= 0 or _integer(node, "parent_id") not in (0, parent["id"]):
                raise _InvalidCommentTree
            comments.append(
                ArticleComment(
                    id=comment_id,
                    author=_string(node, "author"),
                    text_html=_string(node, "text"),
                    discussion_url=f"https://news.ycombinator.com/item?id={comment_id}",
                    replies=self.read_children(node, depth + 1),
                )
            )
        return tuple(comments)
