# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Durable single-dispatch publishing for a Facebook Page feed."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    ProviderTransportError,
    is_header_safe_credential,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import PUBLISH_TEXT_POST_DEFINITION
from ._graph_error import decode_graph_error
from .models import PublishedPost, PublishTextPostInput

if TYPE_CHECKING:
    from .client import Client

_PAGE_ID = re.compile(r"[0-9]{1,40}\Z")
_POST_ID = re.compile(r"[0-9]{1,40}(?:_[0-9]{1,40})?\Z")
_OPERATION = "publishTextPost"
# Only these statuses prove Meta refused a post; other 4xx statuses leave the outcome unknown.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "facebook-pages", _OPERATION, message)


def _branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[PublishedPost]:
    return MutationAttempt.branch_result(branch, PublishedPost(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[PublishedPost]:
    return MutationAttempt.retry(_failure(kind, message), delay)


def _uncertain(kind: FailureKind, message: str) -> MutationAttempt[PublishedPost]:
    return MutationAttempt.uncertain(PublishedPost(), _failure(kind, message), Receipt())


@dataclass(slots=True)
class _PublishDispatch:
    client: httpx.AsyncClient
    request: httpx.Request

    async def send(self, write_deadline: datetime) -> httpx.Response:
        """Send one request within the fence's deadline without transport replay."""
        return (await send_traced(self.client, self.request))[0]


class PublishTextPostOperation:
    """Publish one plain-text Page post with a durable retry fence."""

    def __init__(self, client: "Client") -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Page publishing contract."""
        return PUBLISH_TEXT_POST_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PublishTextPostInput) -> IdempotencyKey:
        """Record the stable Call ID; Meta does not deduplicate on this key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: PublishTextPostInput) -> MutationAttempt[PublishedPost]:
        """Publish under a single-dispatch fence, resending an unknown outcome below the send cap.

        Meta has no post idempotency key, so an unknown outcome retries while possible sends
        stay below `call.max_sends` and selects uncertain at the cap. A checkpointed post ID
        completes without resending, and a definite refusal is never resent.
        """
        if (
            not isinstance(input, PublishTextPostInput)
            or not isinstance(input.page_id, str)
            or _PAGE_ID.fullmatch(input.page_id) is None
            or not isinstance(input.message, str)
            or not input.message.strip()
        ):
            return _branch("defect", FailureKind.VALIDATION, "Page ID or message is invalid")
        try:
            message = input.message.encode("utf-8", errors="strict")
        except UnicodeEncodeError:
            return _branch("defect", FailureKind.VALIDATION, "Page ID or message is invalid")
        if len(message) > 65536:
            return _branch("defect", FailureKind.VALIDATION, "Page ID or message is invalid")
        try:
            fence = Fence(
                call,
                PUBLISH_TEXT_POST_DEFINITION.step_defaults.execute_method_timeout,
                max_sends=call.max_sends,
            )
            decision = await fence.decide()
        except Exception:
            return _branch("defect", FailureKind.LOCAL_DEFECT, "Meta dispatch fence is unavailable")
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return _retry(
                FailureKind.AVAILABILITY,
                "Meta dispatch checkpoint is unreadable; no post was sent in this attempt",
            )
        if decision.kind in (DecisionKind.READ_BACK_REQUIRED, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED):
            saved_post_id = decision.marker.provider_id if decision.marker is not None else ""
            # Meta returned this ID only for a post it accepted, so the post is never resent.
            if _POST_ID.fullmatch(saved_post_id) is not None:
                return MutationAttempt.branch_result("published", PublishedPost(saved_post_id), Receipt())
            if decision.kind == DecisionKind.READ_BACK_REQUIRED:
                return _uncertain(
                    FailureKind.TRANSPORT,
                    "an earlier attempt may have published the post, and the send cap is reached",
                )
        # This attempt's send would be the last one the cap allows when no further send remains.
        can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        # An earlier send may have published, so a refusal of a resend proves nothing.
        has_possible_earlier_send = decision.prior_send_upper_bound > 0
        try:
            credential = await self.client.credentials.resolve(call)
            access_token = credential.page_access_token.reveal()
            if not is_header_safe_credential(access_token):
                raise ValueError("invalid Page token")
        except (ValueError, LookupError, OSError, AttributeError):
            return await _local_failure(
                fence,
                has_possible_earlier_send,
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/" + input.page_id + "/feed",
                content=urlencode({"message": input.message}),
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/x-www-form-urlencoded",
                    "Authorization": "Bearer " + access_token,
                    "User-Agent": "Dex Connector (https://superdurable.io)",
                },
            )
        except (ValueError, httpx.HTTPError):
            return await _local_failure(
                fence, has_possible_earlier_send, FailureKind.LOCAL_DEFECT, "post request could not be built"
            )
        try:
            response = await fence.dispatch(_PublishDispatch(self.client.http_client, request).send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _retry(FailureKind.TRANSPORT, "Meta dispatch was not started")
        except ProviderTransportError as error:
            if error.provably_not_sent:
                try:
                    await fence.record_can_resend()
                except Exception:
                    pass  # A lost canResend checkpoint leaves the claim, which the next attempt counts.
                return _retry(FailureKind.TRANSPORT, "Meta could not be reached before sending")
            return _resend_or_select_uncertain(
                can_resend_unknown_outcome, FailureKind.TRANSPORT, "Meta publish outcome is unknown"
            )
        except (DispatchNotAllowedError, httpx.HTTPError, TimeoutError):
            return _resend_or_select_uncertain(
                can_resend_unknown_outcome, FailureKind.TRANSPORT, "Meta publish outcome is unknown"
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body: bytes | None = await read_bounded_body(response, self.client.max_response_bytes)
            except Exception:
                body = None
            graph_error = decode_graph_error(body) if body is not None and 400 <= status < 500 else None
            # A Graph throttling code means Meta refused the post before processing it.
            is_rate_limited = status == 429 or (graph_error is not None and graph_error.is_throttled)
            if is_rate_limited or status == 503:
                try:
                    await fence.record_can_resend()
                except Exception:
                    return _retry(FailureKind.AVAILABILITY, "Meta retry checkpoint is unavailable")
                kind = FailureKind.RATE_LIMIT if is_rate_limited else FailureKind.AVAILABILITY
                return _retry(kind, "Meta did not process the post", retry_after)
            accepted_post_id = _read_post_id(body) if body is not None else ""
            if status in _DEFINITE_REFUSAL_STATUSES:
                if accepted_post_id:
                    # The body reports a created post, so a resend could duplicate it.
                    return _uncertain(FailureKind.PROTOCOL, "Meta returned a contradictory publish response")
                return await _reject_or_select_uncertain(
                    fence, has_possible_earlier_send, _rejection_kind(status), f"Meta rejected HTTP {status}"
                )
            if not 200 <= status < 300:
                return _resend_or_select_uncertain(
                    can_resend_unknown_outcome,
                    FailureKind.AVAILABILITY,
                    f"Meta publish outcome is unknown after HTTP {status}",
                    retry_after,
                )
            if body is None:
                return _resend_or_select_uncertain(
                    can_resend_unknown_outcome,
                    FailureKind.TRANSPORT,
                    "Meta publish response could not be read",
                )
            if not accepted_post_id:
                return _resend_or_select_uncertain(
                    can_resend_unknown_outcome, FailureKind.PROTOCOL, "Meta publish response is malformed"
                )
            try:
                await fence.record_provider_id(accepted_post_id)
            except Exception:
                pass  # A lost provider-ID checkpoint leaves the claim, which still counts this send.
            return MutationAttempt.branch_result("published", PublishedPost(accepted_post_id), Receipt())
        finally:
            await response.aclose()


def _read_post_id(body: bytes) -> str:
    """Return the body's post ID, or empty when the body has no valid one."""
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return ""
    post_id = document.get("id") if isinstance(document, dict) else None
    if not isinstance(post_id, str) or _POST_ID.fullmatch(post_id) is None:
        return ""
    return post_id


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _resend_or_select_uncertain(
    can_resend_unknown_outcome: bool, kind: FailureKind, message: str, delay: float = 0
) -> MutationAttempt[PublishedPost]:
    """Retry a possibly published post while the send cap allows another send."""
    if can_resend_unknown_outcome:
        # The claim stays the last marker, so the next attempt counts this send.
        return _retry(kind, message, delay)
    return _uncertain(kind, message)


async def _reject_or_select_uncertain(
    fence: Fence, has_possible_earlier_send: bool, kind: FailureKind, message: str
) -> MutationAttempt[PublishedPost]:
    """Select providerRejected only when no earlier send may have published."""
    if has_possible_earlier_send:
        return _uncertain(kind, message)
    try:
        await fence.record_can_resend()
    except Exception:
        pass  # The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
    return _branch("providerRejected", kind, message)


async def _local_failure(
    fence: Fence, has_possible_earlier_send: bool, kind: FailureKind, message: str
) -> MutationAttempt[PublishedPost]:
    """Select defect only when no earlier send may have published."""
    if has_possible_earlier_send:
        # notSent would hide the earlier possible send, so nothing is recorded.
        return _retry(kind, message + "; no post was sent in this attempt")
    try:
        await fence.record_not_sent()
    except Exception:
        pass  # The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
    return _branch("defect", kind, message)
