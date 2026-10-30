# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded, redirect-safe Stripe HTTP client and provider response decoding."""

import base64
import json
import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass, replace
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from .models import CheckoutSession, Config, Credentials, PaymentIntent, Refund

if TYPE_CHECKING:
    from .operations import (
        CreateACHCheckoutSessionOperation,
        CreateCardCheckoutSessionOperation,
        CreateRefundOperation,
        GetCheckoutSessionOperation,
        GetPaymentIntentOperation,
    )

_CURRENCY_PATTERN = re.compile(r"^[a-z]{3}$")
_REFUND_STATUSES = frozenset({"succeeded", "pending", "failed", "requires_action", "canceled"})


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """Response status and headers retained when the body exceeds its byte limit."""

    status_code: int
    headers: httpx.Headers
    body: bytes = b""
    is_body_too_large: bool = False


class Client:
    """Execute authenticated Stripe API calls without persisting credentials.

    The caller owns an injected HTTP client, if supplied, and closes it. A client
    constructed here is owned by this connector and closed with ``aclose``.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Stripe configuration and credential provider are required")
        config = replace(
            config,
            endpoint=config.endpoint or Config().endpoint,
            max_response_bytes=config.max_response_bytes or Config().max_response_bytes,
        )
        endpoint = validate_base_url(config.endpoint)
        parsed = urlsplit(endpoint)
        if parsed.scheme == "http" and parsed.hostname not in ("localhost", "127.0.0.1"):
            raise ValueError("Stripe endpoint must use HTTPS")
        if config.max_response_bytes <= 0:
            raise ValueError("Stripe maxResponseBytes must be positive")
        self.config = config
        self._endpoint = endpoint.rstrip("/")
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None
        self._now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the connector-owned HTTP client after all invocations finish."""
        if self._owns_http_client:
            await self._http_client.aclose()

    def create_card_checkout_session(self) -> "CreateCardCheckoutSessionOperation":
        """Return the card Checkout mutation bound to this client."""
        from .operations import CreateCardCheckoutSessionOperation

        return CreateCardCheckoutSessionOperation(self)

    def create_ach_checkout_session(self) -> "CreateACHCheckoutSessionOperation":
        """Return the US bank account Checkout mutation bound to this client."""
        from .operations import CreateACHCheckoutSessionOperation

        return CreateACHCheckoutSessionOperation(self)

    def get_checkout_session(self) -> "GetCheckoutSessionOperation":
        """Return the Checkout Session query bound to this client."""
        from .operations import GetCheckoutSessionOperation

        return GetCheckoutSessionOperation(self)

    def get_payment_intent(self) -> "GetPaymentIntentOperation":
        """Return the PaymentIntent query bound to this client."""
        from .operations import GetPaymentIntentOperation

        return GetPaymentIntentOperation(self)

    def create_refund(self) -> "CreateRefundOperation":
        """Return the refund mutation bound to this client."""
        from .operations import CreateRefundOperation

        return CreateRefundOperation(self)

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Resolve a secret key at call time, returning a safe authentication failure."""
        try:
            credentials = await self._credentials.resolve(call)
            if not isinstance(credentials, Credentials) or not credentials.secret_key.reveal():
                raise ValueError("empty Stripe key")
            return credentials, None
        except Exception:
            return None, stripe_failure(
                operation, FailureKind.AUTHENTICATION, "Stripe connection credentials are unavailable"
            )

    async def request(
        self,
        call: Call,
        secret_key: str,
        method: str,
        path: str,
        values: Mapping[str, str] | None = None,
    ) -> ProviderResponse:
        """Send one request, preserving status when its body is oversized."""
        target = f"{self._endpoint}/{path}"
        parameters = values if method == "GET" else None
        body = values if method == "POST" else None
        basic_token = base64.b64encode(f"{secret_key}:".encode()).decode("ascii")
        headers = {"Accept": "application/json", "Authorization": f"Basic {basic_token}"}
        if method == "POST":
            headers["Content-Type"] = "application/x-www-form-urlencoded"
            headers["Idempotency-Key"] = str(call.idempotency_key)
        request = self._http_client.build_request(
            method,
            target,
            params=parameters,
            data=body,
            headers=headers,
        )
        response, _ = await send_traced(self._http_client, request)
        try:
            contents = await read_bounded_body(response, self.config.max_response_bytes)
            return ProviderResponse(response.status_code, response.headers, contents)
        except BodyTooLargeError:
            return ProviderResponse(response.status_code, response.headers, is_body_too_large=True)

    def receipt(self, call: Call, response: ProviderResponse | None = None, object_id: str = "") -> Receipt:
        """Create a secret-safe provider correlation receipt for one attempt."""
        headers = response.headers if response is not None else httpx.Headers()
        return Receipt(
            call.id,
            "stripe",
            self._now().astimezone(UTC),
            call.idempotency_key,
            object_id,
            headers.get("Request-Id", ""),
        )


def stripe_failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Construct a persistable Stripe failure without provider response bodies."""
    return Failure(kind, "stripe", operation, message)


def retry_after_seconds(headers: httpx.Headers) -> float:
    """Read Stripe's integer Retry-After hint, capped to one hour like Go."""
    value = headers.get("Retry-After", "")
    if re.fullmatch(r"[+-]?[0-9]+", value) is None:
        return 0
    try:
        seconds = int(value)
    except ValueError:
        return 0
    if not -(2**63) <= seconds <= 2**63 - 1:
        return 0
    return float(min(seconds, 3600)) if seconds >= 1 else 0


def should_retry_keyed_server_failure(response: ProviderResponse) -> bool:
    """Respect Stripe replay and retry hints before classifying keyed 5xx."""
    if response.headers.get("Idempotent-Replayed", "").lower() == "true":
        return False
    hint = response.headers.get("Stripe-Should-Retry", "").lower()
    if hint == "false":
        return False
    if hint == "true":
        return True
    return response.status_code in (502, 503, 504)


def should_retry_keyed_conflict(response: ProviderResponse) -> bool:
    """Retry 409 unless Stripe rejects replay or the key's parameters differ."""
    return (
        response.headers.get("Stripe-Should-Retry", "").lower() != "false"
        and parse_stripe_api_error(response.body).get("type") != "idempotency_error"
    )


def classify_stripe_rejection(operation: str, response: ProviderResponse) -> Failure:
    """Map terminal Stripe status to a safe Failure kind."""
    kind = {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
    }.get(response.status_code, FailureKind.PROVIDER_REJECTION)
    return stripe_failure(operation, kind, "Stripe rejected the request")


def parse_stripe_api_error(body: bytes) -> dict[str, str]:
    """Extract bounded Stripe error tokens from a complete response body."""
    try:
        decoded = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return {}
    error = decoded.get("error") if isinstance(decoded, dict) else None
    if not isinstance(error, dict):
        return {}
    return {key: value for key in ("type", "code", "message") if isinstance(value := error.get(key), str)}


def decode_json_object(body: bytes, resource: str) -> dict[str, object]:
    """Decode one complete provider object, reporting only fixed safe errors."""
    if not body:
        raise ValueError(f"Stripe response is invalid: Stripe returned an empty {resource}")
    try:
        decoded = json.loads(body)
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError(f"Stripe response is invalid: Stripe returned malformed {resource} JSON") from error
    if not isinstance(decoded, dict):
        raise ValueError(f"Stripe response is invalid: Stripe returned an invalid {resource}")
    return decoded


def _string(value: object) -> str:
    return value if isinstance(value, str) else ""


def _integer(value: object) -> int:
    return value if isinstance(value, int) and not isinstance(value, bool) else 0


def _object_id(value: object) -> str:
    if isinstance(value, dict):
        return _string(value.get("id"))
    return _string(value)


def _check_fields(
    decoded: dict[str, object], resource: str, strings: tuple[str, ...], integers: tuple[str, ...]
) -> None:
    for field_name in strings:
        if decoded.get(field_name) is not None and not isinstance(decoded[field_name], str):
            raise ValueError(f"Stripe response is invalid: Stripe returned malformed {resource} JSON")
    for field_name in integers:
        value = decoded.get(field_name)
        if value is not None and (not isinstance(value, int) or isinstance(value, bool)):
            raise ValueError(f"Stripe response is invalid: Stripe returned malformed {resource} JSON")


def _check_object_id(value: object, resource: str) -> None:
    if value is None or isinstance(value, str):
        return
    if isinstance(value, dict):
        _check_fields(value, resource, ("id",), ())
        return
    raise ValueError(f"Stripe response is invalid: Stripe returned malformed {resource} JSON")


def decode_checkout_session(body: bytes) -> CheckoutSession:
    """Validate and reduce a Stripe Checkout Session to connector-safe fields."""
    decoded = decode_json_object(body, "Checkout Session")
    _check_fields(
        decoded,
        "Checkout Session",
        ("id", "object", "url", "client_reference_id", "payment_status", "status", "currency"),
        ("amount_total", "expires_at"),
    )
    _check_object_id(decoded.get("payment_intent"), "Checkout Session")
    identifier = _string(decoded.get("id"))
    if decoded.get("object") != "checkout.session" or not identifier.startswith("cs_"):
        raise ValueError("Stripe response is invalid: Stripe returned an invalid Checkout Session")
    metadata = decoded.get("metadata")
    if metadata is not None and (
        not isinstance(metadata, dict)
        or not all(isinstance(k, str) and isinstance(v, str) for k, v in metadata.items())
    ):
        raise ValueError("Stripe response is invalid: Stripe returned malformed Checkout Session JSON")
    if metadata is None:
        metadata = {}
    expiry = _integer(decoded.get("expires_at"))
    try:
        expires_at = datetime.fromtimestamp(expiry, UTC) if expiry > 0 else None
    except (OverflowError, OSError, ValueError) as error:
        raise ValueError(
            "Stripe response is invalid: Stripe returned malformed Checkout Session JSON"
        ) from error
    return CheckoutSession(
        identifier,
        _string(decoded.get("url")),
        _string(decoded.get("client_reference_id")),
        _string(decoded.get("payment_status")),
        _string(decoded.get("status")),
        _object_id(decoded.get("payment_intent")),
        _string(decoded.get("currency")),
        _integer(decoded.get("amount_total")),
        expires_at,
        metadata,
    )


def decode_payment_intent(body: bytes) -> PaymentIntent:
    """Validate a PaymentIntent and reduce its latest charge and receipt email."""
    decoded = decode_json_object(body, "PaymentIntent")
    _check_fields(
        decoded,
        "PaymentIntent",
        ("id", "object", "currency", "status", "receipt_email"),
        ("amount",),
    )
    identifier = _string(decoded.get("id"))
    currency = _string(decoded.get("currency"))
    status = _string(decoded.get("status"))
    if (
        decoded.get("object") != "payment_intent"
        or not identifier.startswith("pi_")
        or not currency
        or not status
    ):
        raise ValueError("Stripe response is invalid: Stripe returned an invalid PaymentIntent")
    charge = decoded.get("latest_charge")
    _check_object_id(charge, "PaymentIntent")
    charge_id = _object_id(charge)
    refunded = 0
    billing_email = ""
    if isinstance(charge, dict):
        _check_fields(charge, "PaymentIntent", (), ("amount_refunded",))
        refunded = _integer(charge.get("amount_refunded"))
        billing = charge.get("billing_details")
        if billing is not None and not isinstance(billing, dict):
            raise ValueError("Stripe response is invalid: Stripe returned malformed PaymentIntent JSON")
        if isinstance(billing, dict):
            _check_fields(billing, "PaymentIntent", ("email",), ())
            billing_email = _string(billing.get("email"))
    return PaymentIntent(
        identifier,
        _integer(decoded.get("amount")),
        currency,
        status,
        refunded,
        _string(decoded.get("receipt_email")) or billing_email,
        charge_id,
    )


def decode_refund(body: bytes) -> Refund:
    """Validate a Stripe refund, including its accepted provider status."""
    decoded = decode_json_object(body, "refund")
    _check_fields(decoded, "refund", ("id", "object", "status", "currency"), ("amount",))
    identifier = _string(decoded.get("id"))
    status = _string(decoded.get("status"))
    currency = _string(decoded.get("currency"))
    if (
        decoded.get("object") != "refund"
        or not identifier.startswith("re_")
        or status not in _REFUND_STATUSES
        or not currency
    ):
        raise ValueError("Stripe response is invalid: Stripe returned an invalid refund")
    return Refund(identifier, status, _integer(decoded.get("amount")), currency)
