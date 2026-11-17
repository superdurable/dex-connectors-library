# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify inbound webhooks and deliver requestReceived events through durable bindings."""

from __future__ import annotations

import base64
import binascii
import hashlib
import hmac
import json
import re
import weakref
from collections.abc import Awaitable, Callable, Iterable, MutableMapping, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Final, TypeAlias
from urllib.parse import parse_qs

from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    ConnectionRef,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerConfig,
    TriggerContext,
    TriggerEvent,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)
from dex_connectors.sdk.webhooktrigger import (
    WebhookEndpoint,
    WebhookEndpointConfig,
    WebhookEndpointRunner,
    WebhookRequest,
    WebhookResponse,
    WebhookVerificationUnavailableError,
)

from ._generated.definitions import CONNECTOR_ID
from ._generated.definitions import REQUEST_RECEIVED_TRIGGER_DEFINITION as REQUEST_RECEIVED_TRIGGER_DEFINITION
from ._generated.triggers import decode_request_received_trigger_configuration
from ._generated.triggers import (
    define_request_received_trigger_binding as define_request_received_trigger_binding,
)
from .client import Client, Config, Credentials
from .connection import Connection
from .models import RequestReceivedTriggerConfiguration, WebhookRequestEvent

CONTENT_TYPE_JSON: Final = "application/json"
CONTENT_TYPE_FORM: Final = "application/x-www-form-urlencoded"
_TRIGGER_NAME: Final = REQUEST_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name
_HEADER_NAME: Final = re.compile(r"^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
_BAD_FORM_ESCAPE: Final = re.compile(r"%(?![0-9A-Fa-f]{2})")
_DENIED_HEADERS: Final = {"authorization", "proxy-authorization", "cookie", "set-cookie", "webhook-signature"}
_ASGIScope: TypeAlias = MutableMapping[str, Any]
_ASGIReceive: TypeAlias = Callable[[], Awaitable[MutableMapping[str, Any]]]
_ASGISend: TypeAlias = Callable[[MutableMapping[str, Any]], Awaitable[None]]
_ENDPOINTS: weakref.WeakKeyDictionary[
    Client, dict[ConnectionRef, WebhookEndpoint[Credentials, WebhookRequestEvent]]
] = weakref.WeakKeyDictionary()


class _JSONNumber(str):
    pass


@dataclass(frozen=True, slots=True)
class _DecodedRequestEvent(TriggerEvent[WebhookRequestEvent]):
    raw_document: Any


def _canonical_header(name: str) -> str:
    name = name.strip()
    if not _HEADER_NAME.fullmatch(name):
        raise ValueError("webhook header name is invalid")
    return "-".join(part[:1].upper() + part[1:].lower() for part in name.split("-"))


def _pointer_tokens(pointer: str) -> tuple[str, ...]:
    if not pointer.startswith("/"):
        raise ValueError("JSON Pointer must start with /")
    tokens = []
    for encoded in pointer[1:].split("/"):
        if re.search(r"~(?![01])|~$", encoded):
            raise ValueError("JSON Pointer escape is invalid")
        tokens.append(encoded.replace("~1", "/").replace("~0", "~"))
    return tuple(tokens)


def _pointer_value(document: Any, pointer: str) -> Any:
    value = document
    for token in _pointer_tokens(pointer):
        if isinstance(value, dict):
            if token not in value:
                return None
            value = value[token]
        elif isinstance(value, list):
            if not token.isascii() or not token.isdigit() or (len(token) > 1 and token[0] == "0"):
                return None
            index = int(token)
            if index >= len(value):
                return None
            value = value[index]
        else:
            return None
    return value


def _scalar_text(value: Any) -> str | None:
    if isinstance(value, _JSONNumber):
        return str(value)
    if isinstance(value, str):
        return value
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return json.dumps(value, separators=(",", ":"))
    return None


def validate_request_received_trigger_configuration(
    configuration: RequestReceivedTriggerConfiguration,
) -> None:
    """Require a valid RFC 6901 pointer and at least one value when filtering."""
    if bool(configuration.match_pointer) != bool(configuration.match_values):
        raise ValueError("webhook matchPointer and matchValues must be set together")
    if configuration.match_pointer:
        _pointer_tokens(configuration.match_pointer)


def _validate_config(config: Config) -> tuple[str, str, tuple[str, ...]]:
    if config.verification not in {"hmacSha256", "standardWebhooks", "sharedToken"}:
        raise ValueError("webhook verification scheme is invalid")
    if config.signature_encoding not in {"hex", "base64"}:
        raise ValueError("webhook signatureEncoding is invalid")
    if config.timestamp_tolerance_seconds <= 0 or config.max_body_bytes <= 0:
        raise ValueError("webhook timestamp tolerance and maximum body bytes must be positive")
    if any(ord(character) < 32 or ord(character) == 127 for character in config.signature_prefix):
        raise ValueError("webhook signaturePrefix cannot contain control characters")
    signature_header = _canonical_header(config.signature_header)
    token_header = _canonical_header(config.token_header)
    if config.event_id_header:
        _canonical_header(config.event_id_header)
    if config.event_id_pointer:
        _pointer_tokens(config.event_id_pointer)
    denied = _DENIED_HEADERS | {signature_header.lower(), token_header.lower()}
    forwarded: list[str] = []
    for header in config.forwarded_headers:
        canonical = _canonical_header(header)
        if canonical.lower() in denied:
            raise ValueError(f"webhook forwardedHeaders cannot include {canonical}")
        if canonical not in forwarded:
            forwarded.append(canonical)
    return signature_header, token_header, tuple(forwarded)


def _decode_signature(encoded: str, encoding: str) -> bytes:
    try:
        if encoding == "base64":
            return base64.b64decode(encoded, validate=True)
        if not re.fullmatch(r"(?:[0-9A-Fa-f]{2})*", encoded):
            raise ValueError("invalid hexadecimal signature")
        return bytes.fromhex(encoded)
    except (ValueError, binascii.Error) as error:
        raise ValueError("webhook signature encoding is invalid") from error


async def _verify_request(
    request: WebhookRequest,
    credentials: Credentials,
    cancellation: TriggerCancellation,
    *,
    config: Config,
    signature_header: str,
    token_header: str,
) -> None:
    secret = credentials.signing_secret.reveal()
    if not secret:
        raise WebhookVerificationUnavailableError()
    if config.verification == "sharedToken":
        token = request.header(token_header)
        if not token or not hmac.compare_digest(
            hashlib.sha256(token.encode()).digest(), hashlib.sha256(secret.encode()).digest()
        ):
            raise ValueError("webhook token does not match")
        return
    if config.verification == "hmacSha256":
        header = (request.header(signature_header) or "").strip()
        if not header or not header.startswith(config.signature_prefix):
            raise ValueError("webhook signature is missing")
        signature = _decode_signature(header[len(config.signature_prefix) :], config.signature_encoding)
        expected = hmac.new(secret.encode(), request.body, hashlib.sha256).digest()
        if not hmac.compare_digest(signature, expected):
            raise ValueError("webhook signature does not match")
        return
    try:
        key = base64.b64decode(secret[6:], validate=True) if secret.startswith("whsec_") else secret.encode()
        if not key:
            raise ValueError
    except (ValueError, binascii.Error) as error:
        raise WebhookVerificationUnavailableError() from error
    event_id = request.header("webhook-id") or ""
    timestamp_text = request.header("webhook-timestamp") or ""
    signatures = request.header("webhook-signature") or ""
    if not event_id or not timestamp_text or not signatures:
        raise ValueError("Standard Webhooks headers are incomplete")
    if not re.fullmatch(r"[+-]?[0-9]+", timestamp_text):
        raise ValueError("Standard Webhooks timestamp is invalid")
    try:
        sent_at = datetime.fromtimestamp(int(timestamp_text), tz=UTC)
    except (OverflowError, OSError, ValueError) as error:
        raise ValueError("Standard Webhooks timestamp is invalid") from error
    if abs((request.received_at - sent_at).total_seconds()) > config.timestamp_tolerance_seconds:
        raise ValueError("Standard Webhooks timestamp is outside the tolerance")
    expected = hmac.new(
        key, event_id.encode() + b"." + timestamp_text.encode() + b"." + request.body, hashlib.sha256
    ).digest()
    for entry in signatures.split():
        version, separator, encoded = entry.partition(",")
        if separator and version == "v1":
            try:
                if hmac.compare_digest(base64.b64decode(encoded, validate=True), expected):
                    return
            except (ValueError, binascii.Error):
                continue
    raise ValueError("Standard Webhooks signature does not match")


def _event_id(value: str) -> str:
    if not 1 <= len(value.encode()) <= 256 or any(
        ord(character) < 33 or ord(character) > 126 for character in value
    ):
        raise ValueError("webhook event ID must be printable ASCII without spaces and at most 256 bytes")
    return value


def _decode_event(
    request: WebhookRequest, *, config: Config, forwarded_headers: tuple[str, ...]
) -> TriggerEvent[WebhookRequestEvent]:
    content_type = (request.header("content-type") or "").split(";", 1)[0].strip().lower()
    if content_type == CONTENT_TYPE_JSON:
        try:
            raw_document = json.loads(
                request.body,
                parse_int=_JSONNumber,
                parse_float=_JSONNumber,
                parse_constant=lambda value: (_ for _ in ()).throw(ValueError(value)),
            )
            document = json.loads(request.body)
        except (ValueError, UnicodeDecodeError) as error:
            raise ValueError("webhook JSON body is invalid") from error
        form_body: dict[str, tuple[str, ...]] = {}
    elif content_type == CONTENT_TYPE_FORM:
        try:
            encoded = request.body.decode("utf-8")
            if _BAD_FORM_ESCAPE.search(encoded) or ";" in encoded:
                raise ValueError
            parsed = parse_qs(encoded, keep_blank_values=True, strict_parsing=False, errors="strict")
        except (ValueError, UnicodeDecodeError) as error:
            raise ValueError("webhook form body is invalid") from error
        form_body = {name: tuple(values) for name, values in parsed.items()}
        raw_document = {name: list(values) for name, values in parsed.items()}
        document = None
    else:
        raise ValueError("webhook Content-Type is not supported")
    if config.verification == "standardWebhooks":
        event_id = _event_id(request.header("webhook-id") or "")
    elif config.event_id_header and request.header(config.event_id_header):
        event_id = _event_id(request.header(config.event_id_header) or "")
    elif config.event_id_pointer:
        event_id = _event_id(_scalar_text(_pointer_value(raw_document, config.event_id_pointer)) or "")
    else:
        event_id = hashlib.sha256(request.body).hexdigest()
    headers = {
        name: ", ".join(request.header_values(name))
        for name in forwarded_headers
        if request.header_values(name)
    }
    payload = WebhookRequestEvent(
        content_type=content_type,
        headers=headers,
        json_body=document,
        form_body=form_body,
        received_at=request.received_at,
    )
    return _DecodedRequestEvent(event_id, request.received_at, payload, raw_document)


def _matches(
    event: TriggerEvent[WebhookRequestEvent], configuration: RequestReceivedTriggerConfiguration
) -> bool:
    if not configuration.match_pointer:
        return True
    payload = event.payload
    document = (
        event.raw_document
        if isinstance(event, _DecodedRequestEvent)
        else (
            payload.json_body
            if payload.content_type == CONTENT_TYPE_JSON
            else {name: list(values) for name, values in payload.form_body.items()}
        )
    )
    value = _pointer_value(document, configuration.match_pointer)
    return (_scalar_text(value) or "") in configuration.match_values if value is not None else False


def _decode_stored_payload(payload_json: str, payload_bytes: bytes) -> WebhookRequestEvent:
    decoded = CodecRegistry().resolve(WebhookRequestEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, WebhookRequestEvent):
        raise ValueError("pending webhook requestReceived payload is invalid")
    return decoded


def request_received_webhook_endpoint(
    connection: Connection,
) -> WebhookEndpoint[Credentials, WebhookRequestEvent]:
    """Return a shared ASGI endpoint for this provider-backed connection."""
    client = connection._client
    if not isinstance(client, Client):
        raise ValueError("requestReceived requires a provider-backed webhook connection")
    endpoints = _ENDPOINTS.setdefault(client, {})
    endpoint = endpoints.get(connection.reference)
    if endpoint is None:
        signature_header, token_header, forwarded_headers = _validate_config(client.config)
        endpoint = WebhookEndpoint(
            WebhookEndpointConfig(
                connector_id=CONNECTOR_ID,
                trigger_name=_TRIGGER_NAME,
                connection=connection.reference,
                credentials=client.credentials,
                verify_request=lambda request, credentials, cancellation: _verify_request(
                    request,
                    credentials,
                    cancellation,
                    config=client.config,
                    signature_header=signature_header,
                    token_header=token_header,
                ),
                decode_event=lambda request: _decode_event(
                    request, config=client.config, forwarded_headers=forwarded_headers
                ),
                maximum_body_bytes=client.config.max_body_bytes,
                clock=client.clock,
            )
        )
        endpoints[connection.reference] = endpoint
    return endpoint


def new_request_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[WebhookRequestEvent],
    configuration: RequestReceivedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Run one filtered requestReceived binding; use the project runner for durable delivery."""
    if not binding_name.strip() or target is None:
        raise ValueError("webhook requestReceived binding name and target are required")
    selected = configuration or RequestReceivedTriggerConfiguration()
    validate_request_received_trigger_configuration(selected)
    return new_trigger(
        TriggerConfig(
            definition=REQUEST_RECEIVED_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, REQUEST_RECEIVED_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=request_received_webhook_endpoint(connection).new_source(
                lambda event: _matches(event, selected)
            ),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectRequestReceivedTriggerRoute:
    """One stored requestReceived binding and its application target."""

    binding_name: str
    """Application binding name in Dex project configuration."""
    target: TriggerTarget[WebhookRequestEvent]
    """Target receiving the verified webhook through a durable inbox."""


class RequestReceivedWebhookRuntime:
    """Serve the shared endpoint and replay every durable binding inbox."""

    def __init__(self, connection: Connection, runners: Sequence[TriggerRunner]) -> None:
        """Bind one endpoint and at least one Trigger runner."""
        if not runners:
            raise ValueError("webhook requestReceived routes are required")
        self._endpoint = request_received_webhook_endpoint(connection)
        self._runner = WebhookEndpointRunner(self._endpoint, runners)

    async def __call__(self, scope: _ASGIScope, receive: _ASGIReceive, send: _ASGISend) -> None:
        """Serve an ASGI HTTP scope."""
        await self._runner(scope, receive, send)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Verify and persist one request before acknowledging it."""
        return await self._runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay pending events and receive new requests until cancelled."""
        await self._runner.run(context)

    def running_source_count(self) -> int:
        """Return the number of active binding sources."""
        return self._endpoint.running_source_count()


def new_project_request_received_webhook_runtime(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectRequestReceivedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> RequestReceivedWebhookRuntime:
    """Load stored filters and durably record each accepted request before HTTP 200."""
    if project is None or not routes:
        raise ValueError("webhook requestReceived project and routes are required")
    runners: list[TriggerRunner] = []
    names: set[str] = set()
    for route in routes:
        name = route.binding_name.strip()
        if not name or route.target is None or name in names:
            raise ValueError("webhook requestReceived binding names and targets must be unique")
        names.add(name)
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, name)
        configuration = decode_trigger_configuration(
            project.configuration, key, decode_request_received_trigger_configuration
        )
        validate_request_received_trigger_configuration(configuration)
        target = new_durable_trigger_target(
            project.trigger_inbox(key), route.target, decode_payload=_decode_stored_payload, logger=logger
        )
        runners.append(
            new_request_received_trigger(
                connection=connection,
                binding_name=name,
                configuration=configuration,
                target=target,
                logger=logger,
            )
        )
    return RequestReceivedWebhookRuntime(connection, runners)
