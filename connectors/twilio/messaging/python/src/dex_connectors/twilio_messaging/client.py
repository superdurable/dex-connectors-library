# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Twilio Messages HTTP client and secret-safe resource decoding."""

import base64
import json
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from email.utils import parsedate_to_datetime
from typing import TYPE_CHECKING, NoReturn
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
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
from .models import Message

if TYPE_CHECKING:
    from .operations import GetMessageOperation, SendMessageOperation

_ACCOUNT = re.compile(r"AC[0-9a-f]{32}\Z")
_API_KEY = re.compile(r"SK[0-9a-f]{32}\Z")
_MESSAGE = re.compile(r"(?:SM|MM)[0-9a-f]{32}\Z")
_SERVICE = re.compile(r"MG[0-9a-f]{32}\Z")
_PHONE = re.compile(r"\+[1-9][0-9]{6,14}\Z")
_STATUS = re.compile(r"[a-z_]{1,32}\Z")
_DIRECTION = re.compile(r"[a-z-]{1,32}\Z")
_PRICE = re.compile(r"-?[0-9]{1,12}(?:\.[0-9]{1,12})?\Z")
_CURRENCY = re.compile(r"[A-Z]{3}\Z")
_NUMBER = re.compile(r"[0-9]{1,9}\Z")
_ZERO_TIME = datetime.min.replace(tzinfo=UTC)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded response with evidence about whether a request was dispatched."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""
    is_body_too_large: bool = False
    has_body_read_failed: bool = False


class Client:
    """Send authenticated Twilio requests; close a connector-owned HTTP client with ``aclose``."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Twilio configuration and credential provider are required")
        account_sid = config.account_sid.strip()
        if not _ACCOUNT.fullmatch(account_sid):
            raise ValueError("Twilio accountSid must be AC followed by 32 lowercase hexadecimal characters")
        endpoint = validate_base_url(config.endpoint or "https://api.twilio.com/2010-04-01")
        max_response_bytes = config.max_response_bytes or 1048576
        if (
            isinstance(max_response_bytes, bool)
            or not isinstance(max_response_bytes, int)
            or max_response_bytes < 1
        ):
            raise ValueError("Twilio maxResponseBytes must be positive")
        self.account_sid = account_sid
        self.messages_url = endpoint.rstrip("/") + f"/Accounts/{account_sid}/Messages"
        self.default_sender = (
            parse_sender(config.default_sender) if config.default_sender.strip() else ("", "")
        )
        self.max_response_bytes = max_response_bytes
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None
        self._now = now or (lambda: datetime.now(UTC))

    def send_message(self) -> "SendMessageOperation":
        """Bind a fenced message send Mutation."""
        from .operations import SendMessageOperation

        return SendMessageOperation(self)

    def get_message(self) -> "GetMessageOperation":
        """Bind a message status Query."""
        from .operations import GetMessageOperation

        return GetMessageOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned HTTP client."""
        if self._owns_http_client:
            await self._http_client.aclose()

    async def authorization(
        self, call: Call, operation: str
    ) -> tuple[tuple[str, str] | None, Failure | None, bool]:
        """Resolve current credentials, separating transient lookup errors from invalid material."""
        try:
            credentials = await self._credentials.resolve(call)
        except ReauthorizationRequiredError:
            return (
                None,
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Twilio connection credentials require reauthorization; reconnect the connection",
                ),
                False,
            )
        except Exception:
            return (
                None,
                failure(
                    operation, FailureKind.AVAILABILITY, "Twilio credential lookup is temporarily unavailable"
                ),
                True,
            )
        if not isinstance(credentials, Credentials):
            return (
                None,
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Twilio connection credentials are incomplete; update or reconnect the connection",
                ),
                False,
            )
        if credentials.auth_method_id == "auth-token" and credentials.auth_token is not None:
            username, password = self.account_sid, credentials.auth_token.reveal()
        elif (
            credentials.auth_method_id == "api-key"
            and credentials.api_key_secret is not None
            and credentials.api_key_sid
        ):
            username, password = credentials.api_key_sid.strip(), credentials.api_key_secret.reveal()
            if not _API_KEY.fullmatch(username):
                return (
                    None,
                    failure(
                        operation,
                        FailureKind.AUTHENTICATION,
                        "Twilio api_key_sid must be SK followed by 32 lowercase hexadecimal characters",
                    ),
                    False,
                )
        else:
            return (
                None,
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Twilio connection credentials are incomplete; update or reconnect the connection",
                ),
                False,
            )
        if not password or not is_header_safe_credential(password):
            return (
                None,
                failure(operation, FailureKind.AUTHENTICATION, "Twilio credential secret is malformed"),
                False,
            )
        return (username, password), None, False

    async def exchange(
        self, authorization: tuple[str, str], method: str, url: str, form: list[tuple[str, str]] | None = None
    ) -> Exchange:
        """Perform one request without redirects or transparent HTTP replay."""
        token = base64.b64encode(f"{authorization[0]}:{authorization[1]}".encode()).decode("ascii")
        headers = {"Authorization": f"Basic {token}", "Accept": "application/json"}
        content = None
        if form is not None:
            headers["Content-Type"] = "application/x-www-form-urlencoded"
            content = urlencode(sorted(form, key=lambda pair: pair[0])).encode("ascii")
        try:
            request = self._http_client.build_request(method, url, headers=headers, content=content)
        except (httpx.HTTPError, ValueError):
            return Exchange(error="notBuilt")
        try:
            response, _ = await send_traced(self._http_client, request)
        except ProviderTransportError as error:
            return Exchange(error="beforeSend" if error.provably_not_sent else "afterSend")
        limit = self.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
        try:
            body = await read_bounded_body(response, limit)
        except BodyTooLargeError:
            return Exchange(response.status_code, response.headers, is_body_too_large=True)
        except ProviderTransportError:
            return Exchange(response.status_code, response.headers, has_body_read_failed=True)
        return Exchange(response.status_code, response.headers, body)

    def decode_message(self, body: bytes, authorization: tuple[str, str]) -> Message:
        """Validate the safe fields of a Twilio Message resource."""
        try:
            resource = json.loads(body, parse_constant=reject_invalid_json_constant)
            if not isinstance(resource, dict):
                raise ValueError("message must be an object")
            sid, account, recipient, status = (
                resource[key] for key in ("sid", "account_sid", "to", "status")
            )
            if not isinstance(sid, str) or not _MESSAGE.fullmatch(sid) or account != self.account_sid:
                raise ValueError("message identity differs")
            if not isinstance(status, str) or not _STATUS.fullmatch(status) or not safe_address(recipient):
                raise ValueError("invalid message status or recipient")
            service = optional_string(resource.get("messaging_service_sid"))
            sender = optional_string(resource.get("from"))
            if service and not _SERVICE.fullmatch(service):
                raise ValueError("invalid Messaging Service SID")
            if sender and not safe_address(sender):
                raise ValueError("invalid sender")
            direction = optional_string(resource.get("direction"))
            price = optional_string(resource.get("price"))
            price_unit = optional_string(resource.get("price_unit"))
            created = optional_string(resource.get("date_created"))
            sent = optional_string(resource.get("date_sent"))
            updated = optional_string(resource.get("date_updated"))
            message = Message(
                sid=sid,
                account_sid=account,
                messaging_service_sid=service,
                from_=sender,
                to=recipient,
                status=status,
                direction=direction if _DIRECTION.fullmatch(direction) else "",
                error_code=flexible_integer(resource.get("error_code")),
                segment_count=flexible_integer(resource.get("num_segments")),
                media_count=flexible_integer(resource.get("num_media")),
                price=price if _PRICE.fullmatch(price) else "",
                price_unit=price_unit if _CURRENCY.fullmatch(price_unit) else "",
                created_at=twilio_time(created),
                sent_at=twilio_time(sent),
                updated_at=twilio_time(updated),
            )
            if any(
                authorization[1] in value
                for value in (
                    sid,
                    account,
                    service,
                    sender,
                    recipient,
                    status,
                    message.direction,
                    message.price,
                    message.price_unit,
                )
            ):
                raise ValueError("response reflects credential")
            return message
        except (KeyError, TypeError, ValueError, UnicodeDecodeError) as error:
            raise ValueError("Twilio returned an invalid message") from error

    def receipt(
        self, call: Call, exchange: Exchange | None = None, message_sid: str = "", error_code: int = 0
    ) -> Receipt:
        """Persist a bounded provider correlation ID without request or response secrets."""
        request_id = exchange.headers.get("Twilio-Request-Id", "") if exchange else ""
        if not safe_address(request_id):
            request_id = ""
        metadata = {"twilioErrorCode": str(error_code)} if error_code > 0 else {}
        return Receipt(
            call.id,
            "twilio",
            self._now().astimezone(UTC),
            call.idempotency_key,
            message_sid,
            request_id,
            metadata,
        )

    def retry_after(self, exchange: Exchange) -> float:
        """Read Twilio's bounded Retry-After hint."""
        return parse_retry_after(exchange.headers.get("Retry-After", ""), self._now())


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Construct a secret-safe provider failure."""
    return Failure(kind, "twilio", operation, message)


def parse_sender(value: str) -> tuple[str, str]:
    """Validate an E.164 sender, WhatsApp sender, or Messaging Service SID."""
    sender = value.strip()
    if _SERVICE.fullmatch(sender):
        return "", sender
    if _PHONE.fullmatch(sender) or is_whatsapp(sender):
        return sender, ""
    raise ValueError(
        "sender must be an E.164 phone number, a whatsapp:+E.164 address, or an MG Messaging Service SID"
    )


def is_whatsapp(value: str) -> bool:
    """Check Twilio's WhatsApp address syntax."""
    return value.startswith("whatsapp:") and bool(_PHONE.fullmatch(value[9:]))


def safe_address(value: object) -> bool:
    """Check a printable bounded provider identifier."""
    return (
        isinstance(value, str)
        and 0 < len(value.encode("utf-8")) <= 64
        and all(32 <= ord(ch) <= 126 for ch in value)
    )


def flexible_integer(value: object) -> int:
    """Parse Twilio's decimal string or integer fields, treating null as zero."""
    if value is None or value == "":
        return 0
    text = str(value) if isinstance(value, int) and not isinstance(value, bool) else value
    if not isinstance(text, str) or not _NUMBER.fullmatch(text):
        raise ValueError("Twilio integer field is invalid")
    return int(text)


def optional_string(value: object) -> str:
    """Match Go pointer-to-string decoding for nullable provider fields."""
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("Twilio string field is invalid")
    return value


def reject_invalid_json_constant(value: str) -> NoReturn:
    """Reject Python's nonstandard NaN and Infinity JSON extensions."""
    raise ValueError("invalid JSON constant")


def twilio_time(value: object) -> datetime:
    """Parse Twilio RFC1123Z timestamps; malformed optional times become zero."""
    if not isinstance(value, str):
        return _ZERO_TIME
    try:
        parsed = parsedate_to_datetime(value)
        return parsed.astimezone(UTC) if parsed.tzinfo else _ZERO_TIME
    except (TypeError, ValueError, IndexError):
        return _ZERO_TIME


def error_code(body: bytes) -> int:
    """Read only Twilio's bounded numeric error code."""
    try:
        value = json.loads(body).get("code")
        return flexible_integer(value)
    except (TypeError, ValueError, AttributeError):
        return 0


def rejection(operation: str, subject: str, status: int, code: int) -> Failure:
    """Classify a conclusive provider rejection."""
    kind = {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
    }.get(status, FailureKind.PROVIDER_REJECTION)
    message = f"Twilio rejected the {subject}"
    if code > 0:
        message += f" with error {code}"
    return failure(operation, kind, message)
