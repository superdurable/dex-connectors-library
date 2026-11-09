# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""MongoDB findDocuments decisions matching the Go connector contract."""

import asyncio
import json
import re
from typing import Any

from bson import json_util
from bson.errors import InvalidBSON
from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt
from pymongo.errors import ConfigurationError, ConnectionFailure, InvalidOperation, OperationFailure

from ._generated.definitions import FIND_DOCUMENTS_DEFINITION
from .client import Client
from .models import DocumentSet, FindDocumentsInput

_COLLECTION_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_.-]*\Z")
_JAVASCRIPT_OPERATORS = frozenset(("$where", "$function", "$accumulator"))


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "mongodb", "findDocuments", message)


def _branch(
    branch: str, kind: FailureKind, message: str, output: DocumentSet | None = None
) -> QueryAttempt[DocumentSet]:
    return QueryAttempt.branch_result(branch, output or DocumentSet(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str) -> QueryAttempt[DocumentSet]:
    return QueryAttempt.retry(_failure(kind, message))


def _has_javascript_operator(value: object) -> bool:
    if isinstance(value, dict):
        return any(
            key in _JAVASCRIPT_OPERATORS or _has_javascript_operator(child)
            for key, child in value.items()
        )
    if isinstance(value, (list, tuple)):
        return any(_has_javascript_operator(child) for child in value)
    return False


def _valid_connection_uri(uri: str) -> bool:
    return (
        len(uri) <= 4096
        and not any(character in uri for character in "\x00\r\n")
        and uri.startswith(("mongodb://", "mongodb+srv://"))
    )


class FindDocumentsOperation:
    """Read a bounded set of documents with a native asynchronous driver."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and branches."""
        return FIND_DOCUMENTS_DEFINITION

    async def invoke(self, call: Call, input: FindDocumentsInput) -> QueryAttempt[DocumentSet]:
        """Resolve the current URI, read once, and classify the definite outcome."""
        if (
            not _COLLECTION_NAME.fullmatch(input.collection)
            or (input.filter is not None and not isinstance(input.filter, dict))
            or _has_javascript_operator(input.filter)
        ):
            return _branch("defect", FailureKind.VALIDATION, "MongoDB collection or filter is invalid")
        try:
            filter_json = json.dumps(
                input.filter if input.filter is not None else {}, separators=(",", ":"), allow_nan=False
            )
            if len(filter_json.encode("utf-8")) > 64 << 10:
                raise ValueError("filter too large")
            decoded_filter = json_util.loads(filter_json)
            if not isinstance(decoded_filter, dict):
                raise ValueError("filter must be a JSON object")
        except (TypeError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "MongoDB filter Extended JSON is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except Exception:
            return _retry(FailureKind.TRANSPORT, "MongoDB credentials are unavailable")
        uri = credentials.connection_uri.reveal()
        if not _valid_connection_uri(uri):
            return _branch("defect", FailureKind.AUTHENTICATION, "MongoDB connection URI is invalid")
        try:
            async with asyncio.timeout(self.client.config.timeout_seconds):
                async with self.client.create_driver(
                    uri,
                    serverSelectionTimeoutMS=int(self.client.config.timeout_seconds * 1000),
                    connectTimeoutMS=int(self.client.config.timeout_seconds * 1000),
                ) as provider_client:
                    cursor = provider_client[self.client.config.database][input.collection].find(
                        decoded_filter, limit=self.client.config.max_documents + 1
                    )
                    documents = await cursor.to_list(length=self.client.config.max_documents + 1)
            result: list[dict[str, Any]] = []
            encoded_bytes = 0
            for document in documents:
                if len(result) >= self.client.config.max_documents:
                    return _branch(
                        "truncated", FailureKind.RESPONSE_TOO_LARGE,
                        "MongoDB document count limit was reached", DocumentSet(tuple(result)),
                    )
                try:
                    encoded = json_util.dumps(document, json_options=json_util.CANONICAL_JSON_OPTIONS)
                    decoded = json.loads(encoded)
                except (TypeError, ValueError):
                    return _branch(
                        "invalidResponse", FailureKind.PROTOCOL, "MongoDB document encoding failed"
                    )
                if uri in encoded:
                    return _branch(
                        "defect", FailureKind.AUTHENTICATION, "MongoDB response contained a credential"
                    )
                encoded_bytes += len(json.dumps(decoded, separators=(",", ":")).encode("utf-8"))
                if encoded_bytes > self.client.config.max_response_bytes:
                    return _branch(
                        "truncated", FailureKind.RESPONSE_TOO_LARGE,
                        "MongoDB response byte limit was reached", DocumentSet(tuple(result)),
                    )
                if not isinstance(decoded, dict):
                    return _branch(
                        "invalidResponse", FailureKind.PROTOCOL, "MongoDB document JSON was invalid"
                    )
                result.append(decoded)
            return QueryAttempt.branch_result("found", DocumentSet(tuple(result)), Receipt())
        except (TimeoutError, ConnectionFailure):
            return _retry(FailureKind.TRANSPORT, "MongoDB read was interrupted")
        except OperationFailure as error:
            if error.has_error_label("RetryableReadError") or error.code == 50:
                return _retry(FailureKind.AVAILABILITY, "MongoDB read is temporarily unavailable")
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "MongoDB rejected the find command"
            )
        except (ConfigurationError, InvalidOperation, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "MongoDB connection URI was rejected")
        except InvalidBSON:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "MongoDB document encoding failed")


__all__ = ["FindDocumentsOperation"]
