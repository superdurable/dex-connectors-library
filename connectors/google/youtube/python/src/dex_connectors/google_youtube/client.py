# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""YouTube Data API transport and Go-compatible provider classification."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Literal, TypeVar, overload

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    ADD_PLAYLIST_ITEM_DEFINITION,
    ADDED,
    DEFECT,
    FOUND,
    GET_CHANNEL_DEFINITION,
    GET_VIDEO_DEFINITION,
    INVALID_RESPONSE,
    LIST_PLAYLIST_ITEMS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    QUOTA_EXHAUSTED,
    SEARCH_VIDEOS_DEFINITION,
    SEARCHED,
)
from .json_values import strict_json_loads
from .models import (
    AddedPlaylistItem,
    AddPlaylistItemInput,
    Channel,
    GetChannelInput,
    GetVideoInput,
    ListPlaylistItemsInput,
    PlaylistItem,
    PlaylistItemPage,
    SearchVideosInput,
    Video,
    VideoSearchPage,
    VideoSearchResult,
)
from .query_values import encode_query_values

_VIDEO_ID = re.compile(r"[A-Za-z0-9_-]{11}\Z")
_CHANNEL_ID = re.compile(r"UC[A-Za-z0-9_-]{22}\Z")
_PLAYLIST_ID = re.compile(r"[A-Za-z0-9_-]{2,64}\Z")
_HANDLE = re.compile(r"@?[A-Za-z0-9._-]{3,30}\Z")
_PAGE_TOKEN = re.compile(r"[A-Za-z0-9_-]+\Z")
_TIMESTAMP = re.compile(r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})\Z")
_DOCUMENTED_REASONS = {
    "quotaExceeded",
    "dailyLimitExceeded",
    "rateLimitExceeded",
    "userRateLimitExceeded",
    "forbidden",
    "insufficientPermissions",
    "authError",
    "invalidCredentials",
    "badRequest",
    "invalidParameter",
    "videoNotFound",
    "playlistNotFound",
    "channelNotFound",
    "playlistItemsNotAccessible",
    "playlistContainsMaximumNumberOfVideos",
    "manualSortRequired",
    "videoAlreadyInPlaylist",
    "backendError",
    "notFound",
    "youtubeSignupRequired",
}
_Output = TypeVar("_Output")


@dataclass(frozen=True)
class _Outcome:
    disposition: str
    failure: Failure
    receipt: Receipt
    retry_after: float = 0


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "youtube", operation, message)


def _empty_receipt() -> Receipt:
    return Receipt()


@overload
def _branch(
    operation: str,
    branch: str,
    output: _Output,
    kind: FailureKind,
    message: str,
    receipt: Receipt | None = None,
    *,
    mutation: Literal[False] = False,
) -> QueryAttempt[_Output]: ...


@overload
def _branch(
    operation: str,
    branch: str,
    output: _Output,
    kind: FailureKind,
    message: str,
    receipt: Receipt | None = None,
    *,
    mutation: Literal[True],
) -> MutationAttempt[_Output]: ...


def _branch(
    operation: str,
    branch: str,
    output: Any,
    kind: FailureKind,
    message: str,
    receipt: Receipt | None = None,
    *,
    mutation: bool = False,
) -> Any:
    attempt_type = MutationAttempt if mutation else QueryAttempt
    return attempt_type.branch_result(
        branch, output, receipt or _empty_receipt(), _failure(operation, kind, message)
    )


def _as_object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("YouTube returned a malformed response")
    return value


def _object(value: dict[str, Any], key: str) -> dict[str, Any]:
    field = value.get(key, {})
    if field is None:
        return {}
    return _as_object(field)


def _text(value: dict[str, Any], key: str) -> str:
    field = value.get(key, "")
    if field is None:
        return ""
    if not isinstance(field, str):
        raise ValueError("YouTube returned a malformed response")
    return field


def _integer(value: dict[str, Any], key: str) -> int:
    field = value.get(key, 0)
    if field is None:
        return 0
    if type(field) is not int:
        raise ValueError("YouTube returned a malformed response")
    return field


def _items(value: dict[str, Any], *, required: bool) -> list[Any]:
    field = value.get("items")
    if field is None and not required:
        return []
    if not isinstance(field, list):
        raise ValueError("YouTube returned an incomplete page")
    return field


def _decode(body: bytes) -> dict[str, Any]:
    try:
        return _as_object(strict_json_loads(body))
    except (UnicodeError, json.JSONDecodeError, TypeError) as error:
        raise ValueError("YouTube returned a malformed response") from error


def _time(value: str, *, required: bool = False) -> datetime | None:
    if not value.strip():
        if required:
            raise ValueError("YouTube returned an invalid timestamp")
        return None
    match = _TIMESTAMP.fullmatch(value)
    if match is None:
        raise ValueError("YouTube returned an invalid timestamp")
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    if hour > 23 or minute > 59 or second > 59:
        raise ValueError("YouTube returned an invalid timestamp")
    fraction = (match.group(7) or "")[:6].ljust(6, "0")
    try:
        result = datetime(year, month, day, hour, minute, second, int(fraction), tzinfo=UTC)
        zone = match.group(8)
        if zone != "Z":
            offset_hour, offset_minute = int(zone[1:3]), int(zone[4:6])
            if offset_minute > 59:
                raise ValueError("invalid offset")
            offset = timedelta(hours=offset_hour, minutes=offset_minute)
            result += offset if zone[0] == "-" else -offset
        return result
    except ValueError as error:
        raise ValueError("YouTube returned an invalid timestamp") from error


def _count(value: dict[str, Any], key: str) -> int | None:
    field = value.get(key)
    if field is None:
        return None
    if not isinstance(field, str) or not re.fullmatch(r"[+]?[0-9]+", field):
        raise ValueError("YouTube returned an invalid statistic")
    result = int(field)
    if result > 2**63 - 1:
        raise ValueError("YouTube returned an invalid statistic")
    return result


def _page_token(value: str) -> str:
    if value and (len(value) > 256 or not _PAGE_TOKEN.fullmatch(value)):
        raise ValueError("YouTube returned an invalid nextPageToken")
    return value


def _page_query(max_results: int, page_token: str) -> dict[str, str]:
    if type(max_results) is not int or max_results < 0 or max_results > 50:
        raise ValueError("maxResults must be from 1 through 50, or zero for YouTube's default of 5")
    query: dict[str, str] = {}
    if max_results:
        query["maxResults"] = str(max_results)
    if page_token:
        if not isinstance(page_token, str) or len(page_token) > 256 or not _PAGE_TOKEN.fullmatch(page_token):
            raise ValueError("pageToken must be a nextPageToken returned by YouTube")
        query["pageToken"] = page_token
    return query


def _reason(body: bytes) -> str:
    try:
        errors = _object(_decode(body), "error").get("errors")
        if isinstance(errors, list) and errors:
            reason = _text(_as_object(errors[0]), "reason")
            return reason if reason in _DOCUMENTED_REASONS else ""
    except ValueError:
        pass
    return ""


class Client:
    """Execute authenticated YouTube requests through a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("YouTube configuration and credential source are required")
        self.endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        if self.max_response_bytes <= 0:
            raise ValueError("YouTube maxResponseBytes must be positive")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)

    def search_videos(self) -> SearchVideosOperation:
        """Return the video search Query."""
        return SearchVideosOperation(self)

    def get_video(self) -> GetVideoOperation:
        """Return the video read Query."""
        return GetVideoOperation(self)

    def get_channel(self) -> GetChannelOperation:
        """Return the channel read Query."""
        return GetChannelOperation(self)

    def list_playlist_items(self) -> ListPlaylistItemsOperation:
        """Return the playlist page Query."""
        return ListPlaylistItemsOperation(self)

    def add_playlist_item(self) -> AddPlaylistItemOperation:
        """Return the add-once playlist Mutation."""
        return AddPlaylistItemOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport after Worker shutdown."""
        await self.http_client.aclose()

    def _receipt(self, call: Call, object_id: str, headers: httpx.Headers | None = None) -> Receipt:
        request_id = headers.get("X-Goog-Request-Id", "") if headers is not None else ""
        if len(request_id) > 128 or not _PAGE_TOKEN.fullmatch(request_id):
            request_id = ""
        return Receipt(call.id, "youtube", datetime.now(UTC), call.idempotency_key, object_id, request_id)

    async def _exchange(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str],
        object_id: str,
        body: bytes | None = None,
        requires_manage: bool = False,
    ) -> tuple[bytes, Receipt, _Outcome | None]:
        try:
            credential = await self.credentials.resolve(call)
        except Exception as error:
            from dex_connectors.sdk import ReauthorizationRequiredError

            if isinstance(error, ReauthorizationRequiredError):
                failure = _failure(
                    operation, FailureKind.AUTHENTICATION, "the YouTube connection must be authorized again"
                )
                return b"", _empty_receipt(), _Outcome("rejected", failure, _empty_receipt())
            failure = _failure(
                operation,
                FailureKind.AUTHENTICATION,
                "YouTube connection credentials are unavailable or invalid",
            )
            return b"", _empty_receipt(), _Outcome("defect", failure, _empty_receipt())
        token = credential.access_token.reveal() if credential.access_token is not None else ""
        if not is_header_safe_credential(token):
            failure = _failure(
                operation,
                FailureKind.AUTHENTICATION,
                "YouTube connection credentials are unavailable or invalid",
            )
            return b"", _empty_receipt(), _Outcome("defect", failure, _empty_receipt())
        if requires_manage and credential.auth_method_id != "google-oauth-manage":
            receipt = self._receipt(call, object_id)
            failure = _failure(
                operation,
                FailureKind.AUTHORIZATION,
                "addPlaylistItem requires a YouTube connection authorized with the "
                "google-oauth-manage method and its youtube scope",
            )
            return b"", receipt, _Outcome("rejected", failure, receipt)
        url = self.endpoint + path
        if query:
            url += "?" + encode_query_values(query)
        for request_attempt in range(2):
            try:
                headers = {"Accept": "application/json", "Authorization": f"Bearer {token}"}
                if body is not None:
                    headers["Content-Type"] = "application/json"
                request = self.http_client.build_request(method, url, content=body, headers=headers)
            except (httpx.HTTPError, ValueError):
                receipt = self._receipt(call, object_id)
                failure = _failure(operation, FailureKind.LOCAL_DEFECT, "YouTube request could not be built")
                return b"", receipt, _Outcome("defect", failure, receipt)
            try:
                response, _ = await send_traced(self.http_client, request)
            except ProviderTransportError:
                receipt = self._receipt(call, object_id)
                failure = _failure(
                    operation, FailureKind.TRANSPORT, "YouTube did not return a complete response"
                )
                return b"", receipt, _Outcome("retry", failure, receipt)
            if (
                response.status_code == 401
                and request_attempt == 0
                and hasattr(self.credentials, "resolve_after_rejection")
            ):
                try:
                    credential = await self.credentials.resolve_after_rejection(call)
                    token = credential.access_token.reveal() if credential.access_token is not None else ""
                    if is_header_safe_credential(token):
                        continue
                    failure = _failure(
                        operation, FailureKind.AUTHENTICATION,
                        "YouTube connection credentials are unavailable or invalid",
                    )
                    return b"", _empty_receipt(), _Outcome("defect", failure, _empty_receipt())
                except Exception as error:
                    from dex_connectors.sdk import ReauthorizationRequiredError

                    if isinstance(error, ReauthorizationRequiredError):
                        failure = _failure(
                            operation,
                            FailureKind.AUTHENTICATION,
                            "the YouTube connection must be authorized again",
                        )
                        return b"", _empty_receipt(), _Outcome("rejected", failure, _empty_receipt())
                    failure = _failure(
                        operation,
                        FailureKind.AUTHENTICATION,
                        "YouTube connection credentials are unavailable or invalid",
                    )
                    return b"", _empty_receipt(), _Outcome("defect", failure, _empty_receipt())
            break
        receipt = self._receipt(call, object_id, response.headers)
        is_success = 200 <= response.status_code < 300
        try:
            response_body = await read_bounded_body(
                response, self.max_response_bytes if is_success else 65536
            )
        except BodyTooLargeError:
            if is_success:
                failure = _failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "YouTube response exceeds the configured size limit",
                )
                return b"", receipt, _Outcome("invalidResponse", failure, receipt)
            response_body = b""
        except ProviderTransportError:
            failure = _failure(operation, FailureKind.TRANSPORT, "YouTube did not return a complete response")
            return b"", receipt, _Outcome("retry", failure, receipt)
        if is_success:
            return response_body, receipt, None
        reason = _reason(response_body)
        suffix = f", {reason}" if reason else ""
        status = response.status_code
        delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
        if reason in {"quotaExceeded", "dailyLimitExceeded"}:
            disposition, kind, message = (
                "quotaExhausted",
                FailureKind.QUOTA_EXHAUSTED,
                f"the project's daily YouTube Data API quota is spent (HTTP {status}{suffix})",
            )
        elif status == 429 or reason in {"rateLimitExceeded", "userRateLimitExceeded"}:
            disposition, kind, message = (
                "retry",
                FailureKind.RATE_LIMIT,
                f"YouTube rate limited the request (HTTP {status}{suffix})",
            )
        elif status == 408 or status >= 500 and status != 501:
            disposition, kind, message = (
                "retry",
                FailureKind.AVAILABILITY,
                f"YouTube is temporarily unavailable (HTTP {status}{suffix})",
            )
        elif status == 401:
            disposition, kind, message = (
                "rejected",
                FailureKind.AUTHENTICATION,
                f"Google rejected the access token after one refresh (HTTP 401{suffix})",
            )
        elif status == 404:
            disposition, kind, message = (
                "notFound",
                FailureKind.NOT_FOUND,
                f"YouTube found no such video, channel, or playlist (HTTP 404{suffix})",
            )
        elif status == 403:
            disposition, kind, message = (
                "rejected",
                FailureKind.AUTHORIZATION,
                "YouTube denied access: check the youtube scope and that the account owns "
                f"the playlist (HTTP 403{suffix})",
            )
        elif status in (400, 409):
            disposition, kind, message = (
                "rejected",
                FailureKind.VALIDATION,
                f"YouTube rejected the request as invalid (HTTP {status}{suffix})",
            )
        elif 300 <= status < 400:
            disposition, kind, message = (
                "rejected",
                FailureKind.PROTOCOL,
                f"YouTube redirected the request (HTTP {status})",
            )
        else:
            disposition, kind, message = (
                "rejected",
                FailureKind.PROVIDER_REJECTION,
                f"YouTube rejected the request (HTTP {status}{suffix})",
            )
        failure = _failure(operation, kind, message)
        return (
            response_body,
            receipt,
            _Outcome(disposition, failure, receipt, delay if disposition == "retry" else 0),
        )


@overload
def _attempt_for_outcome(
    outcome: _Outcome, output: _Output, *, mutation: Literal[False] = False, has_not_found: bool = True
) -> QueryAttempt[_Output]: ...


@overload
def _attempt_for_outcome(
    outcome: _Outcome, output: _Output, *, mutation: Literal[True], has_not_found: bool = True
) -> MutationAttempt[_Output]: ...


def _attempt_for_outcome(
    outcome: _Outcome, output: Any, *, mutation: bool = False, has_not_found: bool = True
) -> Any:
    attempt_type = MutationAttempt if mutation else QueryAttempt
    if outcome.disposition == "retry":
        return attempt_type.retry(outcome.failure, outcome.retry_after)
    branch = {
        "notFound": NOT_FOUND if has_not_found else PROVIDER_REJECTED,
        "quotaExhausted": QUOTA_EXHAUSTED,
        "rejected": PROVIDER_REJECTED,
        "invalidResponse": INVALID_RESPONSE,
        "defect": DEFECT,
    }[outcome.disposition]
    return attempt_type.branch_result(branch, output, outcome.receipt, outcome.failure)


def _decode_search(body: bytes, max_results: int) -> VideoSearchPage:
    decoded = _decode(body)
    page_info = _object(decoded, "pageInfo")
    if not isinstance(decoded.get("pageInfo"), dict):
        raise ValueError("YouTube returned an incomplete search page")
    items = _items(decoded, required=True)
    if len(items) > max(max_results, 5) and len(items) > 50:
        raise ValueError("YouTube returned more results than the page size")
    results = []
    for raw in items:
        item = _as_object(raw)
        identity, snippet = _object(item, "id"), _object(item, "snippet")
        video_id, channel_id = _text(identity, "videoId"), _text(snippet, "channelId")
        if (
            _text(identity, "kind") != "youtube#video"
            or not _VIDEO_ID.fullmatch(video_id)
            or not _CHANNEL_ID.fullmatch(channel_id)
        ):
            raise ValueError("YouTube returned a search result that is not a valid video")
        published_at = _time(_text(snippet, "publishedAt"), required=True)
        assert published_at is not None
        results.append(
            VideoSearchResult(
                video_id,
                channel_id,
                _text(snippet, "channelTitle"),
                _text(snippet, "title"),
                _text(snippet, "description"),
                published_at,
            )
        )
    return VideoSearchPage(
        tuple(results), _page_token(_text(decoded, "nextPageToken")), _integer(page_info, "totalResults")
    )


def _decode_video(body: bytes, requested_id: str) -> Video | None:
    items = _items(_decode(body), required=True)
    if not items:
        return None
    if len(items) != 1:
        raise ValueError("YouTube returned a different or invalid video")
    item = _as_object(items[0])
    snippet = _object(item, "snippet")
    if _text(item, "id") != requested_id or not _CHANNEL_ID.fullmatch(_text(snippet, "channelId")):
        raise ValueError("YouTube returned a different or invalid video")
    published_at = _time(_text(snippet, "publishedAt"), required=True)
    assert published_at is not None
    stats = _object(item, "statistics")
    return Video(
        requested_id,
        _text(snippet, "channelId"),
        _text(snippet, "channelTitle"),
        _text(snippet, "title"),
        _text(snippet, "description"),
        published_at,
        _text(_object(item, "contentDetails"), "duration"),
        _text(_object(item, "status"), "privacyStatus"),
        _count(stats, "viewCount"),
        _count(stats, "likeCount"),
        _count(stats, "commentCount"),
    )


def _decode_channel(body: bytes, requested_id: str) -> Channel | None:
    items = _items(_decode(body), required=False)
    if not items:
        return None
    if len(items) != 1:
        raise ValueError("YouTube returned more than one channel")
    item = _as_object(items[0])
    identity = _text(item, "id")
    if not _CHANNEL_ID.fullmatch(identity) or requested_id and identity != requested_id:
        raise ValueError("YouTube returned a different channel or an invalid channel ID")
    uploads = _text(_object(_object(item, "contentDetails"), "relatedPlaylists"), "uploads")
    if not _PLAYLIST_ID.fullmatch(uploads):
        raise ValueError("YouTube returned a channel without an uploads playlist")
    snippet, stats = _object(item, "snippet"), _object(item, "statistics")
    published_at = _time(_text(snippet, "publishedAt"), required=True)
    assert published_at is not None
    hidden = stats.get("hiddenSubscriberCount", False)
    if type(hidden) is not bool:
        raise ValueError("YouTube returned a malformed response")
    return Channel(
        identity,
        _text(snippet, "title"),
        _text(snippet, "description"),
        _text(snippet, "customUrl"),
        published_at,
        uploads,
        None if hidden else _count(stats, "subscriberCount"),
        _count(stats, "videoCount"),
        _count(stats, "viewCount"),
    )


def _decode_playlist_item(raw: Any, playlist_id: str) -> PlaylistItem:
    item = _as_object(raw)
    identity, snippet = _text(item, "id"), _object(item, "snippet")
    resource = _object(snippet, "resourceId")
    video_id = _text(resource, "videoId")
    if not identity or len(identity) > 128 or not _PAGE_TOKEN.fullmatch(identity):
        raise ValueError("YouTube returned a playlist item with an invalid ID")
    if (
        _text(snippet, "playlistId") != playlist_id
        or _text(resource, "kind") != "youtube#video"
        or not _VIDEO_ID.fullmatch(video_id)
        or _integer(snippet, "position") < 0
    ):
        raise ValueError("YouTube returned a playlist item for another playlist or not for a valid video")
    added_at = _time(_text(snippet, "publishedAt"), required=True)
    assert added_at is not None
    return PlaylistItem(
        identity,
        playlist_id,
        video_id,
        _text(snippet, "title"),
        _integer(snippet, "position"),
        added_at,
        _time(_text(_object(item, "contentDetails"), "videoPublishedAt")),
    )


def _decode_playlist_page(body: bytes, input: ListPlaylistItemsInput) -> PlaylistItemPage:
    decoded = _decode(body)
    if not isinstance(decoded.get("pageInfo"), dict):
        raise ValueError("YouTube returned an incomplete playlist page")
    items = _items(decoded, required=True)
    if len(items) > 50:
        raise ValueError("YouTube returned more items than the page size")
    converted = tuple(_decode_playlist_item(item, input.playlist_id) for item in items)
    if input.video_id and any(item.video_id != input.video_id for item in converted):
        raise ValueError("YouTube returned an item for a different video than the filter")
    return PlaylistItemPage(
        converted,
        _page_token(_text(decoded, "nextPageToken")),
        _integer(_object(decoded, "pageInfo"), "totalResults"),
    )


def _playlist_query(input: ListPlaylistItemsInput) -> dict[str, str]:
    if not isinstance(input.playlist_id, str) or not _PLAYLIST_ID.fullmatch(input.playlist_id):
        raise ValueError("playlistId must be a YouTube playlist ID")
    query = {"part": "snippet,contentDetails", "playlistId": input.playlist_id}
    if input.video_id:
        if not isinstance(input.video_id, str) or not _VIDEO_ID.fullmatch(input.video_id):
            raise ValueError("videoId must be an 11-character YouTube video ID")
        query["videoId"] = input.video_id
    query.update(_page_query(input.max_results, input.page_token))
    return query


class SearchVideosOperation:
    """Search one page of public YouTube videos."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return SEARCH_VIDEOS_DEFINITION

    async def invoke(self, call: Call, input: SearchVideosInput) -> QueryAttempt[VideoSearchPage]:
        """Validate and dispatch one search request."""
        try:
            if (
                not isinstance(input, SearchVideosInput)
                or not isinstance(input.query, str)
                or not isinstance(input.channel_id, str)
            ):
                raise ValueError("query or channelId must be set")
            if not input.query.strip() and not input.channel_id:
                raise ValueError("query or channelId must be set")
            if len(input.query) > 500:
                raise ValueError("query must be valid UTF-8 of at most 500 characters")
            query = {"part": "snippet", "type": "video"}
            if input.query.strip():
                query["q"] = input.query
            if input.channel_id:
                if not _CHANNEL_ID.fullmatch(input.channel_id):
                    raise ValueError("channelId must be a YouTube channel ID: UC followed by 22 characters")
                query["channelId"] = input.channel_id
            if input.order:
                if input.order not in {"relevance", "date", "viewCount", "rating", "title"}:
                    raise ValueError("order must be relevance, date, viewCount, rating, or title")
                query["order"] = input.order
            if input.published_after is not None:
                query["publishedAfter"] = (
                    input.published_after.astimezone(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")
                )
            query.update(_page_query(input.max_results, input.page_token))
        except (ValueError, TypeError, AttributeError) as error:
            return _branch("searchVideos", DEFECT, VideoSearchPage(), FailureKind.VALIDATION, str(error))
        body, receipt, outcome = await self.client._exchange(
            call, "searchVideos", "GET", "/search", query, input.channel_id
        )
        if outcome is not None:
            return _attempt_for_outcome(outcome, VideoSearchPage(), has_not_found=False)
        try:
            return QueryAttempt.branch_result(SEARCHED, _decode_search(body, input.max_results), receipt)
        except ValueError as error:
            return _branch(
                "searchVideos", INVALID_RESPONSE, VideoSearchPage(), FailureKind.PROTOCOL, str(error), receipt
            )


class GetVideoOperation:
    """Read one YouTube video's details."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_VIDEO_DEFINITION

    async def invoke(self, call: Call, input: GetVideoInput) -> QueryAttempt[Video]:
        """Validate and dispatch one videos.list request."""
        if (
            not isinstance(input, GetVideoInput)
            or not isinstance(input.video_id, str)
            or not _VIDEO_ID.fullmatch(input.video_id)
        ):
            return _branch(
                "getVideo",
                DEFECT,
                Video(),
                FailureKind.VALIDATION,
                "videoId must be an 11-character YouTube video ID",
            )
        body, receipt, outcome = await self.client._exchange(
            call,
            "getVideo",
            "GET",
            "/videos",
            {"id": input.video_id, "part": "snippet,contentDetails,status,statistics"},
            input.video_id,
        )
        if outcome is not None:
            return _attempt_for_outcome(outcome, Video())
        try:
            video = _decode_video(body, input.video_id)
        except ValueError:
            return _branch(
                "getVideo",
                INVALID_RESPONSE,
                Video(),
                FailureKind.PROTOCOL,
                "YouTube returned a different or invalid video",
                receipt,
            )
        if video is None:
            return _branch(
                "getVideo",
                NOT_FOUND,
                Video(),
                FailureKind.NOT_FOUND,
                "no visible YouTube video has the ID",
                receipt,
            )
        return QueryAttempt.branch_result(FOUND, video, receipt)


class GetChannelOperation:
    """Read one YouTube channel by ID, handle, or owning account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_CHANNEL_DEFINITION

    async def invoke(self, call: Call, input: GetChannelInput) -> QueryAttempt[Channel]:
        """Validate and dispatch one channels.list request."""
        try:
            if not isinstance(input, GetChannelInput):
                raise ValueError("set exactly one of channelId, handle, and mine")
            query = {"part": "snippet,contentDetails,statistics"}
            selectors = int(bool(input.channel_id)) + int(bool(input.handle)) + int(bool(input.mine))
            if selectors != 1:
                raise ValueError("set exactly one of channelId, handle, and mine")
            object_id = ""
            if input.channel_id:
                if not _CHANNEL_ID.fullmatch(input.channel_id):
                    raise ValueError("channelId must be a YouTube channel ID: UC followed by 22 characters")
                query["id"] = object_id = input.channel_id
            elif input.handle:
                if not _HANDLE.fullmatch(input.handle):
                    raise ValueError(
                        "handle must be a YouTube handle of 3 to 30 letters, digits, periods, "
                        "hyphens, or underscores"
                    )
                query["forHandle"] = object_id = "@" + input.handle.removeprefix("@")
            else:
                query["mine"] = "true"
        except (ValueError, TypeError) as error:
            return _branch("getChannel", DEFECT, Channel(), FailureKind.VALIDATION, str(error))
        body, receipt, outcome = await self.client._exchange(
            call, "getChannel", "GET", "/channels", query, object_id
        )
        if outcome is not None:
            return _attempt_for_outcome(outcome, Channel())
        try:
            channel = _decode_channel(body, input.channel_id)
        except ValueError as error:
            return _branch(
                "getChannel", INVALID_RESPONSE, Channel(), FailureKind.PROTOCOL, str(error), receipt
            )
        if channel is None:
            return _branch(
                "getChannel",
                NOT_FOUND,
                Channel(),
                FailureKind.NOT_FOUND,
                "no YouTube channel matches the request",
                receipt,
            )
        receipt = Receipt(
            receipt.call_id,
            receipt.provider,
            receipt.observed_at,
            receipt.idempotency_key,
            channel.id,
            receipt.provider_request_id,
        )
        return QueryAttempt.branch_result(FOUND, channel, receipt)


class ListPlaylistItemsOperation:
    """List one page of YouTube playlist items."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_PLAYLIST_ITEMS_DEFINITION

    async def invoke(self, call: Call, input: ListPlaylistItemsInput) -> QueryAttempt[PlaylistItemPage]:
        """Validate and dispatch one playlistItems.list request."""
        try:
            query = _playlist_query(input)
        except (ValueError, TypeError, AttributeError) as error:
            return _branch(
                "listPlaylistItems", DEFECT, PlaylistItemPage(), FailureKind.VALIDATION, str(error)
            )
        body, receipt, outcome = await self.client._exchange(
            call, "listPlaylistItems", "GET", "/playlistItems", query, input.playlist_id
        )
        if outcome is not None:
            return _attempt_for_outcome(outcome, PlaylistItemPage())
        try:
            page = _decode_playlist_page(body, input)
        except ValueError as error:
            return _branch(
                "listPlaylistItems",
                INVALID_RESPONSE,
                PlaylistItemPage(),
                FailureKind.PROTOCOL,
                str(error),
                receipt,
            )
        return QueryAttempt.branch_result(LISTED, page, receipt)


class AddPlaylistItemOperation:
    """Add a video once after checking the playlist for an existing item."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return ADD_PLAYLIST_ITEM_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddPlaylistItemInput) -> IdempotencyKey:
        """Use the stable Dex Call ID for repeat-safe playlist insertion."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: AddPlaylistItemInput) -> MutationAttempt[AddedPlaylistItem]:
        """Check first, then insert only if the video is absent."""
        try:
            list_input = ListPlaylistItemsInput(input.playlist_id, input.video_id, 1)
            query = _playlist_query(list_input)
            if not input.video_id:
                raise ValueError("videoId must be an 11-character YouTube video ID")
        except (ValueError, TypeError, AttributeError) as error:
            return _branch(
                "addPlaylistItem",
                DEFECT,
                AddedPlaylistItem(),
                FailureKind.VALIDATION,
                str(error),
                mutation=True,
            )
        body, receipt, outcome = await self.client._exchange(
            call, "addPlaylistItem", "GET", "/playlistItems", query, input.playlist_id, requires_manage=True
        )
        if outcome is not None:
            return _attempt_for_outcome(outcome, AddedPlaylistItem(), mutation=True)
        try:
            page = _decode_playlist_page(body, list_input)
        except ValueError as error:
            return _branch(
                "addPlaylistItem",
                INVALID_RESPONSE,
                AddedPlaylistItem(),
                FailureKind.PROTOCOL,
                str(error),
                receipt,
                mutation=True,
            )
        if page.items:
            item = page.items[0]
            receipt = Receipt(
                receipt.call_id,
                receipt.provider,
                receipt.observed_at,
                receipt.idempotency_key,
                item.id,
                receipt.provider_request_id,
            )
            return MutationAttempt.branch_result(ADDED, AddedPlaylistItem(item, True), receipt)
        request_body = json.dumps(
            {
                "snippet": {
                    "playlistId": input.playlist_id,
                    "resourceId": {"kind": "youtube#video", "videoId": input.video_id},
                }
            },
            separators=(",", ":"),
        ).encode()
        body, receipt, outcome = await self.client._exchange(
            call,
            "addPlaylistItem",
            "POST",
            "/playlistItems",
            {"part": "snippet,contentDetails"},
            input.playlist_id,
            request_body,
            requires_manage=True,
        )
        if outcome is not None:
            if outcome.disposition == "invalidResponse":
                return MutationAttempt.retry(outcome.failure)
            return _attempt_for_outcome(outcome, AddedPlaylistItem(), mutation=True)
        try:
            decoded_item = _decode(body)
        except ValueError:
            return MutationAttempt.retry(
                _failure(
                    "addPlaylistItem",
                    FailureKind.PROTOCOL,
                    "YouTube returned a malformed insert response; the retry checks the playlist",
                )
            )
        try:
            item = _decode_playlist_item(decoded_item, input.playlist_id)
        except ValueError:
            return _branch(
                "addPlaylistItem",
                INVALID_RESPONSE,
                AddedPlaylistItem(),
                FailureKind.PROTOCOL,
                "YouTube returned an invalid or different inserted item",
                receipt,
                mutation=True,
            )
        if item.video_id != input.video_id:
            return _branch(
                "addPlaylistItem",
                INVALID_RESPONSE,
                AddedPlaylistItem(),
                FailureKind.PROTOCOL,
                "YouTube returned an invalid or different inserted item",
                receipt,
                mutation=True,
            )
        receipt = Receipt(
            receipt.call_id,
            receipt.provider,
            receipt.observed_at,
            receipt.idempotency_key,
            item.id,
            receipt.provider_request_id,
        )
        return MutationAttempt.branch_result(ADDED, AddedPlaylistItem(item), receipt)
