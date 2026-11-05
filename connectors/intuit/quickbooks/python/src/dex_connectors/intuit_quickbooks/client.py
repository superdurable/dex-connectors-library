# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""QuickBooks Online transport, credential checks, and response classification."""

import base64
import binascii
import ipaddress
import json
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import urlencode, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        CreateCustomerOperation,
        CreateInvoiceOperation,
        FindCustomerOperation,
        GetInvoiceOperation,
        ListInvoicesOperation,
        RecordPaymentOperation,
        SendInvoiceOperation,
    )

_ID = re.compile(r"[0-9]{1,20}\Z")
_REALM = re.compile(r"[1-9][0-9]{0,24}\Z")
_TID = re.compile(r"[A-Za-z0-9-]{1,64}\Z")
_FAULT_TYPE = re.compile(r"[A-Za-z]{1,64}\Z")
_FAULT_CODE = re.compile(r"[0-9]{1,10}\Z")


@dataclass(slots=True)
class Exchange:
    """One classified QuickBooks provider exchange."""

    outcome: str
    failure: Failure | None = None
    status: int = 0
    body: bytes = b""
    request_id: str = ""
    metadata: dict[str, str] = field(default_factory=dict)
    retry_after: float = 0


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "quickbooks", operation, message)


def _encode_query(values: dict[str, str]) -> str:
    return urlencode(sorted(values.items()))


def _fault(body: bytes) -> tuple[str, list[str], int]:
    try:
        envelope = json.loads(body)
        fault = envelope.get("Fault") if isinstance(envelope, dict) else None
        if not isinstance(fault, dict):
            return "", [], 0
        fault_type = fault.get("type", "")
        fault_type = fault_type if isinstance(fault_type, str) and _FAULT_TYPE.fullmatch(fault_type) else ""
        errors = fault.get("Error", [])
        if not isinstance(errors, list):
            return fault_type, [], 0
        codes: list[str] = []
        for error in errors:
            if isinstance(error, dict):
                code = error.get("code", "")
                if isinstance(code, (str, int)) and _FAULT_CODE.fullmatch(str(code)) and len(codes) < 5:
                    codes.append(str(code))
        return fault_type, codes, min(len(errors), 1000)
    except (ValueError, TypeError):
        return "", [], 0


def _classify(
    operation: str, status: int, headers: httpx.Headers, body: bytes, limit: int, token: str, now: datetime
) -> Exchange:
    request_id = headers.get("intuit_tid", "")
    if not _TID.fullmatch(request_id):
        request_id = ""
    fault_type, codes, count = _fault(body[:65536])
    metadata = {"faultCode": codes[0]} if codes else {}
    suffix_parts = ([fault_type] if fault_type else []) + (["code " + ", ".join(codes)] if codes else [])
    if count > 1:
        suffix_parts.append(f"{count} errors")
    suffix = f" [{'; '.join(suffix_parts)}]" if suffix_parts else ""

    def result(outcome: str, kind: FailureKind, message: str, retry_after: float = 0) -> Exchange:
        return Exchange(
            outcome,
            _failure(operation, kind, f"{message} (HTTP {status}){suffix}"),
            status,
            b"",
            request_id,
            metadata,
            retry_after,
        )

    if not 200 <= status < 300:
        if status == 429:
            delay = parse_retry_after(headers.get("Retry-After", ""), now) or 60.0
            return result("retry", FailureKind.RATE_LIMIT, "QuickBooks throttled the request", delay)
        if status == 501:
            return result(
                "rejected", FailureKind.PROVIDER_REJECTION, "QuickBooks does not implement the request"
            )
        if status == 408 or status >= 500:
            return result("retry", FailureKind.AVAILABILITY, "QuickBooks is temporarily unavailable")
        if status == 401:
            return result("rejected", FailureKind.AUTHENTICATION, "QuickBooks rejected the access token")
        if status == 403:
            message = (
                "QuickBooks token lacks the com.intuit.quickbooks.accounting scope"
                if "3100" in codes
                else "QuickBooks denied access to the company or the request"
            )
            return result("rejected", FailureKind.AUTHORIZATION, message)
        if status == 404:
            return result("notFound", FailureKind.NOT_FOUND, "QuickBooks found no such resource")
        if status == 409:
            return result(
                "retry", FailureKind.CONFLICT, "QuickBooks returned a conflict; retrying the same requestid"
            )
        if 300 <= status < 400:
            return result("rejected", FailureKind.PROTOCOL, "QuickBooks redirected the request")
        if status != 400:
            return result("rejected", FailureKind.PROVIDER_REJECTION, "QuickBooks rejected the request")
    if status == 400 or fault_type or count:
        if "600" in codes:
            return result(
                "retry",
                FailureKind.CONFLICT,
                "QuickBooks is still answering an earlier attempt with this requestid",
            )
        if "610" in codes:
            return result("notFound", FailureKind.NOT_FOUND, "QuickBooks found no such object")
        if "6240" in codes:
            return result(
                "nameConflict",
                FailureKind.CONFLICT,
                "QuickBooks already has a name list entry with that display name",
            )
        if "6140" in codes:
            return result(
                "rejected",
                FailureKind.CONFLICT,
                "QuickBooks already has a transaction with that document number",
            )
        if fault_type.lower() == "authenticationfault":
            return result("rejected", FailureKind.AUTHENTICATION, "QuickBooks rejected the authentication")
        if fault_type.lower() == "authorizationfault":
            return result("rejected", FailureKind.AUTHORIZATION, "QuickBooks denied the request")
        if fault_type.lower() == "systemfault":
            return result("retry", FailureKind.AVAILABILITY, "QuickBooks failed internally")
        return result("rejected", FailureKind.VALIDATION, "QuickBooks rejected the request")
    if len(body) > limit:
        return Exchange(
            "invalid",
            _failure(
                operation,
                FailureKind.RESPONSE_TOO_LARGE,
                "QuickBooks response exceeds the configured maxResponseBytes limit",
            ),
            status,
            b"",
            request_id,
        )
    if token and token.encode() in body:
        return Exchange(
            "invalid",
            _failure(
                operation, FailureKind.PROTOCOL, "QuickBooks response reflected the connection credential"
            ),
            status,
            b"",
            request_id,
        )
    return Exchange("succeeded", status=status, body=body, request_id=request_id)


async def _read_error_body(response: httpx.Response) -> bytes:
    """Read at most Go's 64 KiB error token prefix and close the response."""
    chunks: list[bytes] = []
    remaining = MAX_ERROR_BODY_BYTES
    try:
        async for chunk in response.aiter_bytes():
            if not remaining:
                break
            chunks.append(chunk[:remaining])
            remaining -= min(len(chunk), remaining)
        return b"".join(chunks)
    finally:
        await response.aclose()


def _resolve_realm(credentials: Credentials, configured: str) -> str:
    token = credentials.id_token.reveal()
    if not token:
        if configured:
            return configured
        raise ValueError(
            "QuickBooks company ID is unknown: authorize again while signed in to the company, "
            "or enter its company ID in realmId"
        )
    if len(token) > 16384:
        raise ValueError("QuickBooks ID token is too large")
    segments = token.split(".")
    if len(segments) != 3:
        raise ValueError("QuickBooks ID token is not a JWT")
    try:
        payload = base64.urlsafe_b64decode(segments[1].rstrip("=") + "===")
        claims = json.loads(payload)
    except (ValueError, UnicodeDecodeError, binascii.Error):
        raise ValueError("QuickBooks ID token payload is not a JSON object") from None
    if not isinstance(claims, dict):
        raise ValueError("QuickBooks ID token payload is not a JSON object")
    if claims.get("iss") != "https://oauth.platform.intuit.com/op/v1":
        raise ValueError("QuickBooks ID token was not issued by Intuit")
    audience = claims.get("aud")
    if not (
        audience == credentials.client_id or isinstance(audience, list) and credentials.client_id in audience
    ):
        raise ValueError("QuickBooks ID token was issued to another app")
    realm = claims.get("realmid", claims.get("realmId", ""))
    if not isinstance(realm, str):
        raise ValueError("QuickBooks ID token realmid is not a company ID")
    if not realm:
        if configured:
            return configured
        raise ValueError(
            "QuickBooks company ID is unknown: authorize again while signed in to the company, "
            "or enter its company ID in realmId"
        )
    if not _REALM.fullmatch(realm):
        raise ValueError("QuickBooks ID token realmid is not a company ID")
    if configured and configured != realm:
        raise ValueError(
            "the realmId setting names another company than the one Intuit authorized: "
            "clear it or authorize that company"
        )
    return realm


class Client:
    """Execute seven QuickBooks operations with call-time OAuth credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
        local_provider_url: str = "",
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("QuickBooks configuration and credential provider are required")
        if config.environment not in ("production", "sandbox") or config.max_response_bytes <= 0:
            raise ValueError("QuickBooks configuration is invalid")
        if config.realm_id.strip() and not _REALM.fullmatch(config.realm_id.strip()):
            raise ValueError("QuickBooks realmId must be a company ID")
        if local_provider_url:
            parsed = urlsplit(local_provider_url)
            try:
                is_loopback = parsed.hostname is not None and (
                    parsed.hostname.lower() == "localhost"
                    or ipaddress.ip_address(parsed.hostname).is_loopback
                )
            except ValueError:
                is_loopback = False
            if (
                parsed.scheme not in ("http", "https")
                or not is_loopback
                or parsed.path
                or parsed.query
                or parsed.fragment
                or parsed.username
            ):
                raise ValueError("QuickBooks local provider URL must be loopback without a path")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or httpx.AsyncClient(timeout=20, follow_redirects=False)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        host = (
            "sandbox-quickbooks.api.intuit.com"
            if config.environment == "sandbox"
            else "quickbooks.api.intuit.com"
        )
        self.endpoint = (
            local_provider_url.rstrip("/") if local_provider_url else f"https://{host}"
        ) + "/v3/company"

    async def aclose(self) -> None:
        """Close a connector-owned provider transport."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def find_customer(self) -> "FindCustomerOperation":
        """Return the findCustomer Query."""
        from .operations import FindCustomerOperation

        return FindCustomerOperation(self)

    def create_customer(self) -> "CreateCustomerOperation":
        """Return the createCustomer Mutation."""
        from .operations import CreateCustomerOperation

        return CreateCustomerOperation(self)

    def create_invoice(self) -> "CreateInvoiceOperation":
        """Return the createInvoice Mutation."""
        from .operations import CreateInvoiceOperation

        return CreateInvoiceOperation(self)

    def get_invoice(self) -> "GetInvoiceOperation":
        """Return the getInvoice Query."""
        from .operations import GetInvoiceOperation

        return GetInvoiceOperation(self)

    def list_invoices(self) -> "ListInvoicesOperation":
        """Return the listInvoices Query."""
        from .operations import ListInvoicesOperation

        return ListInvoicesOperation(self)

    def send_invoice(self) -> "SendInvoiceOperation":
        """Return the sendInvoice Mutation."""
        from .operations import SendInvoiceOperation

        return SendInvoiceOperation(self)

    def record_payment(self) -> "RecordPaymentOperation":
        """Return the recordPayment Mutation."""
        from .operations import RecordPaymentOperation

        return RecordPaymentOperation(self)

    async def exchange(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: bytes | None = None,
        mutation: bool = False,
    ) -> Exchange:
        """Resolve credentials and issue one bounded Accounting API request."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return Exchange(
                "rejected",
                _failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "QuickBooks authorization must be renewed: connect the company again",
                ),
            )
        except RefreshFailedError:
            return Exchange(
                "retry",
                _failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Intuit access token refresh is temporarily unavailable",
                ),
            )
        except Exception:
            return Exchange(
                "defect",
                _failure(
                    operation, FailureKind.AUTHENTICATION, "QuickBooks connection credentials are unavailable"
                ),
            )
        if not isinstance(credentials, Credentials) or not credentials.access_token.reveal():
            return Exchange(
                "defect",
                _failure(
                    operation, FailureKind.AUTHENTICATION, "QuickBooks connection credentials are unavailable"
                ),
            )
        try:
            realm = _resolve_realm(credentials, self.config.realm_id.strip())
        except ValueError as error:
            return Exchange("defect", _failure(operation, FailureKind.AUTHENTICATION, str(error)))
        parameters = dict(query or {})
        parameters["minorversion"] = "75"
        if mutation:
            if not str(call.idempotency_key) or len(str(call.idempotency_key)) > 50:
                return Exchange(
                    "defect",
                    _failure(
                        operation, FailureKind.LOCAL_DEFECT, "QuickBooks mutation has no usable requestid"
                    ),
                )
            parameters["requestid"] = str(call.idempotency_key)
        query_string = _encode_query(parameters)
        target = f"{self.endpoint}/{realm}{path}?{query_string}"
        headers = {
            "Accept": "application/json",
            "Authorization": f"Bearer {credentials.access_token.reveal()}",
        }
        if method == "POST":
            headers["Content-Type"] = (
                "application/json" if payload is not None else "application/octet-stream"
            )
        try:
            request = self.http_client.build_request(
                method,
                target,
                content=payload if payload is not None else (b"" if method == "POST" else None),
                headers=headers,
                timeout=20,
            )
            response, _ = await send_traced(self.http_client, request)
            if 200 <= response.status_code < 300:
                try:
                    body = await read_bounded_body(response, self.config.max_response_bytes)
                except BodyTooLargeError:
                    body = b"" * 0
                    return Exchange(
                        "invalid",
                        _failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "QuickBooks response exceeds the configured maxResponseBytes limit",
                        ),
                        response.status_code,
                        request_id=response.headers.get("intuit_tid", ""),
                        metadata={"realmId": realm, "environment": self.config.environment},
                    )
                except (httpx.HTTPError, OSError):
                    return Exchange(
                        "interrupted",
                        _failure(operation, FailureKind.TRANSPORT, "QuickBooks response was interrupted"),
                        response.status_code,
                        request_id=response.headers.get("intuit_tid", ""),
                        metadata={"realmId": realm, "environment": self.config.environment},
                    )
            else:
                body = await _read_error_body(response)
            result = _classify(
                operation,
                response.status_code,
                response.headers,
                body,
                self.config.max_response_bytes,
                credentials.access_token.reveal(),
                self.now(),
            )
            if response.status_code == 401 and hasattr(self.credentials, "resolve_after_rejection"):
                try:
                    replacement = await self.credentials.resolve_after_rejection(call)
                    has_same_realm = _resolve_realm(
                        replacement, self.config.realm_id.strip()
                    ) == realm and bool(replacement.access_token.reveal())
                except Exception:
                    has_same_realm = False
                if has_same_realm:
                    try:
                        replacement_headers = dict(headers)
                        replacement_headers["Authorization"] = f"Bearer {replacement.access_token.reveal()}"
                        replacement_request = self.http_client.build_request(
                            method,
                            target,
                            content=payload if payload is not None else (b"" if method == "POST" else None),
                            headers=replacement_headers,
                            timeout=20,
                        )
                        response, _ = await send_traced(self.http_client, replacement_request)
                        if 200 <= response.status_code < 300:
                            try:
                                body = await read_bounded_body(response, self.config.max_response_bytes)
                            except BodyTooLargeError:
                                result = Exchange(
                                    "invalid",
                                    _failure(
                                        operation,
                                        FailureKind.RESPONSE_TOO_LARGE,
                                        "QuickBooks response exceeds the configured maxResponseBytes limit",
                                    ),
                                    response.status_code,
                                    request_id=response.headers.get("intuit_tid", ""),
                                )
                            except (httpx.HTTPError, OSError):
                                result = Exchange(
                                    "interrupted",
                                    _failure(
                                        operation,
                                        FailureKind.TRANSPORT,
                                        "QuickBooks response was interrupted",
                                    ),
                                    response.status_code,
                                    request_id=response.headers.get("intuit_tid", ""),
                                )
                            else:
                                result = _classify(
                                    operation,
                                    response.status_code,
                                    response.headers,
                                    body,
                                    self.config.max_response_bytes,
                                    replacement.access_token.reveal(),
                                    self.now(),
                                )
                        else:
                            body = await _read_error_body(response)
                            result = _classify(
                                operation,
                                response.status_code,
                                response.headers,
                                body,
                                self.config.max_response_bytes,
                                replacement.access_token.reveal(),
                                self.now(),
                            )
                    except (httpx.HTTPError, OSError, ValueError):
                        result = Exchange(
                            "retry",
                            _failure(
                                operation,
                                FailureKind.TRANSPORT,
                                "QuickBooks request failed before a response arrived",
                            ),
                        )
        except (httpx.HTTPError, OSError, ValueError):
            result = Exchange(
                "retry",
                _failure(
                    operation, FailureKind.TRANSPORT, "QuickBooks request failed before a response arrived"
                ),
            )
        result.metadata.update({"realmId": realm, "environment": self.config.environment})
        return result

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Build a secret-safe correlation receipt."""
        return Receipt(
            call.id,
            "quickbooks",
            self.now(),
            call.idempotency_key,
            object_id,
            exchange.request_id,
            exchange.metadata,
        )
