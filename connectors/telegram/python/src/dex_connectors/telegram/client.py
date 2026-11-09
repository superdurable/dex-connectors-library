# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Telegram Bot API client with safe retries and single-dispatch message sends."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
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
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FOUND as FOUND
from ._generated.definitions import GET_CHAT_DEFINITION as GET_CHAT_DEFINITION
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import NOT_FOUND as NOT_FOUND
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from ._generated.definitions import SEND_MESSAGE_DEFINITION as SEND_MESSAGE_DEFINITION
from ._generated.definitions import SENT as SENT
from ._generated.definitions import UNCERTAIN as UNCERTAIN
from .models import Chat, GetChatInput, SendMessageInput, SentMessage

_BOT_TOKEN = re.compile(r"^[0-9]+:[A-Za-z0-9_-]+$")
_CHAT_USERNAME = re.compile(r"^@[A-Za-z0-9_]{5,32}$")
_CHAT_TYPES = frozenset({"private", "group", "supergroup", "channel"})


class Config(_GeneratedConfig):
    """Telegram Bot API URL, request timeout, and complete response bound."""

    __slots__ = ()


class Credentials(_GeneratedCredentials):
    """Bot token resolved from the host for each provider attempt."""

    __slots__ = ()


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider response or safe transport failure without token-bearing URLs."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""
    request_sent: bool = False


class Client:
    """Resolve Bot credentials per call and use one redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Telegram configuration and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Telegram timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url or "https://api.telegram.org")
        self.max_response_bytes = config.max_response_bytes or 1_048_576
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds or 15)

    def send_message(self) -> "SendMessageOperation":
        """Bind the single-dispatch message Mutation."""
        return SendMessageOperation(self)

    def get_chat(self) -> "GetChatOperation":
        """Bind the read-only chat metadata Query."""
        return GetChatOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()

    async def resolve_bot_token(self, call: Call) -> str:
        """Read the token without exposing it through Flow state or failures."""
        try:
            credentials = await self.credentials.resolve(call)
            token = credentials.bot_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return ""
        return token if _BOT_TOKEN.fullmatch(token) else ""

    async def request(self, request: httpx.Request) -> Exchange:
        """Send one request and classify read interruption separately from no send."""
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
            if retry_after <= 0:
                retry_after = _body_retry_after(body)
            return Exchange(response.status_code, body, retry_after, read_error, True)
        finally:
            await response.aclose()


class SendMessageOperation:
    """Send text once under a Dex heartbeat fence with no provider deduplication key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Telegram client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-generated send contract and sync Execute defaults."""
        return SEND_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendMessageInput) -> IdempotencyKey:
        """Keep stable call identity; Telegram does not receive this key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: SendMessageInput) -> MutationAttempt[SentMessage]:
        """Send once, retry proven unprocessed requests, and fail closed on ambiguity."""
        try:
            fence = Fence(call, SEND_MESSAGE_DEFINITION.step_defaults.execute_method_timeout)
            decision = await fence.decide()
        except ValueError:
            return _send_failure(DEFECT, FailureKind.LOCAL_DEFECT, "Telegram dispatch fence is unavailable")
        if decision.kind != DecisionKind.SEND_ALLOWED:
            return _send_uncertain(
                FailureKind.TRANSPORT, "Telegram send outcome cannot be confirmed; do not resend"
            )
        if (
            not isinstance(input, SendMessageInput)
            or not _valid_chat_id(input.chat_id)
            or not isinstance(input.text, str)
            or not input.text
            or len(input.text) > 4096
        ):
            return _send_failure(DEFECT, FailureKind.VALIDATION, "chatId or text is invalid")
        token = await self.client.resolve_bot_token(call)
        if not token:
            return _send_failure(DEFECT, FailureKind.AUTHENTICATION, "bot token is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/bot" + token + "/sendMessage",
                headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
                content=urlencode({"chat_id": input.chat_id, "text": input.text}).encode(),
            )
        except (httpx.HTTPError, ValueError):
            return _send_failure(DEFECT, FailureKind.LOCAL_DEFECT, "Telegram send request could not be built")

        async def send(_deadline: datetime) -> Exchange:
            return await self.client.request(request)

        try:
            exchange = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return _send_retry(FailureKind.TRANSPORT, "Telegram dispatch was not started")
        except Exception:
            return _send_uncertain(FailureKind.TRANSPORT, "Telegram message outcome is unknown")
        if exchange.error == "request":
            if not exchange.request_sent:
                try:
                    await fence.record_can_resend()
                    return _send_retry(FailureKind.TRANSPORT, "Telegram could not be reached before sending")
                except Exception:
                    pass
            return _send_uncertain(FailureKind.TRANSPORT, "Telegram message outcome is unknown")
        if exchange.status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return _send_retry(FailureKind.AVAILABILITY, "Telegram retry checkpoint is unavailable")
            kind = FailureKind.RATE_LIMIT if exchange.status == 429 else FailureKind.AVAILABILITY
            return _send_retry(kind, "Telegram did not process the send", exchange.retry_after)
        if exchange.status >= 500 or exchange.status in (408, 409):
            return _send_uncertain(FailureKind.AVAILABILITY, "Telegram message outcome is unknown")
        if exchange.error:
            return _send_uncertain(FailureKind.TRANSPORT, "Telegram send response could not be confirmed")
        try:
            envelope = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            return _send_uncertain(FailureKind.PROTOCOL, "Telegram send response is malformed")
        if 400 <= exchange.status < 500 and isinstance(envelope, dict) and not envelope.get("ok"):
            return _send_failure(
                PROVIDER_REJECTED,
                _rejection_kind(exchange.status),
                f"Telegram rejected the message with HTTP {exchange.status}",
            )
        result = envelope.get("result") if isinstance(envelope, dict) else None
        chat = result.get("chat") if isinstance(result, dict) else None
        if (
            exchange.status != 200
            or not isinstance(envelope, dict)
            or envelope.get("ok") is not True
            or not isinstance(result, dict)
            or type(result.get("message_id")) is not int
            or result["message_id"] <= 0
            or type(result.get("date")) is not int
            or result["date"] <= 0
            or not isinstance(chat, dict)
            or type(chat.get("id")) is not int
            or chat["id"] == 0
        ):
            return _send_uncertain(FailureKind.PROTOCOL, "Telegram send response is incomplete")
        try:
            await fence.record_provider_id(str(result["message_id"]))
        except Exception:
            pass  # The claimed dispatch marker still forbids another send.
        return MutationAttempt.branch_result(
            SENT, SentMessage(chat["id"], result["message_id"], result["date"]), Receipt()
        )


class GetChatOperation:
    """Read basic metadata for one Telegram chat accessible to the bot."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Telegram client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated chat lookup contract."""
        return GET_CHAT_DEFINITION

    async def invoke(self, call: Call, input: GetChatInput) -> QueryAttempt[Chat]:
        """Read and classify one Bot API chat lookup response."""
        if not isinstance(input, GetChatInput) or not _valid_chat_id(input.chat_id):
            return _chat_failure(
                DEFECT, FailureKind.VALIDATION, "chatId must be a decimal ID or public @username"
            )
        token = await self.client.resolve_bot_token(call)
        if not token:
            return _chat_failure(DEFECT, FailureKind.AUTHENTICATION, "bot token is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/bot" + token + "/getChat?" + urlencode({"chat_id": input.chat_id}),
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _chat_failure(DEFECT, FailureKind.LOCAL_DEFECT, "Telegram request could not be built")
        exchange = await self.client.request(request)
        if exchange.error == "request":
            return _chat_retry(FailureKind.AVAILABILITY, "Telegram request failed")
        if exchange.status == 429:
            return _chat_retry(
                FailureKind.RATE_LIMIT, "Telegram rate limited the request", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _chat_retry(
                FailureKind.AVAILABILITY, "Telegram is temporarily unavailable", exchange.retry_after
            )
        if exchange.error == "too_large":
            return _chat_failure(
                INVALID_RESPONSE,
                FailureKind.RESPONSE_TOO_LARGE,
                "Telegram response exceeded the configured limit",
            )
        if exchange.error:
            return _chat_retry(FailureKind.AVAILABILITY, "Telegram response was interrupted")
        try:
            envelope = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            return _chat_failure(INVALID_RESPONSE, FailureKind.PROTOCOL, "Telegram returned invalid JSON")
        if not isinstance(envelope, dict):
            return _chat_failure(INVALID_RESPONSE, FailureKind.PROTOCOL, "Telegram returned invalid JSON")
        is_ok = envelope.get("ok") is True
        error_code = envelope.get("error_code") if type(envelope.get("error_code")) is int else 0
        if exchange.status == 200 and not is_ok and error_code == 0:
            return _chat_failure(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Telegram returned an incomplete response"
            )
        if not is_ok:
            status = error_code or exchange.status
            if status == 429:
                after = envelope.get("parameters")
                seconds = after.get("retry_after") if isinstance(after, dict) else 0
                return _chat_retry(
                    FailureKind.RATE_LIMIT,
                    "Telegram rate limited the request",
                    max(seconds, 0) if type(seconds) is int else 0,
                )
            if status >= 500:
                return _chat_retry(FailureKind.AVAILABILITY, "Telegram is temporarily unavailable")
            if status == 400 and "chat not found" in str(envelope.get("description", "")).lower():
                return _chat_failure(NOT_FOUND, FailureKind.NOT_FOUND, "Telegram did not find the chat")
            if status == 401:
                return _chat_failure(
                    PROVIDER_REJECTED, FailureKind.AUTHENTICATION, "Telegram rejected the bot token"
                )
            if status == 403:
                return _chat_failure(
                    PROVIDER_REJECTED, FailureKind.AUTHORIZATION, "Telegram denied access to the chat"
                )
            return _chat_failure(
                PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Telegram rejected HTTP {status}"
            )
        result = envelope.get("result")
        if exchange.status != 200 or not isinstance(result, dict):
            return _chat_failure(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Telegram returned an inconsistent response"
            )
        if (
            type(result.get("id")) is not int
            or result["id"] == 0
            or result.get("type") not in _CHAT_TYPES
            or not isinstance(result.get("title", ""), str)
            or not isinstance(result.get("username", ""), str)
        ):
            return _chat_failure(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Telegram returned unusable chat data"
            )
        return QueryAttempt.branch_result(
            FOUND,
            Chat(result["id"], result["type"], result.get("title", ""), result.get("username", "")),
            Receipt(),
        )


def _body_retry_after(body: bytes) -> float:
    """Read Bot API parameters.retry_after seconds when Retry-After is absent."""
    try:
        envelope = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return 0
    parameters = envelope.get("parameters") if isinstance(envelope, dict) else None
    seconds = parameters.get("retry_after") if isinstance(parameters, dict) else None
    if type(seconds) is not int or seconds <= 0:
        return 0
    return parse_retry_after(str(seconds), datetime.now(UTC))


def _valid_chat_id(value: Any) -> bool:
    if not isinstance(value, str):
        return False
    if _CHAT_USERNAME.fullmatch(value):
        return True
    if not re.fullmatch(r"-?[0-9]+", value):
        return False
    numeric = int(value)
    return numeric != 0 and -(1 << 63) <= numeric < 1 << 63


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _send_failure(branch: str, kind: FailureKind, message: str) -> MutationAttempt[SentMessage]:
    return MutationAttempt.branch_result(
        branch, SentMessage(), Receipt(), _failure("sendMessage", kind, message)
    )


def _send_retry(kind: FailureKind, message: str, after: float = 0) -> MutationAttempt[SentMessage]:
    return MutationAttempt.retry(_failure("sendMessage", kind, message), after)


def _send_uncertain(kind: FailureKind, message: str) -> MutationAttempt[SentMessage]:
    return MutationAttempt.uncertain(SentMessage(), _failure("sendMessage", kind, message), Receipt())


def _chat_failure(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Chat]:
    return QueryAttempt.branch_result(branch, Chat(), Receipt(), _failure("getChat", kind, message))


def _chat_retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[Chat]:
    return QueryAttempt.retry(_failure("getChat", kind, message), after)
