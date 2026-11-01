# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cohere embedding and rerank calls with Go-compatible classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    RetryPolicy,
    SecretString,
    StepDefaults,
)
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

from .models import EmbedInput, EmbedOutput, RankedDocument, RerankInput, RerankOutput

CONNECTOR_ID = "cohere"
EMBED_OPERATION_ID = "embed"
RERANK_OPERATION_ID = "rerank"
EMBEDDED = "embedded"
RANKED = "ranked"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
INPUT_TYPES = ("search_document", "search_query", "classification", "clustering")
_DEFAULT_BASE_URL = "https://api.cohere.com"
_DEFAULT_TIMEOUT_SECONDS = 30
_DEFAULT_MAX_RESPONSE_BYTES = 4_194_304
# Billed calls: three attempts within five minutes, as the manifest explains.
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=45),
    2,
    execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(minutes=5)),
)
_DEFECT_BRANCH = BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True)
EMBED_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, EMBED_OPERATION_ID),
    (
        BranchDefinition(EMBEDDED, "Cohere returned one float vector per text."),
        BranchDefinition(PROVIDER_REJECTED, "Cohere conclusively rejected the embedding request.", True),
        BranchDefinition(INVALID_RESPONSE, "Cohere returned malformed or oversized embedding data.", True),
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)
RERANK_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, RERANK_OPERATION_ID),
    (
        BranchDefinition(RANKED, "Cohere returned ranked document indices and scores."),
        BranchDefinition(PROVIDER_REJECTED, "Cohere conclusively rejected the rerank request.", True),
        BranchDefinition(INVALID_RESPONSE, "Cohere returned malformed or oversized rerank data.", True),
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """Cohere HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Cohere API key, resolved anew for every provider attempt."""

    api_key: SecretString


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Cohere configuration is required")
        if credentials is None:
            raise ValueError("Cohere credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Cohere timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def embed(self) -> "EmbedOperation":
        """Bind the embedding Query to this client."""
        return EmbedOperation(self)

    def rerank(self) -> "RerankOperation":
        """Bind the rerank Query to this client."""
        return RerankOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def invoke_json(
        self,
        call: Call,
        operation: str,
        path: str,
        payload: dict[str, Any],
        empty: Output,
        success: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """POST one JSON request and classify it exactly like the Go invokeJSON helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        key = credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + path,
                content=json.dumps(payload).encode(),
                headers={
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                    "Authorization": "Bearer " + key,
                },
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _failed(operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "request cannot be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, "Cohere request failed", 0)
        try:
            status = response.status_code
            if status != 200:
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                if status == 429:
                    return _retry(operation, FailureKind.RATE_LIMIT, "Cohere rate limit", retry_after)
                if status in (408, 409) or status >= 500:
                    return _retry(
                        operation, FailureKind.AVAILABILITY, "Cohere temporarily unavailable", retry_after
                    )
                if status in (401, 498):
                    return _failed(
                        operation,
                        PROVIDER_REJECTED,
                        empty,
                        FailureKind.AUTHENTICATION,
                        "Cohere rejected the API key",
                    )
                if status == 403:
                    return _failed(
                        operation,
                        PROVIDER_REJECTED,
                        empty,
                        FailureKind.AUTHORIZATION,
                        "Cohere denied the request",
                    )
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"Cohere rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Cohere response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "Cohere response interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation, INVALID_RESPONSE, empty, FailureKind.PROTOCOL, "Cohere returned unusable JSON"
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class EmbedOperation:
    """Embed texts as float vectors; a repeat may be billed again."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Cohere client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and the billed-operation retry policy."""
        return EMBED_DEFINITION

    async def invoke(self, call: Call, input: EmbedInput) -> QueryAttempt[EmbedOutput]:
        """Validate, send once, then select a branch or limited retry."""
        operation = EMBED_OPERATION_ID
        if (
            not isinstance(input, EmbedInput)
            or not isinstance(input.model, str)
            or not input.model.strip()
            or not isinstance(input.texts, (tuple, list))
            or not 1 <= len(input.texts) <= 96
        ):
            return _failed(
                operation,
                DEFECT,
                EmbedOutput(),
                FailureKind.VALIDATION,
                "model and 1 to 96 texts are required",
            )
        if any(not isinstance(text, str) or not text.strip() for text in input.texts):
            return _failed(operation, DEFECT, EmbedOutput(), FailureKind.VALIDATION, "texts must be nonempty")
        if input.input_type not in INPUT_TYPES:
            return _failed(operation, DEFECT, EmbedOutput(), FailureKind.VALIDATION, "unsupported input_type")
        payload = {
            "model": input.model,
            "texts": list(input.texts),
            "input_type": input.input_type,
            "embedding_types": ["float"],
        }
        expected = len(input.texts)
        return await self.client.invoke_json(
            call,
            operation,
            "/v2/embed",
            payload,
            EmbedOutput(),
            EMBEDDED,
            lambda document: _decode_embed(document, expected),
        )


class RerankOperation:
    """Rank documents against a query; a repeat may be billed again."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Cohere client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and the billed-operation retry policy."""
        return RERANK_DEFINITION

    async def invoke(self, call: Call, input: RerankInput) -> QueryAttempt[RerankOutput]:
        """Validate, send once, then select a branch or limited retry."""
        operation = RERANK_OPERATION_ID
        documents = input.documents if isinstance(input, RerankInput) else ()
        if (
            not isinstance(input, RerankInput)
            or not isinstance(input.model, str)
            or not isinstance(input.query, str)
            or not input.model.strip()
            or not input.query.strip()
            or not isinstance(documents, (tuple, list))
            or not 1 <= len(documents) <= 1000
            or type(input.top_n) is not int
            or not 0 <= input.top_n <= len(documents)
        ):
            return _failed(
                operation,
                DEFECT,
                RerankOutput(),
                FailureKind.VALIDATION,
                "model, query, 1 to 1000 documents, and valid topN are required",
            )
        if any(not isinstance(document, str) or not document.strip() for document in documents):
            return _failed(
                operation, DEFECT, RerankOutput(), FailureKind.VALIDATION, "documents must be nonempty"
            )
        payload: dict[str, Any] = {"model": input.model, "query": input.query, "documents": list(documents)}
        if input.top_n:
            payload["top_n"] = input.top_n
        count = len(documents)
        return await self.client.invoke_json(
            call,
            operation,
            "/v2/rerank",
            payload,
            RerankOutput(),
            RANKED,
            lambda document: _decode_rerank(document, count),
        )


def _decode_embed(document: Any, expected: int) -> EmbedOutput:
    document = _object(document)
    embedding_id = document.get("id")
    vectors = _object(document.get("embeddings") or {}).get("float")
    if (
        not isinstance(embedding_id, str)
        or not embedding_id
        or not isinstance(vectors, list)
        or len(vectors) != expected
    ):
        raise ValueError
    decoded = []
    for vector in vectors:
        if (
            not isinstance(vector, list)
            or not vector
            or any(type(value) not in (int, float) for value in vector)
        ):
            raise ValueError
        decoded.append(tuple(float(value) for value in vector))
    return EmbedOutput(embedding_id, tuple(decoded))


def _decode_rerank(document: Any, count: int) -> RerankOutput:
    results = _object(document).get("results")
    if not isinstance(results, list):
        raise ValueError
    seen: set[int] = set()
    ranked = []
    for result in map(_object, results):
        index = result.get("index", 0)
        score = result.get("relevance_score", 0)
        if (
            type(index) is not int
            or type(score) not in (int, float)
            or not 0 <= index < count
            or index in seen
        ):
            raise ValueError
        seen.add(index)
        ranked.append(RankedDocument(index, float(score)))
    return RerankOutput(tuple(ranked))


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value
