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


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "facebook-pages", _OPERATION, message)


def _branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[PublishedPost]:
    return MutationAttempt.branch_result(branch, PublishedPost(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[PublishedPost]:
    return MutationAttempt.retry(_failure(kind, message), delay)


def _uncertain(kind: FailureKind, message: str) -> MutationAttempt[PublishedPost]:
    return MutationAttempt.uncertain(PublishedPost(), _failure(kind, message), Receipt())


async def _is_graph_throttle_response(response: httpx.Response, max_response_bytes: int) -> bool:
    """Read a bounded error body; unreadable bodies keep status-based handling."""
    try:
        body = await read_bounded_body(response, max_response_bytes)
    except Exception:
        return False
    graph_error = decode_graph_error(body)
    return graph_error is not None and graph_error.is_throttled


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
        """Send once and classify ambiguous provider results as uncertain."""
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
            fence = Fence(call, PUBLISH_TEXT_POST_DEFINITION.step_defaults.execute_method_timeout)
            if (await fence.decide()).kind != DecisionKind.SEND_ALLOWED:
                return _uncertain(
                    FailureKind.TRANSPORT,
                    "Meta publish outcome cannot be confirmed; inspect the Page before retrying",
                )
        except Exception:
            return _branch("defect", FailureKind.LOCAL_DEFECT, "Meta dispatch fence is unavailable")
        try:
            credential = await self.client.credentials.resolve(call)
            access_token = credential.page_access_token.reveal()
            if not is_header_safe_credential(access_token):
                raise ValueError("invalid Page token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
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
            return _branch("defect", FailureKind.LOCAL_DEFECT, "post request could not be built")
        try:
            response = await fence.dispatch(_PublishDispatch(self.client.http_client, request).send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _retry(FailureKind.TRANSPORT, "Meta dispatch was not started")
        except ProviderTransportError as error:
            if error.provably_not_sent:
                try:
                    await fence.record_can_resend()
                    return _retry(FailureKind.TRANSPORT, "Meta could not be reached before sending")
                except Exception:
                    pass
            return _uncertain(FailureKind.TRANSPORT, "Meta publish outcome is unknown")
        except (DispatchNotAllowedError, httpx.HTTPError, TimeoutError):
            return _uncertain(FailureKind.TRANSPORT, "Meta publish outcome is unknown")
        try:
            status = response.status_code
            is_rate_limited = status == 429
            if not is_rate_limited and 400 <= status < 500:
                # A Graph throttling code means Meta refused the post before processing it.
                is_rate_limited = await _is_graph_throttle_response(
                    response, self.client.max_response_bytes
                )
            if is_rate_limited or status == 503:
                try:
                    await fence.record_can_resend()
                except Exception:
                    return _retry(FailureKind.AVAILABILITY, "Meta retry checkpoint is unavailable")
                kind = FailureKind.RATE_LIMIT if is_rate_limited else FailureKind.AVAILABILITY
                delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _retry(kind, "Meta did not process the post", delay)
            if status >= 500 or status in (408, 409):
                return _uncertain(FailureKind.AVAILABILITY, "Meta publish outcome is unknown")
            if 400 <= status < 500:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                if status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"Meta rejected HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except ProviderTransportError:
                return _uncertain(FailureKind.TRANSPORT, "Meta publish response could not be confirmed")
            except Exception:
                return _uncertain(FailureKind.TRANSPORT, "Meta publish response could not be confirmed")
            try:
                document = json.loads(body)
            except (ValueError, UnicodeDecodeError):
                document = None
            post_id = document.get("id") if isinstance(document, dict) else None
            if status != 200 or not isinstance(post_id, str) or _POST_ID.fullmatch(post_id) is None:
                return _uncertain(FailureKind.PROTOCOL, "Meta publish response is incomplete")
            try:
                await fence.record_provider_id(post_id)
            except Exception:
                return _uncertain(FailureKind.LOCAL_DEFECT, "Meta post ID could not be checkpointed")
            return MutationAttempt.branch_result("published", PublishedPost(post_id), Receipt())
        finally:
            await response.aclose()
