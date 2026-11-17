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
        """Schedule under a single-dispatch fence, resending an unknown outcome below the send cap.

        Hootsuite has no message idempotency key, so an unknown outcome retries while possible
        sends stay below `call.max_sends` and selects uncertain at the cap. A definite refusal
        is never resent.
        """
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
        try:
            fence = Fence(
                call,
                definitions.SCHEDULE_POST_DEFINITION.step_defaults.execute_method_timeout,
                max_sends=call.max_sends,
            )
            decision = await fence.decide()
        except ValueError:
            return _mutation_branch(
                "defect", FailureKind.LOCAL_DEFECT, "Hootsuite dispatch fence is unavailable"
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return _mutation_retry(
                FailureKind.AVAILABILITY,
                "Hootsuite dispatch checkpoint is unreadable; no message was scheduled in this attempt",
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return _mutation_uncertain(
                FailureKind.TRANSPORT,
                "an earlier attempt may have scheduled the Hootsuite message, and the send cap is reached; "
                "inspect Planner before resending",
            )
        if (
            decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
            and decision.marker is not None
            and decision.marker.provider_id
        ):
            # A saved message ID proves Hootsuite scheduled the message, so it is never resent.
            return _mutation_uncertain(
                FailureKind.TRANSPORT,
                "an earlier attempt scheduled the Hootsuite message, but its confirmation was lost",
            )
        # This attempt's send would be the last one the cap allows when no further send remains.
        can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        # An earlier send may have scheduled the message, so a refusal of a resend proves nothing.
        has_possible_earlier_send = decision.prior_send_upper_bound > 0
        token, error = await self.client.resolve_token(call)
        if error == "credential":
            return await _local_schedule_failure(
                fence,
                has_possible_earlier_send,
                FailureKind.AUTHENTICATION,
                "connection credentials require reauthorization",
            )
        if error:
            try:
                await fence.record_not_sent()
            except Exception:
                pass  # Without notSent the next attempt counts this attempt as a possible send.
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
            return _resend_or_select_uncertain(
                can_resend_unknown_outcome, FailureKind.TRANSPORT, "Hootsuite schedule outcome is unknown"
            )
        if exchange.error == "request":
            if not exchange.request_sent:
                try:
                    await fence.record_can_resend()
                except Exception:
                    pass  # A lost canResend checkpoint leaves the claim, which the next attempt counts.
                return _mutation_retry(FailureKind.TRANSPORT, "Hootsuite could not be reached before sending")
            return _resend_or_select_uncertain(
                can_resend_unknown_outcome, FailureKind.TRANSPORT, "Hootsuite schedule outcome is unknown"
            )
        if exchange.status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return _mutation_retry(FailureKind.AVAILABILITY, "Hootsuite retry checkpoint is unavailable")
            kind = FailureKind.RATE_LIMIT if exchange.status == 429 else FailureKind.AVAILABILITY
            return _mutation_retry(kind, "Hootsuite did not schedule the message", exchange.retry_after)
        is_definite_refusal_status = exchange.status in _DEFINITE_REFUSAL_STATUSES
        if not is_definite_refusal_status and not 200 <= exchange.status < 300:
            return _resend_or_select_uncertain(
                can_resend_unknown_outcome,
                FailureKind.AVAILABILITY,
                f"Hootsuite schedule outcome is unknown after HTTP {exchange.status}",
                exchange.retry_after,
            )
        if exchange.error:
            return _resend_or_select_uncertain(
                can_resend_unknown_outcome,
                FailureKind.TRANSPORT,
                "Hootsuite schedule response could not be read",
            )
        document = _document(exchange.body)
        data = document.get("data") if document else None
        # A message ID in the body means Hootsuite scheduled the message.
        is_accepted = (
            isinstance(data, list)
            and len(data) > 0
            and isinstance(data[0], dict)
            and isinstance(data[0].get("id"), str)
            and bool(data[0]["id"])
        )
        if is_definite_refusal_status:
            if is_accepted:
                return _mutation_uncertain(
                    FailureKind.PROTOCOL, "Hootsuite returned a contradictory schedule response"
                )
            return await _reject_or_select_uncertain(
                fence,
                has_possible_earlier_send,
                _rejection_kind(exchange.status),
                f"Hootsuite rejected HTTP {exchange.status}",
            )
        if not is_accepted or not isinstance(data, list):
            return _resend_or_select_uncertain(
                can_resend_unknown_outcome, FailureKind.PROTOCOL, "Hootsuite schedule response is malformed"
            )
        try:
            await fence.record_provider_id(data[0]["id"])
        except Exception:
            pass  # A lost provider-ID checkpoint leaves the claim, which still counts this send.
        if (
            exchange.status != 200
            or len(data) != 1
            or not isinstance(data[0].get("state"), str)
            or not data[0]["state"]
        ):
            # Hootsuite reported a message ID, so a resend could duplicate the message.
            return _mutation_uncertain(
                FailureKind.PROTOCOL,
                "Hootsuite accepted the request but returned an unexpected schedule response",
            )
        profile = data[0].get("socialProfile") or {}
        if not isinstance(profile, dict) or (profile.get("id") and profile["id"] != input.social_profile_id):
            return _mutation_uncertain(
                FailureKind.PROTOCOL, "Hootsuite returned an unexpected social profile"
            )
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


# Only these statuses prove Hootsuite refused a write; other 4xx statuses leave the outcome unknown.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _resend_or_select_uncertain(
    can_resend_unknown_outcome: bool, kind: FailureKind, message: str, delay: float = 0
) -> MutationAttempt[ScheduledPostReceipt]:
    """Retry a possibly scheduled message while the send cap allows another send."""
    if can_resend_unknown_outcome:
        # The claim stays the last marker, so the next attempt counts this send.
        return _mutation_retry(kind, message, delay)
    return _mutation_uncertain(kind, message)


async def _reject_or_select_uncertain(
    fence: Fence, has_possible_earlier_send: bool, kind: FailureKind, message: str
) -> MutationAttempt[ScheduledPostReceipt]:
    """Select providerRejected only when no earlier send may have scheduled the message."""
    if has_possible_earlier_send:
        return _mutation_uncertain(kind, message)
    try:
        await fence.record_can_resend()
    except Exception:
        pass  # The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
    return _mutation_branch("providerRejected", kind, message)


async def _local_schedule_failure(
    fence: Fence, has_possible_earlier_send: bool, kind: FailureKind, message: str
) -> MutationAttempt[ScheduledPostReceipt]:
    """Select defect only when no earlier send may have scheduled the message."""
    if has_possible_earlier_send:
        # notSent would hide the earlier possible send, so nothing is recorded.
        return _mutation_retry(kind, message + "; no message was scheduled in this attempt")
    try:
        await fence.record_not_sent()
    except Exception:
        pass  # The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
    return _mutation_branch("defect", kind, message)
