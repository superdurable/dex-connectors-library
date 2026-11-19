# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sign Kuaidi100 real-time tracking queries and return safe parcel history."""

import hashlib
import json
import re
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import QUERY_TRACKING_DEFINITION
from .models import QueryTrackingInput, QueryTrackingOutput, TrackingEvent


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[QueryTrackingOutput]:
    return QueryAttempt.branch_result(
        branch, QueryTrackingOutput(), Receipt(), Failure(kind, "kuaidi100", "queryTracking", message)
    )


def _retry(kind: FailureKind, message: str) -> QueryAttempt[QueryTrackingOutput]:
    return QueryAttempt.retry(Failure(kind, "kuaidi100", "queryTracking", message), 1800)


class Client:
    """Resolve current Kuaidi100 signing credentials for each parcel read."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Kuaidi100 configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def query_tracking(self) -> "QueryTrackingOperation":
        """Bind the real-time parcel Query."""
        return QueryTrackingOperation(self)

    async def aclose(self) -> None:
        """Close an HTTP transport owned by this client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class QueryTrackingOperation:
    """Read tracking state and event history for one waybill."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated tracking Query contract."""
        return QUERY_TRACKING_DEFINITION

    async def invoke(self, call: Call, input: QueryTrackingInput) -> QueryAttempt[QueryTrackingOutput]:
        """Sign one request and classify its provider outcome."""
        if (
            not isinstance(input, QueryTrackingInput)
            or re.fullmatch(r"[a-z][a-z0-9_]{0,31}", input.carrier_code) is None
            or re.fullmatch(r"[A-Za-z0-9_-]{6,32}", input.tracking_number) is None
            or (input.phone and re.fullmatch(r"[0-9+*#-]{4,32}", input.phone) is None)
        ):
            return _branch("defect", FailureKind.VALIDATION, "tracking input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            customer = credentials.customer.reveal()
            key = credentials.key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Kuaidi100 credentials are unavailable")
        if any(
            not value
            or len(value) > 512
            or value.strip() != value
            or any(char in "\r\n\t " for char in value)
            for value in (customer, key)
        ):
            return _branch("defect", FailureKind.AUTHENTICATION, "Kuaidi100 credentials are invalid")
        parameters = {"com": input.carrier_code, "num": input.tracking_number}
        if input.phone:
            parameters["phone"] = input.phone
        parameters.update({"show": "0", "order": "desc"})
        parameter_json = json.dumps(parameters, ensure_ascii=False, separators=(",", ":"))
        signature = hashlib.md5((parameter_json + key + customer).encode()).hexdigest().upper()
        form = urlencode({"customer": customer, "param": parameter_json, "sign": signature})
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/poll/query.do",
                content=form,
                headers={"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Kuaidi100 request failed")
        try:
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Kuaidi100 rate limited the request")
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Kuaidi100 is unavailable")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Kuaidi100 response is oversized"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Kuaidi100 response was interrupted")
            try:
                wire = json.loads(body)
                if not isinstance(wire, dict):
                    raise ValueError("invalid tracking payload")
            except (TypeError, ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Kuaidi100 returned invalid JSON")
            if wire.get("result") is False:
                code = wire.get("returnCode")
                if code in ("501", "502"):
                    return _retry(FailureKind.AVAILABILITY, "Kuaidi100 is unavailable")
                kind = (
                    FailureKind.RATE_LIMIT
                    if code == "504"
                    else FailureKind.AUTHENTICATION
                    if code in ("503", "601")
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, "Kuaidi100 rejected the tracking query")
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"Kuaidi100 rejected HTTP {status}"
                )
            try:
                events_wire = wire.get("data")
                if (
                    wire.get("status") != "200"
                    or wire.get("com") != input.carrier_code
                    or wire.get("nu") != input.tracking_number
                    or not wire.get("state")
                    or not isinstance(events_wire, list)
                ):
                    raise ValueError("incomplete tracking data")
                events = tuple(TrackingEvent(item["time"], item["context"]) for item in events_wire)
                if any(not event.time or not event.content for event in events):
                    raise ValueError("incomplete tracking event")
            except (KeyError, TypeError, ValueError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Kuaidi100 returned invalid tracking data"
                )
            return QueryAttempt.branch_result(
                "retrieved",
                QueryTrackingOutput(input.carrier_code, input.tracking_number, wire["state"], events),
                Receipt(),
            )
        finally:
            await response.aclose()
