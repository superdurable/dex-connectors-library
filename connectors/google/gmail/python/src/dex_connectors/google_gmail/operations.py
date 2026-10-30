# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail provider operations and Go-compatible failure classification."""

import json
import re
from datetime import UTC, datetime

import httpx
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
from dex_connectors.sdk.singledispatch import Fence

from .definitions import GET_MESSAGE_DEFINITION, REPLY_TO_MESSAGE_DEFINITION, SEND_MESSAGE_DEFINITION
from .models import GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput
from .provider import (
    GmailProvider,
    GmailResponseReadFailure,
    GmailResponseTooLarge,
    ProviderTransportError,
    decode_message,
)
from .send import SendExecution, SendService


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "gmail", operation, message)


def _receipt(call: Call, headers: httpx.Headers, object_id: str = "") -> Receipt:
    request_id = headers.get("X-Goog-Request-Id") or headers.get("X-Request-Id", "")
    return Receipt(call.id, "gmail", datetime.now(UTC), call.idempotency_key, object_id, request_id)


def _retry_after(headers: httpx.Headers) -> float:
    value = headers.get("Retry-After", "")
    if not value:
        return 0
    if not re.fullmatch(r"[+-]?[0-9]+", value):
        raise ValueError("Gmail returned an invalid Retry-After header")
    seconds = int(value)
    if seconds < 0 or seconds > 2**63 - 1:
        raise ValueError("Gmail returned an invalid Retry-After header")
    return float(seconds)


def _status_failure_kind(status_code: int) -> FailureKind:
    return {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
    }.get(status_code, FailureKind.PROVIDER_REJECTION)


class GetMessageOperation:
    """Read one full Gmail message through the Query interface."""

    def __init__(self, provider: GmailProvider) -> None:
        if provider is None:
            raise ValueError("Gmail provider is required")
        self._provider = provider

    def definition(self) -> QueryDefinition:
        """Return the manifest's stable getMessage routes and Step defaults."""
        return GET_MESSAGE_DEFINITION

    async def invoke(self, call: Call, input: GetMessageInput) -> QueryAttempt[Message]:
        """Read and decode one message; classify a definite outcome or safe retry."""
        if not input.message_id.strip():
            return QueryAttempt.branch_result(
                "defect",
                Message(),
                Receipt(),
                _failure("getMessage", FailureKind.VALIDATION, "message ID is required"),
            )
        try:
            credential = await self._provider.resolve_credentials(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                Message(),
                Receipt(),
                _failure("getMessage", FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        status = 0
        headers = httpx.Headers()
        try:
            response, _ = await self._provider.read_message(call, credential, input.message_id, "full")
            status, headers = response.status_code, response.headers
        except (GmailResponseTooLarge, GmailResponseReadFailure) as error:
            status, headers = error.status_code, error.headers
            response = None
        except ProviderTransportError:
            response = None
        if status == 429 or status >= 500 or status == 0:
            try:
                delay = _retry_after(headers)
            except ValueError:
                return QueryAttempt.retry(
                    _failure(
                        "getMessage", FailureKind.PROTOCOL, "Gmail returned an invalid Retry-After header"
                    )
                )
            return QueryAttempt.retry(
                _failure("getMessage", FailureKind.AVAILABILITY, "Gmail message is temporarily unavailable"),
                delay,
            )
        if status == 404:
            return QueryAttempt.branch_result("notFound", Message(), Receipt())
        if 200 <= status < 300:
            if response is None:
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    Message(),
                    Receipt(),
                    _failure(
                        "getMessage",
                        FailureKind.PROTOCOL,
                        "Gmail returned an invalid or oversized message response",
                    ),
                )
            try:
                resource = json.loads(response.body)
            except (ValueError, UnicodeDecodeError, KeyError, TypeError):
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    Message(),
                    Receipt(),
                    _failure(
                        "getMessage", FailureKind.PROTOCOL,
                        "Gmail returned an invalid or oversized message response",
                    ),
                )
            try:
                message = decode_message(resource)
            except (ValueError, UnicodeDecodeError, KeyError, TypeError):
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    Message(),
                    _receipt(call, headers, input.message_id),
                    _failure("getMessage", FailureKind.PROTOCOL, "Gmail returned an invalid message"),
                )
            receipt = Receipt(
                call.id,
                "gmail",
                datetime.now(UTC),
                provider_object_id=message.message_id,
                provider_request_id=headers.get("X-Goog-Request-Id") or headers.get("X-Request-Id", ""),
            )
            return QueryAttempt.branch_result("read", message, receipt)
        return QueryAttempt.branch_result(
            "providerRejected",
            Message(),
            Receipt(),
            _failure("getMessage", _status_failure_kind(status), "Gmail message lookup failed"),
        )


class SendMessageOperation:
    """Send one message using the run-scoped read-back fence when Dex execution metadata is available."""

    def __init__(self, provider: GmailProvider) -> None:
        if provider is None:
            raise ValueError("Gmail provider is required")
        self._send_service = SendService(provider)

    def definition(self) -> MutationDefinition:
        """Return stable sendMessage routes and bounded duplicate-risk retry policy."""
        return SEND_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendMessageInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as Gmail's correlation key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendMessageInput) -> MutationAttempt[SendMessageOutput]:
        """Send once per attempt using Dex's run identity, deadline, and Sent Mail fence."""
        context = call.context
        if context is None or context.deadline is None:
            return MutationAttempt.branch_result(
                "defect",
                SendMessageOutput(),
                Receipt(),
                _failure("sendMessage", FailureKind.LOCAL_DEFECT, "Dex Execute context is unavailable"),
            )
        return await self._send_service.send_message(
            SendExecution(
                call, context.attempt, context.run_id, context.flow_started_at, context.deadline,
                SEND_MESSAGE_DEFINITION.step_defaults.execute_method_timeout,
                Fence(call, SEND_MESSAGE_DEFINITION.step_defaults.execute_method_timeout),
            ),
            input,
        )


class ReplyToMessageOperation:
    """Reply in a Gmail thread using the run-scoped read-back fence."""

    def __init__(self, provider: GmailProvider) -> None:
        if provider is None:
            raise ValueError("Gmail provider is required")
        self._send_service = SendService(provider)

    def definition(self) -> MutationDefinition:
        """Return stable replyToMessage routes and bounded duplicate-risk retry policy."""
        return REPLY_TO_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplyToMessageInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as Gmail's correlation key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: ReplyToMessageInput) -> MutationAttempt[SendMessageOutput]:
        """Reply once per attempt using Dex's run identity, deadline, and Sent Mail fence."""
        context = call.context
        if context is None or context.deadline is None:
            return MutationAttempt.branch_result(
                "defect",
                SendMessageOutput(),
                Receipt(),
                _failure("replyToMessage", FailureKind.LOCAL_DEFECT, "Dex Execute context is unavailable"),
            )
        return await self._send_service.reply_to_message(
            SendExecution(
                call, context.attempt, context.run_id, context.flow_started_at, context.deadline,
                REPLY_TO_MESSAGE_DEFINITION.step_defaults.execute_method_timeout,
                Fence(call, REPLY_TO_MESSAGE_DEFINITION.step_defaults.execute_method_timeout),
            ),
            input,
        )
