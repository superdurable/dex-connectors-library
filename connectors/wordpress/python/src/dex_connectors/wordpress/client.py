# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WordPress REST operations with bounded reads and single-dispatch creation."""

import base64
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.config import Config as _GeneratedConfig
from ._generated.config import Credentials as _GeneratedCredentials
from ._generated.definitions import (
    CONNECTOR_ID,
    CREATE_POST_DEFINITION,
    CREATED,
    DEFECT,
    FOUND,
    GET_POST_DEFINITION,
    INVALID_RESPONSE,
    LIST_POSTS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from .models import (
    CreatedPost,
    CreatePostInput,
    GetPostInput,
    ListPostsInput,
    Post,
    PostPage,
    RenderedText,
)

_Output = TypeVar("_Output", Post, PostPage)
_CREATED_STATUSES = frozenset({"draft", "publish", "pending", "private", "future"})


class Config(_GeneratedConfig):
    """Site URL, request timeout, and complete request and response limits."""

    __slots__ = ()


class Credentials(_GeneratedCredentials):
    """WordPress account and application password resolved for each call."""

    __slots__ = ()


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider outcome without exposing the credential-bearing request."""

    status: int = 0
    body: bytes = b""
    headers: httpx.Headers | None = None
    retry_after: float = 0
    error: str = ""
    request_sent: bool = False


class Client:
    """Resolve application credentials per attempt and send redirect-safe requests."""

    def __init__(
        self, config: Config, credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("WordPress configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0 or config.max_post_bytes <= 0:
            raise ValueError("WordPress limits must be positive")
        self.site_url = validate_base_url(config.site_url)
        if self.site_url.endswith("/wp-json"):
            raise ValueError("WordPress site URL must omit /wp-json")
        self.max_response_bytes = config.max_response_bytes
        self.max_post_bytes = config.max_post_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def create_post(self) -> "CreatePostOperation":
        """Bind the single-dispatch post creation Mutation."""
        return CreatePostOperation(self)

    def get_post(self) -> "GetPostOperation":
        """Bind the published post lookup Query."""
        return GetPostOperation(self)

    def list_posts(self) -> "ListPostsOperation":
        """Bind the published post page Query."""
        return ListPostsOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()

    async def _authorization(self, call: Call) -> str:
        """Build one Basic authorization value without returning secrets in failures."""
        try:
            credentials = await self.credentials.resolve(call)
            username = credentials.username
            password = credentials.application_password.reveal().replace(" ", "")
        except (ValueError, LookupError, OSError, AttributeError):
            return ""
        if not username or any(character in username for character in ":\r\n\x00"):
            return ""
        if not password or any(character in password for character in "\r\n\x00"):
            return ""
        token = base64.b64encode(f"{username}:{password}".encode()).decode("ascii")
        return f"Basic {token}"

    async def request(self, request: httpx.Request) -> Exchange:
        """Read a bounded response while preserving positive no-send evidence."""
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            return Exchange(error="request", request_sent=not error.provably_not_sent)
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
                read_error = ""
            except BodyTooLargeError:
                body, read_error = b"", "too_large"
            except ProviderTransportError:
                body, read_error = b"", "read"
            return Exchange(response.status_code, body, response.headers, retry_after, read_error, True)
        finally:
            await response.aclose()


class CreatePostOperation:
    """Create one post with a Dex heartbeat fence and no provider deduplication key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("WordPress client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated sync Execute creation contract."""
        return CREATE_POST_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreatePostInput) -> IdempotencyKey:
        """Retain stable Dex call identity; WordPress does not receive it."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreatePostInput) -> MutationAttempt[CreatedPost]:
        """Dispatch at most once unless WordPress definitely declined the write."""
        try:
            fence = Fence(call, CREATE_POST_DEFINITION.step_defaults.execute_method_timeout)
            decision = await fence.decide()
        except ValueError:
            return _create_failure(
                DEFECT, FailureKind.LOCAL_DEFECT, "WordPress dispatch fence is unavailable"
            )
        if decision.kind != DecisionKind.SEND_ALLOWED:
            return _create_uncertain(FailureKind.TRANSPORT, "WordPress post outcome cannot be confirmed")
        status = input.status or "draft"
        if not input.title.strip() or status not in {"draft", "publish"}:
            return _create_failure(DEFECT, FailureKind.VALIDATION, "title, content, or status is invalid")
        payload = json.dumps({"title": input.title, "content": input.content, "status": status},
                             ensure_ascii=False, separators=(",", ":")).encode()
        if len(payload) > self.client.max_post_bytes:
            return _create_failure(
                DEFECT, FailureKind.VALIDATION, "post exceeds the configured request limit"
            )
        authorization = await self.client._authorization(call)
        if not authorization:
            return _create_failure(DEFECT, FailureKind.AUTHENTICATION, "connection credentials are invalid")
        request = self.client.http_client.build_request(
            "POST", self.client.site_url + "/wp-json/wp/v2/posts",
            headers={"Accept": "application/json", "Content-Type": "application/json",
                     "Authorization": authorization}, content=payload,
        )

        async def send(_deadline: datetime) -> Exchange:
            return await self.client.request(request)

        try:
            exchange = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _create_retry(FailureKind.TRANSPORT, "WordPress dispatch was not started")
        except Exception:
            return _create_uncertain(FailureKind.TRANSPORT, "WordPress post outcome is unknown")
        if exchange.error == "request":
            if not exchange.request_sent:
                try:
                    await fence.record_can_resend()
                    return _create_retry(
                        FailureKind.TRANSPORT, "WordPress could not be reached before sending"
                    )
                except Exception:
                    pass
            return _create_uncertain(FailureKind.TRANSPORT, "WordPress post outcome is unknown")
        if exchange.status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return _create_retry(FailureKind.AVAILABILITY, "WordPress retry checkpoint is unavailable")
            kind = FailureKind.RATE_LIMIT if exchange.status == 429 else FailureKind.AVAILABILITY
            return _create_retry(kind, "WordPress did not create the post", exchange.retry_after)
        if exchange.status >= 500 or exchange.status in (408, 409):
            return _create_uncertain(FailureKind.AVAILABILITY, "WordPress post outcome is unknown")
        if exchange.status in (401, 403):
            kind = FailureKind.AUTHENTICATION if exchange.status == 401 else FailureKind.AUTHORIZATION
            return _create_failure(PROVIDER_REJECTED, kind, "WordPress rejected post creation")
        if 400 <= exchange.status < 500:
            return _create_failure(PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION,
                                   f"WordPress rejected HTTP {exchange.status}")
        if exchange.status != 201:
            return _create_uncertain(FailureKind.PROTOCOL, "WordPress did not confirm post creation")
        if exchange.error:
            return _create_uncertain(FailureKind.TRANSPORT, "WordPress post response could not be read")
        try:
            body = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            body = None
        if not isinstance(body, dict) or type(body.get("id")) is not int or body["id"] <= 0 or \
                body.get("status") not in _CREATED_STATUSES:
            return _create_uncertain(FailureKind.PROTOCOL, "WordPress returned no usable post ID")
        try:
            await fence.record_provider_id(str(body["id"]))
        except Exception:
            pass  # A claimed dispatch still forbids another send.
        return MutationAttempt.branch_result(CREATED, CreatedPost(body["id"], body["status"]), Receipt())


class GetPostOperation:
    """Read one published post by numeric ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("WordPress client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated post lookup contract."""
        return GET_POST_DEFINITION

    async def invoke(self, call: Call, input: GetPostInput) -> QueryAttempt[Post]:
        """Return only published post data."""
        if input.post_id <= 0:
            return _query_failure("getPost", DEFECT, FailureKind.VALIDATION, Post())
        return await _query(self.client, call, "getPost", f"/wp-json/wp/v2/posts/{input.post_id}",
                            "", Post(), FOUND)


class ListPostsOperation:
    """Read one bounded page of published posts."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("WordPress client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated page query contract."""
        return LIST_POSTS_DEFINITION

    async def invoke(self, call: Call, input: ListPostsInput) -> QueryAttempt[PostPage]:
        """Validate pagination and an optional RFC 3339 lower bound."""
        if input.page < 0 or input.per_page < 0 or input.per_page > 100:
            return _query_failure("listPosts", DEFECT, FailureKind.VALIDATION, PostPage())
        if input.after:
            try:
                datetime.fromisoformat(input.after.replace("Z", "+00:00"))
            except ValueError:
                return _query_failure("listPosts", DEFECT, FailureKind.VALIDATION, PostPage())
        page = input.page or 1
        parameters = {"status": "publish", "page": str(page)}
        if input.per_page:
            parameters["per_page"] = str(input.per_page)
        if input.after:
            parameters["after"] = input.after
        return await _query(self.client, call, "listPosts", "/wp-json/wp/v2/posts",
                            urlencode(parameters), PostPage(), LISTED, page)


async def _query(client: Client, call: Call, operation: str, path: str, query: str,
                 empty: _Output, success_branch: str, page: int = 1) -> QueryAttempt[_Output]:
    authorization = await client._authorization(call)
    if not authorization:
        return _query_failure(operation, DEFECT, FailureKind.AUTHENTICATION, empty)
    endpoint = client.site_url + path + ("?" + query if query else "")
    request = client.http_client.build_request(
        "GET", endpoint, headers={"Accept": "application/json", "Authorization": authorization}
    )
    exchange = await client.request(request)
    if exchange.error == "request":
        return _query_retry(operation, FailureKind.AVAILABILITY)
    if exchange.status == 429:
        return _query_retry(operation, FailureKind.RATE_LIMIT, exchange.retry_after)
    if exchange.status in (408, 409) or exchange.status >= 500:
        return _query_retry(operation, FailureKind.AVAILABILITY, exchange.retry_after)
    if exchange.status == 404 and operation == "getPost":
        return _query_failure(operation, NOT_FOUND, FailureKind.NOT_FOUND, empty)
    if exchange.status in (401, 403):
        kind = FailureKind.AUTHENTICATION if exchange.status == 401 else FailureKind.AUTHORIZATION
        return _query_failure(operation, PROVIDER_REJECTED, kind, empty)
    if exchange.status != 200:
        return _query_failure(operation, PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, empty)
    if exchange.error == "too_large":
        return _query_failure(operation, INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, empty)
    if exchange.error:
        return _query_retry(operation, FailureKind.AVAILABILITY)
    try:
        body = json.loads(exchange.body)
        if operation == "getPost":
            result: Any = _decode_post(body)
        else:
            result = _decode_page(body, exchange.headers, page)
    except _PrivatePostError:
        return _query_failure(operation, NOT_FOUND, FailureKind.NOT_FOUND, empty)
    except (ValueError, TypeError, KeyError, UnicodeDecodeError):
        return _query_failure(operation, INVALID_RESPONSE, FailureKind.PROTOCOL, empty)
    return QueryAttempt.branch_result(success_branch, result, Receipt())


class _PrivatePostError(ValueError):
    """A draft must not enter a published-post Flow."""


def _decode_post(value: Any) -> Post:
    if not isinstance(value, dict):
        raise ValueError("post is not an object")
    if value.get("status") != "publish":
        raise _PrivatePostError("post is not published")
    if type(value.get("id")) is not int or value["id"] <= 0 or not all(
        isinstance(value.get(field), str) and value[field] for field in ("slug", "link", "date_gmt")
    ):
        raise ValueError("post is incomplete")
    datetime.strptime(value["date_gmt"], "%Y-%m-%dT%H:%M:%S")
    title = value.get("title")
    content = value.get("content")
    if not isinstance(title, dict):
        title = {}
    if not isinstance(content, dict):
        content = {}
    return Post(value["id"], value["slug"], "publish", value["link"],
                RenderedText(str(title.get("rendered", ""))), RenderedText(str(content.get("rendered", ""))),
                value["date_gmt"])


def _decode_page(value: Any, headers: httpx.Headers | None, page: int) -> PostPage:
    if not isinstance(value, list) or headers is None:
        raise ValueError("post page is invalid")
    total = int(headers["X-WP-Total"])
    total_pages = int(headers["X-WP-TotalPages"])
    if total < 0 or total_pages < 0:
        raise ValueError("post totals are invalid")
    return PostPage(tuple(_decode_post(item) for item in value), page, total, total_pages)


def _failure(operation: str, kind: FailureKind) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, "WordPress provider operation failed")


def _create_failure(branch: str, kind: FailureKind, message: str) -> MutationAttempt[CreatedPost]:
    return MutationAttempt.branch_result(branch, CreatedPost(), Receipt(),
                                         Failure(kind, CONNECTOR_ID, "createPost", message))


def _create_retry(kind: FailureKind, message: str, after: float = 0) -> MutationAttempt[CreatedPost]:
    return MutationAttempt.retry(Failure(kind, CONNECTOR_ID, "createPost", message), after)


def _create_uncertain(kind: FailureKind, message: str) -> MutationAttempt[CreatedPost]:
    return MutationAttempt.uncertain(
        CreatedPost(), Failure(kind, CONNECTOR_ID, "createPost", message), Receipt()
    )


def _query_failure(operation: str, branch: str, kind: FailureKind,
                   empty: _Output) -> QueryAttempt[_Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), _failure(operation, kind))


def _query_retry(operation: str, kind: FailureKind, after: float = 0) -> QueryAttempt[Any]:
    return QueryAttempt.retry(_failure(operation, kind), after)
