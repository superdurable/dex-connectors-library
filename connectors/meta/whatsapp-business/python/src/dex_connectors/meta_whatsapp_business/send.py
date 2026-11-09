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
from .models import SendTemplateMessageInput, SendTextMessageInput, SentMessage

if TYPE_CHECKING:
    from .client import Client

_NUMERIC_ID = re.compile(r"[0-9]{1,40}\Z")
_RECIPIENT = re.compile(r"[1-9][0-9]{5,14}\Z")
_TEMPLATE_NAME = re.compile(r"[a-z0-9_]{1,512}\Z")
_LANGUAGE_CODE = re.compile(r"[a-z]{2,3}(?:_[A-Z]{2})?\Z")


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


@dataclass(slots=True)
class _MessageDispatch:
    client: httpx.AsyncClient
    request: httpx.Request

    async def send(self, write_deadline: datetime) -> httpx.Response:
        """Send once without a transport-level replay."""
        return (await send_traced(self.client, self.request))[0]


async def _send_message(
    client: "Client",
    call: Call,
    operation: str,
    phone_number_id: str,
    payload: dict[str, Any],
    definition: MutationDefinition,
) -> MutationAttempt[SentMessage]:
    try:
        fence = Fence(call, definition.step_defaults.execute_method_timeout)
        if (await fence.decide()).kind != DecisionKind.SEND_ALLOWED:
            return _uncertain(
                operation, FailureKind.TRANSPORT, "Meta send outcome cannot be confirmed; do not resend"
            )
    except Exception:
        return _branch(operation, "defect", FailureKind.LOCAL_DEFECT, "Meta dispatch fence is unavailable")
    try:
        credentials = await client.credentials.resolve(call)
        token = credentials.access_token.reveal()
        if not is_header_safe_credential(token):
            raise ValueError("invalid system-user token")
    except (ValueError, LookupError, OSError, AttributeError):
        return _branch(
            operation,
            "defect",
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable or invalid",
        )
    try:
        body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
        if len(body) > 65536:
            return _branch(
                operation, "defect", FailureKind.VALIDATION, "message request is invalid or too large"
            )
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
    except (ValueError, httpx.HTTPError, UnicodeEncodeError):
        return _branch(operation, "defect", FailureKind.LOCAL_DEFECT, "message request could not be built")
    try:
        response = await fence.dispatch(_MessageDispatch(client.http_client, request).send)
    except (CheckpointError, InsufficientAttemptTimeError):
        return _retry(operation, FailureKind.TRANSPORT, "Meta dispatch was not started")
    except ProviderTransportError as error:
        if error.provably_not_sent:
            try:
                await fence.record_can_resend()
                return _retry(operation, FailureKind.TRANSPORT, "Meta could not be reached before sending")
            except Exception:
                pass
        return _uncertain(operation, FailureKind.TRANSPORT, "Meta message outcome is unknown")
    except (DispatchNotAllowedError, httpx.HTTPError, TimeoutError):
        return _uncertain(operation, FailureKind.TRANSPORT, "Meta message outcome is unknown")
    try:
        status = response.status_code
        if status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return _retry(operation, FailureKind.AVAILABILITY, "Meta retry checkpoint is unavailable")
            kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            return _retry(operation, kind, "Meta did not process the send", delay)
        if status >= 500 or status in (408, 409):
            return _uncertain(operation, FailureKind.AVAILABILITY, "Meta message outcome is unknown")
        if 400 <= status < 500:
            kind = FailureKind.PROVIDER_REJECTION
            if status == 401:
                kind = FailureKind.AUTHENTICATION
            if status == 403:
                kind = FailureKind.AUTHORIZATION
            return _branch(operation, "providerRejected", kind, f"Meta rejected HTTP {status}")
        try:
            body = await read_bounded_body(response, client.max_response_bytes)
        except (ProviderTransportError, Exception):
            return _uncertain(operation, FailureKind.TRANSPORT, "Meta send response could not be confirmed")
        try:
            document = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            document = None
        messages = document.get("messages") if isinstance(document, dict) else None
        message_id = (
            messages[0].get("id")
            if isinstance(messages, list) and len(messages) == 1 and isinstance(messages[0], dict)
            else None
        )
        if (
            status != 200
            or not isinstance(document, dict)
            or document.get("messaging_product") != "whatsapp"
            or not isinstance(message_id, str)
            or not message_id.startswith("wamid.")
        ):
            return _uncertain(operation, FailureKind.PROTOCOL, "Meta send response is incomplete")
        try:
            await fence.record_provider_id(message_id)
        except Exception:
            return _uncertain(
                operation, FailureKind.LOCAL_DEFECT, "Meta message ID could not be checkpointed"
            )
        return MutationAttempt.branch_result("sent", SentMessage(message_id), Receipt())
    finally:
        await response.aclose()


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
        """Send once and report ambiguous outcomes as uncertain."""
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
        """Send once and report ambiguous outcomes as uncertain."""
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
