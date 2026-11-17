# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Single-dispatch WhatsApp text and template messages."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

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

from ._generated.definitions import SEND_TEMPLATE_MESSAGE_DEFINITION, SEND_TEXT_MESSAGE_DEFINITION
from ._graph_error import decode_graph_error
from .models import SendTemplateMessageInput, SendTextMessageInput, SentMessage

if TYPE_CHECKING:
    from .client import Client

_NUMERIC_ID = re.compile(r"[0-9]{1,40}\Z")
_RECIPIENT = re.compile(r"[1-9][0-9]{5,14}\Z")
_TEMPLATE_NAME = re.compile(r"[a-z0-9_]{1,512}\Z")
_LANGUAGE_CODE = re.compile(r"[a-z]{2,3}(?:_[A-Z]{2})?\Z")
# Only these statuses prove Meta refused a send; other 4xx statuses leave the outcome unknown.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "whatsapp-business", operation, message)


def _branch(operation: str, branch: str, kind: FailureKind, message: str) -> MutationAttempt[SentMessage]:
    return MutationAttempt.branch_result(branch, SentMessage(), Receipt(), _failure(operation, kind, message))


def _retry(operation: str, kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[SentMessage]:
    return MutationAttempt.retry(_failure(operation, kind, message), delay)


def _uncertain(operation: str, kind: FailureKind, message: str) -> MutationAttempt[SentMessage]:
    return MutationAttempt.uncertain(SentMessage(), _failure(operation, kind, message), Receipt())


def _valid_address(phone_number_id: object, recipient: object) -> bool:
    return (
        isinstance(phone_number_id, str)
        and _NUMERIC_ID.fullmatch(phone_number_id) is not None
        and isinstance(recipient, str)
        and _RECIPIENT.fullmatch(recipient) is not None
    )


def _is_message_id(value: object) -> bool:
    return isinstance(value, str) and value.startswith("wamid.") and len(value) > len("wamid.")


def _read_sent_message_id(body: bytes) -> tuple[str, bool]:
    """Return the first wamid in a send response and whether the response is complete."""
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return "", False
    if not isinstance(document, dict):
        return "", False
    messages = document.get("messages")
    if not isinstance(messages, list) or not messages or not isinstance(messages[0], dict):
        return "", False
    message_id = messages[0].get("id")
    if not isinstance(message_id, str) or not _is_message_id(message_id):
        return "", False
    return message_id, document.get("messaging_product") == "whatsapp" and len(messages) == 1


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


@dataclass(slots=True)
class _MessageDispatch:
    client: httpx.AsyncClient
    request: httpx.Request

    async def send(self, write_deadline: datetime) -> httpx.Response:
        """Send once without a transport-level replay."""
        return (await send_traced(self.client, self.request))[0]


@dataclass(frozen=True, slots=True)
class _MessageSend:
    """One attempt's send-cap decisions for classifying its outcome."""

    operation: str
    fence: Fence
    can_resend_unknown_outcome: bool
    has_possible_earlier_send: bool

    def resend_or_select_uncertain(
        self, kind: FailureKind, message: str, delay: float = 0
    ) -> MutationAttempt[SentMessage]:
        """Retry a possibly sent message while the send cap allows another send."""
        if self.can_resend_unknown_outcome:
            # The claim stays the last marker, so the next attempt counts this send.
            return _retry(self.operation, kind, message, delay)
        return _uncertain(self.operation, kind, message)

    async def reject_or_select_uncertain(
        self, kind: FailureKind, message: str
    ) -> MutationAttempt[SentMessage]:
        """Select providerRejected only when no earlier send may have delivered."""
        if self.has_possible_earlier_send:
            return _uncertain(self.operation, kind, message)
        try:
            await self.fence.record_can_resend()
        except Exception:
            pass  # The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
        return _branch(self.operation, "providerRejected", kind, message)

    async def local_failure(self, kind: FailureKind, message: str) -> MutationAttempt[SentMessage]:
        """Select defect only when no earlier send may have delivered."""
        if self.has_possible_earlier_send:
            # notSent would hide the earlier possible send, so nothing is recorded.
            return _retry(self.operation, kind, message + "; no message was sent in this attempt")
        try:
            await self.fence.record_not_sent()
        except Exception:
            pass  # The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
        return _branch(self.operation, "defect", kind, message)


async def _send_message(
    client: "Client",
    call: Call,
    operation: str,
    phone_number_id: str,
    payload: dict[str, Any],
    definition: MutationDefinition,
) -> MutationAttempt[SentMessage]:
    """Dispatch one Cloud API message, resending an unknown outcome below the send cap.

    Meta has no send idempotency key or read-back, so an unknown outcome retries while
    possible sends stay below `call.max_sends` and selects uncertain at the cap. A
    checkpointed wamid completes without resending, and a definite refusal is never resent.
    """
    try:
        body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
    except (ValueError, UnicodeEncodeError):
        body = b""
    if not body or len(body) > 65536:
        return _branch(operation, "defect", FailureKind.VALIDATION, "message request is invalid or too large")
    try:
        fence = Fence(call, definition.step_defaults.execute_method_timeout, max_sends=call.max_sends)
        decision = await fence.decide()
    except Exception:
        return _branch(operation, "defect", FailureKind.LOCAL_DEFECT, "Meta dispatch fence is unavailable")
    if decision.kind == DecisionKind.FAIL_CLOSED:
        return _retry(
            operation,
            FailureKind.AVAILABILITY,
            "Meta dispatch checkpoint is unreadable; no message was sent in this attempt",
        )
    if decision.kind in (DecisionKind.READ_BACK_REQUIRED, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED):
        saved_message_id = decision.marker.provider_id if decision.marker is not None else ""
        # Meta returned this wamid only for a message it accepted, so the message is never resent.
        if _is_message_id(saved_message_id):
            return MutationAttempt.branch_result("sent", SentMessage(saved_message_id), Receipt())
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return _uncertain(
                operation,
                FailureKind.TRANSPORT,
                "an earlier attempt may have sent the WhatsApp message, and the send cap is reached",
            )
    attempt = _MessageSend(
        operation,
        fence,
        # This attempt's send would be the last one the cap allows when no further send remains.
        can_resend_unknown_outcome=decision.prior_send_upper_bound + 1 < call.max_sends,
        # An earlier send may have delivered, so a refusal of a resend proves nothing.
        has_possible_earlier_send=decision.prior_send_upper_bound > 0,
    )
    try:
        credentials = await client.credentials.resolve(call)
        token = credentials.access_token.reveal()
        if not is_header_safe_credential(token):
            raise ValueError("invalid system-user token")
    except (ValueError, LookupError, OSError, AttributeError):
        return await attempt.local_failure(
            FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    try:
        request = client.http_client.build_request(
            "POST",
            client.base_url + "/" + phone_number_id + "/messages",
            content=body,
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Authorization": "Bearer " + token,
            },
        )
    except (ValueError, httpx.HTTPError):
        return await attempt.local_failure(FailureKind.LOCAL_DEFECT, "message request could not be built")
    try:
        response = await fence.dispatch(_MessageDispatch(client.http_client, request).send)
    except (CheckpointError, InsufficientAttemptTimeError):
        return _retry(operation, FailureKind.TRANSPORT, "Meta dispatch was not started")
    except ProviderTransportError as error:
        if error.provably_not_sent:
            try:
                await fence.record_can_resend()
            except Exception:
                pass  # A lost canResend checkpoint leaves the claim, which the next attempt counts.
            return _retry(operation, FailureKind.TRANSPORT, "Meta could not be reached before sending")
        return attempt.resend_or_select_uncertain(FailureKind.TRANSPORT, "Meta message outcome is unknown")
    except (DispatchNotAllowedError, httpx.HTTPError, TimeoutError):
        return attempt.resend_or_select_uncertain(FailureKind.TRANSPORT, "Meta message outcome is unknown")
    try:
        return await _classify_response(attempt, response, client.max_response_bytes)
    finally:
        await response.aclose()


async def _classify_response(
    attempt: _MessageSend, response: httpx.Response, max_response_bytes: int
) -> MutationAttempt[SentMessage]:
    operation = attempt.operation
    status = response.status_code
    retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
    try:
        body: bytes | None = await read_bounded_body(response, max_response_bytes)
    except Exception:
        body = None
    graph_error = decode_graph_error(body) if body is not None and 400 <= status < 500 else None
    # A Graph or Cloud API throttling code means Meta refused the send before processing it.
    is_rate_limited = status == 429 or (graph_error is not None and graph_error.is_throttled)
    if is_rate_limited or status == 503:
        try:
            await attempt.fence.record_can_resend()
        except Exception:
            return _retry(operation, FailureKind.AVAILABILITY, "Meta retry checkpoint is unavailable")
        kind = FailureKind.RATE_LIMIT if is_rate_limited else FailureKind.AVAILABILITY
        return _retry(operation, kind, "Meta did not process the send", retry_after)
    message_id, is_complete = _read_sent_message_id(body) if body is not None else ("", False)
    if status in _DEFINITE_REFUSAL_STATUSES:
        if message_id:
            # The body reports an accepted message, so a resend could duplicate it.
            return _uncertain(operation, FailureKind.PROTOCOL, "Meta returned a contradictory send response")
        return await attempt.reject_or_select_uncertain(
            _rejection_kind(status), f"Meta rejected HTTP {status}"
        )
    if not 200 <= status < 300:
        return attempt.resend_or_select_uncertain(
            FailureKind.AVAILABILITY, f"Meta message outcome is unknown after HTTP {status}", retry_after
        )
    if body is None:
        return attempt.resend_or_select_uncertain(
            FailureKind.TRANSPORT, "Meta send response could not be read"
        )
    if not message_id:
        return attempt.resend_or_select_uncertain(FailureKind.PROTOCOL, "Meta send response is malformed")
    if not is_complete:
        # Meta returned a wamid, so a resend could duplicate the message.
        return _uncertain(operation, FailureKind.PROTOCOL, "Meta send response is incomplete")
    try:
        await attempt.fence.record_provider_id(message_id)
    except Exception:
        pass  # A lost provider-ID checkpoint leaves the claim, which still counts this send.
    return MutationAttempt.branch_result("sent", SentMessage(message_id), Receipt())


class SendTextMessageOperation:
    """Send one plain-text message within the customer service window."""

    def __init__(self, client: "Client") -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated text send contract."""
        return SEND_TEXT_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendTextMessageInput) -> IdempotencyKey:
        """Record the Call ID; Meta does not deduplicate with this key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendTextMessageInput) -> MutationAttempt[SentMessage]:
        """Send under a fence; an unknown outcome resends below the send cap."""
        if (
            not isinstance(input, SendTextMessageInput)
            or not _valid_address(input.phone_number_id, input.recipient)
            or not isinstance(input.text, str)
            or not input.text
        ):
            return _branch(
                "sendTextMessage", "defect", FailureKind.VALIDATION, "sender, recipient, or text is invalid"
            )
        try:
            length = len(input.text.encode("utf-8", errors="strict"))
        except UnicodeEncodeError:
            length = 4097
        if length > 4096:
            return _branch(
                "sendTextMessage", "defect", FailureKind.VALIDATION, "sender, recipient, or text is invalid"
            )
        payload = {
            "messaging_product": "whatsapp",
            "recipient_type": "individual",
            "to": input.recipient,
            "type": "text",
            "text": {"preview_url": False, "body": input.text},
        }
        return await _send_message(
            self.client, call, "sendTextMessage", input.phone_number_id, payload, SEND_TEXT_MESSAGE_DEFINITION
        )


class SendTemplateMessageOperation:
    """Send one approved template with optional text body parameters."""

    def __init__(self, client: "Client") -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated template send contract."""
        return SEND_TEMPLATE_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendTemplateMessageInput) -> IdempotencyKey:
        """Record the Call ID; Meta does not deduplicate with this key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendTemplateMessageInput) -> MutationAttempt[SentMessage]:
        """Send under a fence; an unknown outcome resends below the send cap."""
        if (
            not isinstance(input, SendTemplateMessageInput)
            or not _valid_address(input.phone_number_id, input.recipient)
            or not isinstance(input.template_name, str)
            or _TEMPLATE_NAME.fullmatch(input.template_name) is None
            or not isinstance(input.language_code, str)
            or _LANGUAGE_CODE.fullmatch(input.language_code) is None
            or not isinstance(input.body_parameters, (tuple, list))
            or len(input.body_parameters) > 50
        ):
            return _branch(
                "sendTemplateMessage",
                "defect",
                FailureKind.VALIDATION,
                "sender, recipient, or template is invalid",
            )
        parameters = []
        for value in input.body_parameters:
            if not isinstance(value, str) or not value:
                return _branch(
                    "sendTemplateMessage",
                    "defect",
                    FailureKind.VALIDATION,
                    "template body parameter is invalid",
                )
            try:
                size = len(value.encode("utf-8", errors="strict"))
            except UnicodeEncodeError:
                size = 1025
            if size > 1024:
                return _branch(
                    "sendTemplateMessage",
                    "defect",
                    FailureKind.VALIDATION,
                    "template body parameter is invalid",
                )
            parameters.append({"type": "text", "text": value})
        template: dict[str, Any] = {"name": input.template_name, "language": {"code": input.language_code}}
        if parameters:
            template["components"] = [{"type": "body", "parameters": parameters}]
        payload = {
            "messaging_product": "whatsapp",
            "recipient_type": "individual",
            "to": input.recipient,
            "type": "template",
            "template": template,
        }
        return await _send_message(
            self.client,
            call,
            "sendTemplateMessage",
            input.phone_number_id,
            payload,
            SEND_TEMPLATE_MESSAGE_DEFINITION,
        )
