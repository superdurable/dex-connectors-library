# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sign KuaidiBird real-time tracking queries and return safe parcel history."""

import base64
import hashlib
import json
import re
from datetime import UTC, datetime
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
    parse_retry_after,
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
        branch, QueryTrackingOutput(), Receipt(), Failure(kind, "kuaidibird", "queryTracking", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[QueryTrackingOutput]:
    return QueryAttempt.retry(Failure(kind, "kuaidibird", "queryTracking", message), delay)


class Client:
    """Resolve current KuaidiBird signing credentials for each parcel read."""

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
            raise ValueError("KuaidiBird configuration and credentials are required")
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
            or re.fullmatch(r"[A-Z][A-Z0-9]{1,19}", input.carrier_code) is None
            or re.fullmatch(r"[A-Za-z0-9_-]{6,30}", input.tracking_number) is None
        ):
            return _branch("defect", FailureKind.VALIDATION, "tracking input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            business_id = credentials.business_id.reveal()
            api_key = credentials.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "KuaidiBird credentials are unavailable")
        if (
            re.fullmatch(r"[0-9]{1,20}", business_id) is None
            or not api_key
            or len(api_key) > 512
            or api_key.strip() != api_key
            or any(char in "\r\n\t " for char in api_key)
        ):
            return _branch("defect", FailureKind.AUTHENTICATION, "KuaidiBird credentials are invalid")
        request_data = json.dumps(
            {"ShipperCode": input.carrier_code, "LogisticCode": input.tracking_number}, separators=(",", ":")
        )
        digest = hashlib.md5((request_data + api_key).encode()).hexdigest()
        signature = base64.b64encode(digest.encode()).decode()
        form = urlencode(
            {
                "RequestData": request_data,
                "EBusinessID": business_id,
                "RequestType": "1002",
                "DataSign": signature,
                "DataType": "2",
            }
        )
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/Ebusiness/EbusinessOrderHandle.aspx",
                content=form,
                headers={
                    "Content-Type": "application/x-www-form-urlencoded;charset=utf-8",
                    "Accept": "application/json",
                },
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "KuaidiBird request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "KuaidiBird rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "KuaidiBird is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "KuaidiBird response is oversized"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "KuaidiBird response was interrupted")
            try:
                wire = json.loads(body)
                if not isinstance(wire, dict) or type(wire.get("Success")) is not bool:
                    raise ValueError("invalid tracking payload")
            except (TypeError, ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "KuaidiBird returned invalid JSON")
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"KuaidiBird rejected HTTP {status}"
                )
            if not wire["Success"]:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "KuaidiBird could not return this waybill",
                )
            try:
                state = wire.get("State")
                if type(state) is int or type(state) is float:
                    state = str(state)
                if (
                    not isinstance(state, str)
                    or not state
                    or wire.get("EBusinessID") != business_id
                    or wire.get("ShipperCode") != input.carrier_code
                    or wire.get("LogisticCode") != input.tracking_number
                    or not isinstance(wire.get("Traces"), list)
                ):
                    raise ValueError("incomplete tracking data")
                events = tuple(
                    TrackingEvent(item["AcceptTime"], item["AcceptStation"]) for item in wire["Traces"]
                )
                if any(not event.time or not event.content for event in events):
                    raise ValueError("incomplete tracking event")
            except (KeyError, TypeError, ValueError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "KuaidiBird returned invalid tracking data"
                )
            return QueryAttempt.branch_result(
                "retrieved",
                QueryTrackingOutput(input.carrier_code, input.tracking_number, state, events),
                Receipt(),
            )
        finally:
            await response.aclose()
