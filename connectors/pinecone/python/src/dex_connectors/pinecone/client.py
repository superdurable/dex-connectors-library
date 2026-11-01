# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Pinecone vector upserts and queries with Go-compatible branch classification."""

import json
import math
import struct
from dataclasses import dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from decimal import Decimal
from typing import Any, TypeVar

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
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
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    QUERIED as QUERIED,
)
from ._generated.definitions import (
    QUERY_VECTORS_DEFINITION as QUERY_VECTORS_DEFINITION,
)
from ._generated.definitions import (
    QUERY_VECTORS_OPERATION_ID as QUERY_VECTORS_OPERATION_ID,
)
from ._generated.definitions import (
    UPSERT_VECTORS_DEFINITION as UPSERT_VECTORS_DEFINITION,
)
from ._generated.definitions import (
    UPSERT_VECTORS_OPERATION_ID as UPSERT_VECTORS_OPERATION_ID,
)
from ._generated.definitions import (
    UPSERTED as UPSERTED,
)
from .models import QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput, Vector

API_VERSION = "2025-04"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_UPSERT_VECTORS = 1000
_MAX_TOP_K = 10000
_FLOAT32_FIXED_MINIMUM = struct.unpack("<f", struct.pack("<f", 1e-6))[0]
_FLOAT32_FIXED_LIMIT = struct.unpack("<f", struct.pack("<f", 1e21))[0]
_STATUS_KINDS = {
    401: FailureKind.AUTHENTICATION,
    403: FailureKind.AUTHORIZATION,
    400: FailureKind.VALIDATION,
    422: FailureKind.VALIDATION,
}

Output = TypeVar("Output")






@dataclass(frozen=True, slots=True)
class _Outcome:
    """A terminal or retry attempt whose empty value the calling operation supplies."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def as_query(self, empty: Output) -> QueryAttempt[Output]:
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)

    def as_mutation(self, empty: Output) -> MutationAttempt[Output]:
        if self.is_retry:
            return MutationAttempt.retry(self.failure, self.retry_after_seconds)
        return MutationAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


class Client:
    """One redirect-safe HTTP client bound to one index host, plus a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("pinecone configuration is required")
        if credentials is None:
            raise ValueError("pinecone credential source is required")
        if not config.base_url:
            raise ValueError("configuration baseUrl is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("pinecone timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def upsert_vectors(self) -> "UpsertVectorsOperation":
        """Bind the upsertVectors Mutation to this client."""
        return UpsertVectorsOperation(self)

    def query_vectors(self) -> "QueryVectorsOperation":
        """Bind the queryVectors Query to this client."""
        return QueryVectorsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def send(self, call: Call, operation: str, path: str, body: bytes) -> Any:
        """POST one JSON body and classify it exactly like the Go send helper.

        Returns the decoded JSON document on success, or an _Outcome.
        """
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _outcome(
                DEFECT, operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.api_key.reveal()
        if not token or not is_header_safe_credential(token):
            return _outcome(DEFECT, operation, FailureKind.AUTHENTICATION, "connection credential is invalid")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + path,
                content=body,
                headers={
                    "Accept": "application/json",
                    "User-Agent": "superdurable-pinecone/0.1",
                    "Api-Key": token,
                    "X-Pinecone-Api-Version": API_VERSION,
                    "Content-Type": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _outcome(DEFECT, operation, FailureKind.LOCAL_DEFECT, "request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _outcome("", operation, FailureKind.AVAILABILITY, "provider request failed", is_retry=True)
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _outcome("", operation, kind, "provider is temporarily unavailable", True, retry_after)
            if status != 200:
                kind = _STATUS_KINDS.get(status, FailureKind.PROVIDER_REJECTION)
                return _outcome(
                    PROVIDER_REJECTED, operation, kind, f"provider rejected the request with HTTP {status}"
                )
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _outcome(
                    INVALID_RESPONSE,
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _outcome(
                    "",
                    operation,
                    FailureKind.AVAILABILITY,
                    "provider response was interrupted",
                    is_retry=True,
                )
            try:
                return json.loads(response_body)
            except (ValueError, UnicodeDecodeError):
                return _outcome(
                    INVALID_RESPONSE, operation, FailureKind.PROTOCOL, "provider returned malformed JSON"
                )
        finally:
            await response.aclose()


class UpsertVectorsOperation:
    """Upsert vector IDs by absolute replacement in one namespace."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Pinecone client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return UPSERT_VECTORS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertVectorsInput) -> IdempotencyKey:
        """Use the stable Dex call identity; the provider write is an upsert by stable vector ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpsertVectorsInput) -> MutationAttempt[UpsertVectorsOutput]:
        """Validate, upsert once, then select a branch or safe retry."""
        operation = UPSERT_VECTORS_OPERATION_ID
        body = _encode_upsert_vectors_input(input)
        if isinstance(body, str):
            return _mutation_failed(DEFECT, FailureKind.VALIDATION, body)
        response = await self.client.send(call, operation, "/vectors/upsert", body)
        if isinstance(response, _Outcome):
            return response.as_mutation(UpsertVectorsOutput())
        if not has_response_shape(response, "upsertedCount"):
            return _mutation_failed(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "provider returned an incomplete response"
            )
        count = response["upsertedCount"]
        if type(count) is not int or not 0 <= count <= len(input.vectors):
            return _mutation_failed(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "provider returned an invalid count"
            )
        return MutationAttempt.branch_result(UPSERTED, UpsertVectorsOutput(count), Receipt())


class QueryVectorsOperation:
    """Find nearest vectors in one namespace."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Pinecone client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return QUERY_VECTORS_DEFINITION

    async def invoke(self, call: Call, input: QueryVectorsInput) -> QueryAttempt[QueryVectorsOutput]:
        """Validate, query once, then select a branch or safe retry."""
        operation = QUERY_VECTORS_OPERATION_ID
        body = _encode_query_vectors_input(input)
        if isinstance(body, str):
            return _query_failed(DEFECT, FailureKind.VALIDATION, body)
        response = await self.client.send(call, operation, "/query", body)
        if isinstance(response, _Outcome):
            return response.as_query(QueryVectorsOutput())
        if not has_response_shape(response, "matches"):
            return _query_failed(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "provider returned an incomplete response"
            )
        return QueryAttempt.branch_result(QUERIED, QueryVectorsOutput(response), Receipt())


def has_response_shape(document: Any, key: str) -> bool:
    """Apply the Go hasResponseShape check to a decoded JSON document."""
    if not isinstance(document, dict) or document.get(key) is None:
        return False
    if key == "matches":
        return isinstance(document[key], list)
    return True


def _encode_upsert_vectors_input(input: UpsertVectorsInput) -> bytes | str:
    if not isinstance(input, UpsertVectorsInput) or not 1 <= len(input.vectors) <= _MAX_UPSERT_VECTORS:
        return "vectors must contain 1 through 1000 items"
    seen_ids: set[str] = set()
    for vector in input.vectors:
        if (
            not isinstance(vector, Vector)
            or not isinstance(vector.id, str)
            or not vector.id
            or not vector.values
        ):
            return "each vector needs an ID and values"
        if vector.id in seen_ids:
            return "vector IDs must be unique within the batch"
        seen_ids.add(vector.id)
    try:
        vectors = ",".join(_encode_vector(vector) for vector in input.vectors)
        namespace = f'"namespace":{_dump_json(input.namespace)},' if input.namespace else ""
    except (TypeError, ValueError, OverflowError):
        return "vectors must contain finite 32-bit floating-point values"
    return f'{{{namespace}"vectors":[{vectors}]}}'.encode()


def _encode_query_vectors_input(input: QueryVectorsInput) -> bytes | str:
    if (
        not isinstance(input, QueryVectorsInput)
        or not input.vector
        or type(input.top_k) is not int
        or not 1 <= input.top_k <= _MAX_TOP_K
    ):
        return "vector and topK from 1 through 10000 are required"
    try:
        namespace = f'"namespace":{_dump_json(input.namespace)},' if input.namespace else ""
        vector = _encode_float32_list(input.vector)
    except (TypeError, ValueError, OverflowError):
        return "vector must contain finite 32-bit floating-point values"
    include_metadata = ',"includeMetadata":true' if input.include_metadata else ""
    return f'{{{namespace}"vector":{vector},"topK":{input.top_k}{include_metadata}}}'.encode()


def _encode_vector(vector: Vector) -> str:
    metadata = f',"metadata":{_dump_json(dict(vector.metadata))}' if vector.metadata else ""
    return f'{{"id":{_dump_json(vector.id)},"values":{_encode_float32_list(vector.values)}{metadata}}}'


def _encode_float32_list(values: tuple[float, ...]) -> str:
    return "[" + ",".join(_format_float32(value) for value in values) + "]"


def _format_float32(value: float) -> str:
    """Round to float32 and write it as Go encoding/json writes a float32."""
    if type(value) not in (int, float) or not math.isfinite(value):
        raise ValueError
    rounded = _to_float32(value)
    if not math.isfinite(rounded):
        raise ValueError
    if rounded == 0:
        return "-0" if math.copysign(1, rounded) < 0 else "0"
    for precision in range(9):
        scientific = f"{rounded:.{precision}e}"
        if _to_float32(float(scientific)) == rounded:
            break
    if abs(rounded) < _FLOAT32_FIXED_MINIMUM or abs(rounded) >= _FLOAT32_FIXED_LIMIT:
        mantissa, _, exponent = scientific.partition("e")
        power = int(exponent)
        return f"{mantissa}e-{-power}" if power < 0 else f"{mantissa}e+{power:02d}"
    fixed = format(Decimal(scientific), "f")
    return fixed.rstrip("0").rstrip(".") if "." in fixed else fixed


def _to_float32(value: float) -> float:
    return float(struct.unpack("<f", struct.pack("<f", value))[0])


def _dump_json(value: object) -> str:
    return json.dumps(value, separators=(",", ":"))


def _outcome(
    branch: str,
    operation: str,
    kind: FailureKind,
    message: str,
    is_retry: bool = False,
    retry_after_seconds: float = 0,
) -> _Outcome:
    return _Outcome(branch, Failure(kind, CONNECTOR_ID, operation, message), is_retry, retry_after_seconds)


def _query_failed(branch: str, kind: FailureKind, message: str) -> QueryAttempt[QueryVectorsOutput]:
    return QueryAttempt.branch_result(
        branch,
        QueryVectorsOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, QUERY_VECTORS_OPERATION_ID, message),
    )


def _mutation_failed(branch: str, kind: FailureKind, message: str) -> MutationAttempt[UpsertVectorsOutput]:
    return MutationAttempt.branch_result(
        branch,
        UpsertVectorsOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, UPSERT_VECTORS_OPERATION_ID, message),
    )
