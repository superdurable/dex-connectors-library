# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail send and reply classification with read-back before possible repeats."""

import asyncio
import base64
import json
import re
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from functools import partial

import httpx
from dex_connectors.sdk import Call, Failure, FailureKind, MutationAttempt, Receipt
from dex_connectors.sdk.credentials import ReauthorizationRequiredError
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from .models import Credentials, ReplyToMessageInput, SendMessageInput, SendMessageOutput
from .provider import (
    GmailCredentialMaterialError,
    GmailHTTPResult,
    GmailProvider,
    GmailResponseReadFailure,
    GmailResponseTooLarge,
    ProviderTransportError,
    _format_go_mailbox,
    build_reply_message,
    build_send_message,
    decode_message,
    parse_mailbox,
    rfc_message_id_address,
    validate_send_input,
)


@dataclass(frozen=True, slots=True)
class SendExecution:
    """Public Dex attempt facts needed for Go's run-scoped send fence."""

    # Logical call and credential reference.
    call: Call
    # One-based Dex Execute attempt number.
    attempt: int
    # Server-assigned Dex run identity.
    run_id: str | None
    # Start time of the entire Flow, not this Step attempt.
    flow_started_at: datetime | None
    # Actual Dex attempt deadline, which can precede the configured method timeout.
    deadline: datetime
    # This operation's manifest Execute timeout.
    execute_timeout: timedelta
    # Persisted Dex heartbeat fence; direct classification tests may omit it.
    fence: Fence | None = None


@dataclass(frozen=True, slots=True)
class _OutgoingMessage:
    payload: bytes
    recipients: list[str]
    message_id_address: str
    requested_thread_id: str = ""
    has_pre_send_search: bool = False


@dataclass(frozen=True, slots=True)
class _SendOutcome:
    operation_id: str
    request_name: str
    rejection_message: str


_SEND = _SendOutcome("sendMessage", "send", "Gmail rejected the message")
_REPLY = _SendOutcome("replyToMessage", "reply", "Gmail rejected the reply")
# Statuses that prove Gmail refused the POST before processing it.
_REFUSED_BEFORE_PROCESSING_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


class _SendBudget:
    def __init__(self, execution: SendExecution) -> None:
        now = datetime.now(UTC)
        available = min(
            execution.execute_timeout.total_seconds(), (execution.deadline - now).total_seconds()
        )
        loop_now = asyncio.get_running_loop().time()
        self.attempt_end = loop_now + available * 5 / 6
        self.send_end = loop_now + available * 7 / 12
        self.minimum_send_time = available / 10

    @property
    def pre_send_end(self) -> float:
        return min(self.attempt_end, self.send_end - self.minimum_send_time)

    @property
    def can_send(self) -> bool:
        return (
            self.minimum_send_time > 0
            and self.send_end - asyncio.get_running_loop().time() >= self.minimum_send_time
        )


def _failure(outcome: _SendOutcome, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "gmail", outcome.operation_id, message)


def _receipt(execution: SendExecution, headers: httpx.Headers | None = None, object_id: str = "") -> Receipt:
    request_id = (
        "" if headers is None else headers.get("X-Goog-Request-Id") or headers.get("X-Request-Id", "")
    )
    return Receipt(
        execution.call.id, "gmail", datetime.now(UTC), execution.call.idempotency_key, object_id, request_id
    )


def _retry(
    outcome: _SendOutcome, kind: FailureKind, message: str, after: float = 0
) -> MutationAttempt[SendMessageOutput]:
    return MutationAttempt.retry(_failure(outcome, kind, message), after)


def _branch(
    execution: SendExecution,
    outcome: _SendOutcome,
    branch: str,
    kind: FailureKind,
    message: str,
    receipt: Receipt | None = None,
) -> MutationAttempt[SendMessageOutput]:
    return MutationAttempt.branch_result(
        branch,
        SendMessageOutput(),
        receipt if receipt is not None else Receipt(),
        _failure(outcome, kind, message),
    )


def _retry_after(
    headers: httpx.Headers, outcome: _SendOutcome, kind: FailureKind, message: str
) -> MutationAttempt[SendMessageOutput]:
    value = headers.get("Retry-After", "")
    if value:
        if not re.fullmatch(r"[+-]?[0-9]+", value):
            return _retry(outcome, FailureKind.PROTOCOL, "Gmail returned an invalid Retry-After header")
        seconds = int(value)
        if seconds < 0 or seconds > 2**63 - 1:
            return _retry(outcome, FailureKind.PROTOCOL, "Gmail returned an invalid Retry-After header")
        return _retry(outcome, kind, message, float(seconds))
    return _retry(outcome, kind, message)


def _status_kind(status: int) -> FailureKind:
    return {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
    }.get(status, FailureKind.PROVIDER_REJECTION)


def _has_accepted_message_id(body: bytes) -> bool:
    """Report a refusal body that still names a Gmail message, which contradicts the status."""
    try:
        decoded = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return False
    return isinstance(decoded, dict) and isinstance(decoded.get("id"), str) and bool(decoded["id"])


def _raw_message_size(payload: bytes) -> int:
    encoded = json.loads(payload)["raw"]
    return len(base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4)))


class SendService:
    """Classify one Gmail send attempt using stable Message-ID and Sent Mail read-back."""

    def __init__(self, provider: GmailProvider) -> None:
        if provider is None:
            raise ValueError("Gmail provider is required")
        self._provider = provider

    async def send_message(
        self, execution: SendExecution, input: SendMessageInput
    ) -> MutationAttempt[SendMessageOutput]:
        """Send a MIME message after validation and pre-send retry read-back."""
        if execution.attempt < 1:
            return _branch(
                execution, _SEND, "defect", FailureKind.LOCAL_DEFECT, "Dex Execute attempt number is invalid"
            )
        if not isinstance(execution.run_id, str) or not execution.run_id:
            return _branch(
                execution, _SEND, "defect", FailureKind.LOCAL_DEFECT,
                "the Dex call context has no run ID, so nothing was sent",
            )
        message_id_address = rfc_message_id_address(
            execution.call, execution.run_id, execution.flow_started_at
        )
        fence_result = await self._decide_fence(execution, _SEND)
        if isinstance(fence_result, MutationAttempt):
            return fence_result
        budget = _SendBudget(execution)
        try:
            async with asyncio.timeout_at(budget.attempt_end):
                credential = await self._provider.resolve_credentials(execution.call)
        except Exception as error:
            return await self._unavailable_credentials(execution, _SEND, error)
        try:
            recipients = validate_send_input(input, credential.primary_email)
            payload = build_send_message(
                execution.call,
                credential.primary_email,
                recipients,
                input,
                execution.run_id,
                execution.flow_started_at,
                message_id_address=message_id_address,
            )
            if _raw_message_size(payload) > self._provider.max_message_bytes:
                raise ValueError("message exceeds configured size limit")
        except ValueError as error:
            return await self._defect_before_any_send(execution, _SEND, FailureKind.VALIDATION, str(error))
        outgoing = _OutgoingMessage(payload, recipients, message_id_address)
        if fence_result == DecisionKind.READ_BACK_REQUIRED:
            return await self._read_back_only(execution, budget, credential, _SEND, outgoing)
        return await self._send_and_confirm(execution, budget, credential, _SEND, outgoing, fence_result)

    async def reply_to_message(
        self,
        execution: SendExecution,
        input: ReplyToMessageInput,
    ) -> MutationAttempt[SendMessageOutput]:
        """Read source metadata and send a threaded reply with the same read-back fence."""
        if execution.attempt < 1:
            return _branch(
                execution, _REPLY, "defect", FailureKind.LOCAL_DEFECT, "Dex Execute attempt number is invalid"
            )
        if not isinstance(execution.run_id, str) or not execution.run_id:
            return _branch(
                execution, _REPLY, "defect", FailureKind.LOCAL_DEFECT,
                "the Dex call context has no run ID, so nothing was sent",
            )
        message_id_address = rfc_message_id_address(
            execution.call, execution.run_id, execution.flow_started_at
        )
        fence_result = await self._decide_fence(execution, _REPLY)
        if isinstance(fence_result, MutationAttempt):
            return fence_result
        budget = _SendBudget(execution)
        try:
            async with asyncio.timeout_at(budget.attempt_end):
                credential = await self._provider.resolve_credentials(execution.call)
        except Exception as error:
            return await self._unavailable_credentials(execution, _REPLY, error)
        if not input.message_id.strip() or not input.text_body.strip():
            return await self._defect_before_any_send(
                execution, _REPLY, FailureKind.VALIDATION, "message ID and text body are required"
            )
        if execution.attempt > 1 or fence_result == DecisionKind.READ_BACK_REQUIRED:
            try:
                async with asyncio.timeout_at(budget.pre_send_end):
                    found, credential = await self._provider.find_sent_message(
                        execution.call, credential, execution.run_id, execution.flow_started_at,
                        message_id_address=message_id_address,
                    )
            except Exception:
                found = None
                # An inconclusive search still permits a resend below the send cap.
                if fence_result != DecisionKind.READ_BACK_THEN_RESEND_ALLOWED:
                    await self._record_not_sent(execution)
                    return _retry(
                        _REPLY, FailureKind.AVAILABILITY,
                        "Gmail sent-mail search did not answer before the reply",
                    )
            if found:
                return await self._sent(
                    execution,
                    budget,
                    credential,
                    _REPLY,
                    _OutgoingMessage(b"", [], message_id_address),
                    found,
                    _receipt(execution),
                )
            if fence_result == DecisionKind.READ_BACK_REQUIRED:
                return _retry(
                    _REPLY, FailureKind.AVAILABILITY,
                    "Gmail sent-mail search found no reply; prior dispatch cannot be repeated",
                )
        status = 0
        headers = httpx.Headers()
        try:
            async with asyncio.timeout_at(budget.pre_send_end):
                source_response, credential = await self._provider.read_message(
                    execution.call, credential, input.message_id, "metadata"
                )
            status, headers = source_response.status_code, source_response.headers
        except (GmailResponseTooLarge, GmailResponseReadFailure) as error:
            status, headers = error.status_code, error.headers
            source_response = None
        except (ProviderTransportError, TimeoutError):
            source_response = None
        if source_response is None or not 200 <= status < 300:
            if status == 0 or status == 429 or status >= 500:
                await self._record_not_sent(execution)
                return _retry_after(
                    headers,
                    _REPLY,
                    FailureKind.AVAILABILITY,
                    "Gmail source message is temporarily unavailable",
                )
            if 200 <= status < 300:
                return await self._reject_unless_earlier_send_is_unconfirmed(
                    execution,
                    _REPLY,
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Gmail returned an invalid source message response",
                    _receipt(execution, headers, input.message_id),
                )
            return await self._reject_unless_earlier_send_is_unconfirmed(
                execution,
                _REPLY,
                "providerRejected",
                _status_kind(status),
                "Gmail rejected the source message lookup",
                _receipt(execution, headers, input.message_id),
            )
        try:
            source_resource = json.loads(source_response.body)
        except (ValueError, UnicodeDecodeError):
            return await self._reject_unless_earlier_send_is_unconfirmed(
                execution, _REPLY, "invalidResponse", FailureKind.PROTOCOL,
                "Gmail returned an invalid source message response",
                _receipt(execution, headers, input.message_id),
            )
        try:
            message = decode_message(source_resource)
        except (ValueError, UnicodeDecodeError, KeyError, TypeError):
            return await self._reject_unless_earlier_send_is_unconfirmed(
                execution,
                _REPLY,
                "invalidResponse",
                FailureKind.PROTOCOL,
                "source message lacks reply metadata",
                _receipt(execution, headers, input.message_id),
            )
        if not message.thread_id or not message.rfc_message_id:
            return await self._reject_unless_earlier_send_is_unconfirmed(
                execution,
                _REPLY,
                "invalidResponse",
                FailureKind.PROTOCOL,
                "source message lacks reply metadata",
                _receipt(execution, headers, input.message_id),
            )
        try:
            recipient = _format_go_mailbox(parse_mailbox(message.reply_to or message.from_address))
        except ValueError:
            return await self._reject_unless_earlier_send_is_unconfirmed(
                execution,
                _REPLY,
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Gmail returned an invalid source sender",
                _receipt(execution, headers, input.message_id),
            )
        if "\r" in message.subject + message.rfc_message_id + message.references or "\n" in (
            message.subject + message.rfc_message_id + message.references
        ):
            return await self._reject_unless_earlier_send_is_unconfirmed(
                execution,
                _REPLY,
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Gmail returned invalid source reply headers",
                _receipt(execution, headers, input.message_id),
            )
        try:
            payload = build_reply_message(
                execution.call,
                credential.primary_email,
                recipient,
                message,
                input,
                execution.run_id,
                execution.flow_started_at,
                message_id_address=message_id_address,
            )
            if _raw_message_size(payload) > self._provider.max_message_bytes:
                raise ValueError("reply exceeds configured size limit")
        except ValueError:
            return await self._defect_before_any_send(
                execution, _REPLY, FailureKind.VALIDATION,
                "reply could not be encoded within the configured limit",
            )
        outgoing = _OutgoingMessage(
            payload, [recipient], message_id_address, message.thread_id, execution.attempt > 1
        )
        return await self._send_and_confirm(execution, budget, credential, _REPLY, outgoing, fence_result)

    async def _decide_fence(
        self, execution: SendExecution, outcome: _SendOutcome
    ) -> DecisionKind | MutationAttempt[SendMessageOutput]:
        if execution.fence is None:
            return DecisionKind.SEND_ALLOWED
        decision = await execution.fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            # An unreadable or untrusted checkpoint may hide an earlier send, so the outcome repeats.
            return _retry(
                outcome, FailureKind.LOCAL_DEFECT,
                f"{decision.error}; an earlier send may still be delivered",
            )
        # A saved provider ID proves Gmail accepted an earlier send, so it is never resent.
        if (
            decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
            and decision.marker is not None and decision.marker.provider_id
        ):
            return DecisionKind.READ_BACK_REQUIRED
        return decision.kind

    async def _unavailable_credentials(
        self, execution: SendExecution, outcome: _SendOutcome, error: Exception
    ) -> MutationAttempt[SendMessageOutput]:
        """Classify a credential failure before any send in this attempt.

        After an earlier possible send, any failure, even one that needs
        reconnection, returns Retry without notSent, so later attempts still
        read back. Only a clean first attempt selects defect.
        """
        if await self._is_earlier_send_possible(execution):
            is_permanent = isinstance(error, (ReauthorizationRequiredError, GmailCredentialMaterialError))
            reason = "need reconnection" if is_permanent else "are temporarily unavailable"
            return _retry(
                outcome, FailureKind.AUTHENTICATION,
                f"Gmail credentials {reason}; an earlier send may still be delivered",
            )
        return _branch(
            execution, outcome, "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable"
        )

    async def _defect_before_any_send(
        self, execution: SendExecution, outcome: _SendOutcome, kind: FailureKind, message: str
    ) -> MutationAttempt[SendMessageOutput]:
        """Select defect only when no earlier attempt may have sent.

        Otherwise that send may still be delivered, so Retry keeps the claim
        and later attempts read back.
        """
        if await self._is_earlier_send_possible(execution):
            return _retry(outcome, kind, f"{message}; an earlier send may still be delivered")
        return _branch(execution, outcome, "defect", kind, message)

    async def _is_earlier_send_possible(self, execution: SendExecution) -> bool:
        return execution.fence is not None and (await execution.fence.decide()).prior_send_upper_bound > 0

    async def _reject_unless_earlier_send_is_unconfirmed(
        self,
        execution: SendExecution,
        outcome: _SendOutcome,
        branch: str,
        kind: FailureKind,
        message: str,
        receipt: Receipt,
    ) -> MutationAttempt[SendMessageOutput]:
        """Select a definite rejection only when no earlier attempt may have sent.

        Otherwise that send may still be delivered, so Retry keeps the claim and
        later attempts read back until a match or the retry budget ends.
        """
        if execution.fence is not None and (
            await execution.fence.decide()
        ).prior_send_upper_bound > 0:
            return _retry(outcome, kind, f"{message}; an earlier send may still be delivered")
        return _branch(execution, outcome, branch, kind, message, receipt)

    async def _reject_refused_send(
        self,
        execution: SendExecution,
        outcome: _SendOutcome,
        is_definite_refusal: bool,
        kind: FailureKind,
        message: str,
        receipt: Receipt,
    ) -> MutationAttempt[SendMessageOutput]:
        """Classify a definite Gmail refusal of this attempt's POST.

        With no earlier possible send, canResend proves nothing was sent, so
        agent hosts report failure instead of an unknown outcome. Only a
        status in ``_REFUSED_BEFORE_PROCESSING_STATUSES`` with a readable body
        naming no message proves that. Any other refusal, such as 408 or 409,
        leaves the outcome unknown: Retry keeps the claim, so the next attempt
        reads back Sent Mail before any resend.
        """
        if not is_definite_refusal:
            return _retry(outcome, kind, f"{message}; Gmail may still have processed the request")
        if execution.fence is not None and (
            await execution.fence.decide()
        ).prior_send_upper_bound == 0:
            try:
                await execution.fence.record_can_resend()
            except Exception:  # noqa: BLE001
                pass  # A failed checkpoint leaves the claim, so the outcome stays unknown.
        return await self._reject_unless_earlier_send_is_unconfirmed(
            execution, outcome, "providerRejected", kind, message, receipt
        )

    async def _record_not_sent(self, execution: SendExecution) -> None:
        if execution.fence is not None and (
            await execution.fence.decide()
        ).kind == DecisionKind.SEND_ALLOWED:
            await execution.fence.record_not_sent()

    async def _read_back_only(
        self, execution: SendExecution, budget: _SendBudget, credential: Credentials,
        outcome: _SendOutcome, message: _OutgoingMessage,
    ) -> MutationAttempt[SendMessageOutput]:
        try:
            async with asyncio.timeout_at(budget.pre_send_end):
                found, credential = await self._provider.find_sent_message(
                    execution.call, credential, execution.run_id, execution.flow_started_at,
                    message_id_address=message.message_id_address,
                )
        except Exception:
            return _retry(outcome, FailureKind.AVAILABILITY, "Gmail sent-mail search did not answer")
        if found:
            return await self._sent(
                execution, budget, credential, outcome, message, found, _receipt(execution)
            )
        return _retry(
            outcome, FailureKind.AVAILABILITY,
            "Gmail sent-mail search found no message; prior dispatch cannot be repeated",
        )

    async def _post_message(
        self, call: Call, credential: Credentials, payload: bytes, write_deadline: datetime
    ) -> tuple[GmailHTTPResult, Credentials]:
        async with asyncio.timeout_at(asyncio.get_running_loop().time() + max(
            0.0, (write_deadline - datetime.now(UTC)).total_seconds()
        )):
            return await self._provider.request(
                call, credential, "POST", "/users/me/messages/send", payload=payload
            )

    async def _send_and_confirm(
        self,
        execution: SendExecution,
        budget: _SendBudget,
        credential: Credentials,
        outcome: _SendOutcome,
        message: _OutgoingMessage,
        decision_kind: DecisionKind,
    ) -> MutationAttempt[SendMessageOutput]:
        if execution.attempt > 1 and not message.has_pre_send_search:
            try:
                async with asyncio.timeout_at(budget.pre_send_end):
                    found, credential = await self._provider.find_sent_message(
                        execution.call, credential, execution.run_id, execution.flow_started_at,
                        message_id_address=message.message_id_address,
                    )
            except Exception:
                found = None
                # An inconclusive search still permits a resend below the send cap.
                if decision_kind != DecisionKind.READ_BACK_THEN_RESEND_ALLOWED:
                    await self._record_not_sent(execution)
                    return _retry(
                        outcome,
                        FailureKind.AVAILABILITY,
                        f"Gmail sent-mail search did not answer before the {outcome.request_name}",
                    )
            if found:
                return await self._sent(
                    execution, budget, credential, outcome, message, found, _receipt(execution)
                )
        if not budget.can_send:
            await self._record_not_sent(execution)
            return _retry(
                outcome,
                FailureKind.AVAILABILITY,
                "too little of the Execute attempt is left to send safely, so nothing was sent",
            )
        response: GmailHTTPResult | None = None
        status = 0
        headers = httpx.Headers()
        try:
            if execution.fence is None:
                async with asyncio.timeout_at(budget.send_end):
                    response, credential = await self._provider.request(
                        execution.call, credential, "POST", "/users/me/messages/send", payload=message.payload
                    )
            else:
                response, credential = await execution.fence.dispatch(
                    partial(self._post_message, execution.call, credential, message.payload)
                )
            assert response is not None
            status, headers = response.status_code, response.headers
        except (CheckpointError, InsufficientAttemptTimeError):
            return _retry(outcome, FailureKind.AVAILABILITY, "Gmail send checkpoint could not be recorded")
        except (GmailResponseTooLarge, GmailResponseReadFailure) as error:
            status, headers = error.status_code, error.headers
        except ProviderTransportError as error:
            if error.provably_not_sent:
                if execution.fence is not None:
                    await execution.fence.record_can_resend()
                return _retry(
                    outcome, FailureKind.TRANSPORT, f"Gmail {outcome.request_name} request was not dispatched"
                )
            return await self._confirm_possible(
                execution,
                budget,
                credential,
                outcome,
                message,
                FailureKind.TRANSPORT,
                f"Gmail {outcome.request_name} outcome is unknown",
                _receipt(execution),
            )
        except TimeoutError:
            return await self._confirm_possible(
                execution,
                budget,
                credential,
                outcome,
                message,
                FailureKind.TRANSPORT,
                f"Gmail {outcome.request_name} outcome is unknown",
                _receipt(execution),
            )
        receipt = _receipt(execution, headers)
        if status == 429:
            if execution.fence is not None:
                await execution.fence.record_can_resend()
            return _retry_after(
                headers,
                outcome,
                FailureKind.RATE_LIMIT,
                f"Gmail temporarily rejected the {outcome.request_name}",
            )
        if status == 503:
            # Gmail documents 5xx as processing failures and Sent Mail search can lag, so the claim stays.
            found, _ = await self._poll_sent(execution, budget, credential, message)
            if found:
                if execution.fence is not None and found.get("id"):
                    await execution.fence.record_provider_id(found["id"])
                return await self._sent(execution, budget, credential, outcome, message, found, receipt)
            return _retry_after(
                headers,
                outcome,
                FailureKind.AVAILABILITY,
                f"Gmail {outcome.request_name} is temporarily unavailable",
            )
        if status >= 500:
            return await self._confirm_possible(
                execution,
                budget,
                credential,
                outcome,
                message,
                FailureKind.AVAILABILITY,
                f"Gmail {outcome.request_name} outcome is unknown",
                receipt,
            )
        if not 200 <= status < 300:
            if execution.attempt > 1:
                try:
                    async with asyncio.timeout_at(budget.attempt_end):
                        found, credential = await self._provider.find_sent_message(
                            execution.call, credential, execution.run_id, execution.flow_started_at,
                            message_id_address=message.message_id_address,
                        )
                except (ValueError, OSError, TimeoutError):
                    found = None
                if found:
                    return await self._sent(execution, budget, credential, outcome, message, found, receipt)
            is_definite_refusal = (
                status in _REFUSED_BEFORE_PROCESSING_STATUSES
                and response is not None
                and not _has_accepted_message_id(response.body)
            )
            return await self._reject_refused_send(
                execution,
                outcome,
                is_definite_refusal,
                _status_kind(status),
                outcome.rejection_message,
                receipt,
            )
        try:
            sent = json.loads(response.body) if response is not None else {}
            if (
                not isinstance(sent, dict)
                or not isinstance(sent.get("id"), str)
                or not sent["id"]
                or not isinstance(sent.get("threadId", ""), str)
            ):
                raise ValueError("Gmail accepted response has no message ID")
        except (ValueError, UnicodeDecodeError):
            found, _ = await self._poll_sent(execution, budget, credential, message)
            sent = found or {"threadId": message.requested_thread_id}
        if execution.fence is not None and sent.get("id"):
            await execution.fence.record_provider_id(sent["id"])
        return await self._sent(execution, budget, credential, outcome, message, sent, receipt)

    async def _confirm_possible(
        self,
        execution: SendExecution,
        budget: _SendBudget,
        credential: Credentials,
        outcome: _SendOutcome,
        message: _OutgoingMessage,
        kind: FailureKind,
        failure_message: str,
        receipt: Receipt,
    ) -> MutationAttempt[SendMessageOutput]:
        found, has_lookup_error = await self._poll_sent(execution, budget, credential, message)
        if found:
            if execution.fence is not None and found.get("id"):
                await execution.fence.record_provider_id(found["id"])
            return await self._sent(execution, budget, credential, outcome, message, found, receipt)
        suffix = (
            "; the sent-mail search did not answer"
            if has_lookup_error
            else "; the sent-mail search found no message"
        )
        return _retry(outcome, kind, failure_message + suffix)

    async def _poll_sent(
        self,
        execution: SendExecution,
        budget: _SendBudget,
        credential: Credentials,
        message: _OutgoingMessage,
    ) -> tuple[dict[str, str] | None, bool]:
        has_lookup_error = False
        for lookup in range(4):
            try:
                async with asyncio.timeout_at(budget.attempt_end):
                    if lookup:
                        await asyncio.sleep(0.075 * (2 ** (lookup - 1)))
                    found, credential = await self._provider.find_sent_message(
                        execution.call, credential, execution.run_id, execution.flow_started_at,
                        message_id_address=message.message_id_address,
                    )
            except TimeoutError:
                return None, True
            except Exception:
                has_lookup_error = True
                continue
            has_lookup_error = False
            if found:
                return found, False
        return None, has_lookup_error

    async def _sent(
        self,
        execution: SendExecution,
        budget: _SendBudget,
        credential: Credentials,
        outcome: _SendOutcome,
        message: _OutgoingMessage,
        sent: dict[str, str],
        receipt: Receipt,
    ) -> MutationAttempt[SendMessageOutput]:
        message_id = sent.get("id", "")
        thread_id = sent.get("threadId", "")
        if not thread_id and message_id:
            try:
                async with asyncio.timeout_at(budget.attempt_end):
                    response, _ = await self._provider.read_message(
                        execution.call, credential, message_id, "minimal"
                    )
                resource = json.loads(response.body)
                thread_id = resource.get("threadId", "") if isinstance(resource, dict) else ""
            except Exception:
                pass
            if not thread_id:
                thread_id = message.requested_thread_id
        result = SendMessageOutput(credential.primary_email, message.recipients, message_id, thread_id)
        return MutationAttempt.branch_result("sent", result, replace(receipt, provider_object_id=message_id))


__all__ = ["SendExecution", "SendService"]
