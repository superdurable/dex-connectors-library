# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hootsuite operation decisions matching Go provider recordings."""

from __future__ import annotations

import json
from datetime import datetime, timedelta
from typing import TYPE_CHECKING, Any, TypeVar
from urllib.parse import urlencode

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions
from .client import Exchange
from .models import (
    ListScheduledPostsInput,
    ListSocialProfilesInput,
    ScheduledPost,
    ScheduledPostReceipt,
    ScheduledPostsPage,
    SchedulePostInput,
    SocialProfile,
    SocialProfilesPage,
)

if TYPE_CHECKING:
    from .client import Client

Output = TypeVar("Output")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "hootsuite", operation, message)


def _query_branch(
    operation: str, branch: str, kind: FailureKind, message: str, empty: Output
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), _failure(operation, kind, message))


def _query_retry(operation: str, kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Any]:
    return QueryAttempt.retry(_failure(operation, kind, message), delay)


def _query_failure(operation: str, exchange: Exchange, empty: Output) -> QueryAttempt[Output] | None:
    if exchange.error == "credential":
        return _query_branch(
            operation,
            "defect",
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable or invalid",
            empty,
        )
    if exchange.error in ("request", "refresh"):
        return _query_retry(operation, FailureKind.TRANSPORT, "Hootsuite request failed")
    if exchange.status == 429:
        return _query_retry(
            operation, FailureKind.RATE_LIMIT, "Hootsuite rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _query_retry(
            operation, FailureKind.AVAILABILITY, "Hootsuite is temporarily unavailable", exchange.retry_after
        )
    if exchange.status != 200:
        return _query_branch(
            operation,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Hootsuite rejected HTTP {exchange.status}",
            empty,
        )
    if exchange.error == "too_large":
        return _query_branch(
            operation,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Hootsuite response exceeded the configured limit",
            empty,
        )
    if exchange.error:
        return _query_retry(operation, FailureKind.TRANSPORT, "Hootsuite response was interrupted")
    return None


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _safe_query_value(value: str) -> bool:
    return all(ord(character) >= 32 and ord(character) != 127 for character in value)


def _utc_time(value: str) -> datetime | None:
    if not value.endswith("Z"):
        return None
    try:
        return datetime.fromisoformat(value)
    except ValueError:
        return None


class ListSocialProfilesOperation:
    """Read social profiles visible to the connected Hootsuite member."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated social profile Query contract."""
        return definitions.LIST_SOCIAL_PROFILES_DEFINITION

    async def invoke(self, call: Call, _input: ListSocialProfilesInput) -> QueryAttempt[SocialProfilesPage]:
        """Return one bounded list of profile identities."""
        empty = SocialProfilesPage()
        exchange = await self.client.request(call, "GET", "/v1/socialProfiles")
        failure = _query_failure("listSocialProfiles", exchange, empty)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None or not isinstance(document.get("data"), list) or len(document["data"]) > 1000:
            return _query_branch(
                "listSocialProfiles",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Hootsuite returned invalid social profile JSON",
                empty,
            )
        profiles: list[SocialProfile] = []
        for item in document["data"]:
            if not isinstance(item, dict) or any(
                not isinstance(item.get(name), str) or not item[name]
                for name in ("id", "type", "socialNetworkUsername")
            ):
                return _query_branch(
                    "listSocialProfiles",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Hootsuite returned incomplete social profile metadata",
                    empty,
                )
            profiles.append(SocialProfile(item["id"], item["type"], item["socialNetworkUsername"]))
        return QueryAttempt.branch_result("listed", SocialProfilesPage(tuple(profiles)), Receipt())


class ListScheduledPostsOperation:
    """Read one bounded page of scheduled outbound messages."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated scheduled message Query contract."""
        return definitions.LIST_SCHEDULED_POSTS_DEFINITION

    async def invoke(self, call: Call, input: ListScheduledPostsInput) -> QueryAttempt[ScheduledPostsPage]:
        """Return a provider page and its continuation cursor."""
        empty = ScheduledPostsPage()
        start, end = _utc_time(input.start_time), _utc_time(input.end_time)
        if (
            start is None
            or end is None
            or end <= start
            or end - start > timedelta(days=28)
            or len(input.cursor) > 4096
            or not _safe_query_value(input.cursor)
            or len(input.social_profile_id) > 40
            or not _safe_query_value(input.social_profile_id)
        ):
            return _query_branch(
                "listScheduledPosts",
                "defect",
                FailureKind.VALIDATION,
                "scheduled post interval or filter is invalid",
                empty,
            )
        query = {
            "startTime": input.start_time,
            "endTime": input.end_time,
            "state": "SCHEDULED",
            "limit": "100",
        }
        if input.social_profile_id:
            query["socialProfileIds"] = input.social_profile_id
        if input.cursor:
            query["cursor"] = input.cursor
        exchange = await self.client.request(call, "GET", "/v1/messages?" + urlencode(query))
        failure = _query_failure("listScheduledPosts", exchange, empty)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None or not isinstance(document.get("data"), list) or len(document["data"]) > 100:
            return _query_branch(
                "listScheduledPosts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Hootsuite returned invalid scheduled message JSON",
                empty,
            )
        posts: list[ScheduledPost] = []
        for item in document["data"]:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("socialProfile"), dict)
                or any(
                    not isinstance(item.get(name), str) or not item[name]
                    for name in ("id", "state", "scheduledSendTime")
                )
                or item["state"] != "SCHEDULED"
                or not isinstance(item["socialProfile"].get("id"), str)
                or not item["socialProfile"]["id"]
            ):
                return _query_branch(
                    "listScheduledPosts",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Hootsuite returned incomplete scheduled message metadata",
                    empty,
                )
            posts.append(
                ScheduledPost(
                    item["id"], item["state"], item["socialProfile"]["id"], item["scheduledSendTime"]
                )
            )
        metadata = document.get("metadata") or {}
        if not isinstance(metadata, dict):
            return _query_branch(
                "listScheduledPosts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Hootsuite returned an invalid page cursor",
                empty,
            )
        cursor = metadata.get("cursor") or {}
        if not isinstance(cursor, dict):
            return _query_branch(
                "listScheduledPosts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Hootsuite returned an invalid page cursor",
                empty,
            )
        next_cursor = cursor.get("next") or ""
        if not isinstance(next_cursor, str) or len(next_cursor) > 4096 or not _safe_query_value(next_cursor):
            return _query_branch(
                "listScheduledPosts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Hootsuite returned an invalid page cursor",
                empty,
            )
        return QueryAttempt.branch_result("listed", ScheduledPostsPage(tuple(posts), next_cursor), Receipt())


class SchedulePostOperation:
    """Schedule one text message behind a durable single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated schedule Mutation contract."""
        return definitions.SCHEDULE_POST_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: SchedulePostInput) -> IdempotencyKey:
        """Record the Call ID; Hootsuite receives no deduplication key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: SchedulePostInput) -> MutationAttempt[ScheduledPostReceipt]:
        """Send once and fail closed when Hootsuite may have accepted the post."""
        try:
            fence = Fence(call, definitions.SCHEDULE_POST_DEFINITION.step_defaults.execute_method_timeout)
            decision = await fence.decide()
        except ValueError:
            return _mutation_branch(
                "defect", FailureKind.LOCAL_DEFECT, "Hootsuite dispatch fence is unavailable"
            )
        if decision.kind != DecisionKind.SEND_ALLOWED:
            return _mutation_uncertain(
                FailureKind.TRANSPORT,
                "Hootsuite schedule outcome cannot be confirmed; inspect Planner before resending",
            )
        if (
            not isinstance(input.text, str)
            or not input.text.strip()
            or len(input.text.encode()) > 10000
            or not _numeric_id(input.social_profile_id)
            or _utc_time(input.scheduled_send_time) is None
        ):
            return _mutation_branch(
                "defect", FailureKind.VALIDATION, "scheduled message text, profile ID, or UTC time is invalid"
            )
        token, error = await self.client.resolve_token(call)
        if error == "credential":
            return _mutation_branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials require reauthorization"
            )
        if error:
            return _mutation_retry(FailureKind.TRANSPORT, "connection refresh failed")
        payload = {
            "text": input.text,
            "socialProfileIds": [input.social_profile_id],
            "scheduledSendTime": input.scheduled_send_time,
        }

        async def send(_deadline: datetime) -> Exchange:
            return await self.client.request(call, "POST", "/v1/messages", token=token, payload=payload)

        try:
            exchange = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _mutation_retry(FailureKind.TRANSPORT, "Hootsuite dispatch did not start")
        except Exception:
            return _mutation_uncertain(FailureKind.TRANSPORT, "Hootsuite schedule outcome is unknown")
        if exchange.error == "request":
            if not exchange.request_sent:
                try:
                    await fence.record_can_resend()
                    return _mutation_retry(
                        FailureKind.TRANSPORT, "Hootsuite could not be reached before sending"
                    )
                except Exception:
                    pass
            return _mutation_uncertain(FailureKind.TRANSPORT, "Hootsuite schedule outcome is unknown")
        if exchange.status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return _mutation_retry(FailureKind.AVAILABILITY, "Hootsuite retry checkpoint is unavailable")
            kind = FailureKind.RATE_LIMIT if exchange.status == 429 else FailureKind.AVAILABILITY
            return _mutation_retry(kind, "Hootsuite did not schedule the message", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _mutation_uncertain(FailureKind.AVAILABILITY, "Hootsuite schedule outcome is unknown")
        if 400 <= exchange.status < 500:
            return _mutation_branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Hootsuite rejected HTTP {exchange.status}",
            )
        if exchange.status != 200:
            return _mutation_uncertain(
                FailureKind.PROTOCOL, "Hootsuite returned an unexpected schedule response"
            )
        if exchange.error:
            return _mutation_uncertain(
                FailureKind.TRANSPORT, "Hootsuite accepted the request but its response could not be read"
            )
        document = _document(exchange.body)
        data = document.get("data") if document else None
        if (
            not isinstance(data, list)
            or len(data) != 1
            or not isinstance(data[0], dict)
            or not isinstance(data[0].get("id"), str)
            or not data[0]["id"]
            or not isinstance(data[0].get("state"), str)
            or not data[0]["state"]
        ):
            return _mutation_uncertain(
                FailureKind.PROTOCOL, "Hootsuite accepted the request but returned no usable message ID"
            )
        profile = data[0].get("socialProfile") or {}
        if not isinstance(profile, dict) or (profile.get("id") and profile["id"] != input.social_profile_id):
            return _mutation_uncertain(
                FailureKind.PROTOCOL, "Hootsuite returned an unexpected social profile"
            )
        try:
            await fence.record_provider_id(data[0]["id"])
        except Exception:
            pass  # The dispatch marker still forbids another send.
        receipt = ScheduledPostReceipt(data[0]["id"], data[0]["state"], input.social_profile_id)
        return MutationAttempt.branch_result("scheduled", receipt, Receipt())


def _numeric_id(value: str) -> bool:
    return (
        isinstance(value, str) and bool(value) and len(value) <= 40 and value.isascii() and value.isdecimal()
    )


def _mutation_branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[ScheduledPostReceipt]:
    return MutationAttempt.branch_result(
        branch, ScheduledPostReceipt(), Receipt(), _failure("schedulePost", kind, message)
    )


def _mutation_retry(
    kind: FailureKind, message: str, delay: float = 0
) -> MutationAttempt[ScheduledPostReceipt]:
    return MutationAttempt.retry(_failure("schedulePost", kind, message), delay)


def _mutation_uncertain(kind: FailureKind, message: str) -> MutationAttempt[ScheduledPostReceipt]:
    return MutationAttempt.uncertain(
        ScheduledPostReceipt(), _failure("schedulePost", kind, message), Receipt()
    )
