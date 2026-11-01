# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed OpenAI scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import (
    COMPLETED,
    CONNECTOR_ID,
    CREATE_RESPONSE_DEFINITION,
    FAILED,
    FOUND,
    NOT_FOUND,
    PROVIDER_REJECTED,
    RETRIEVE_RESPONSE_DEFINITION,
)
from ..connection import Connection, new_connection_with_operations
from ..models import CreateRequest, Response, RetrieveRequest, Usage

_STORED_SUMMARY = Response(
    "resp_mocksummary001", "gpt-6-sol", "completed", "The connector shipped today.", Usage(30, 0, 7, 0, 37)
)


def create_response_completed(value: Response, failure: Failure | None = None) -> Case[Response]:
    """Script the completed route; structured requests need JSON output_text."""
    return Case.branch_result(COMPLETED, value, failure)


def create_response_failed(value: Response, failure: Failure | None = None) -> Case[Response]:
    """Script a failed or incomplete stored response."""
    return Case.branch_result(FAILED, value, failure)


def create_response_provider_rejected(value: Response, failure: Failure | None = None) -> Case[Response]:
    """Script a definite rejection of the create request."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


def create_response_uncertain(value: Response, failure: Failure) -> Case[Response]:
    """Script a create whose outcome cannot be confirmed."""
    return Case.uncertain(value, failure)


def create_response_stored_summary() -> Case[Response]:
    """Return the manifest's default storedSummary answer."""
    return create_response_completed(_STORED_SUMMARY)


def create_response_incomplete_summary() -> Case[Response]:
    """Return the manifest's incompleteSummary answer."""
    return create_response_failed(
        Response("resp_mocksummary002", "gpt-6-sol", "incomplete", "The connector", Usage(30, 0, 3, 0, 33)),
        Failure(
            FailureKind.PROVIDER_REJECTION,
            CONNECTOR_ID,
            "createResponse",
            "OpenAI did not complete the response.",
        ),
    )


def create_response_create_rejected() -> Case[Response]:
    """Return the manifest's createRejected answer."""
    return create_response_provider_rejected(
        Response(),
        Failure(
            FailureKind.AUTHORIZATION, CONNECTOR_ID, "createResponse", "OpenAI rejected the response request."
        ),
    )


def retrieve_response_found(value: Response, failure: Failure | None = None) -> Case[Response]:
    """Script the found route."""
    return Case.branch_result(FOUND, value, failure)


def retrieve_response_stored_response() -> Case[Response]:
    """Return the manifest's default storedResponse answer."""
    return retrieve_response_found(_STORED_SUMMARY)


def retrieve_response_not_found() -> Case[Response]:
    """Return the manifest's responseNotFound answer."""
    return Case.branch_result(
        NOT_FOUND,
        Response(),
        Failure(
            FailureKind.NOT_FOUND, CONNECTOR_ID, "retrieveResponse", "The stored response was not found."
        ),
    )


def retrieve_response_rejected() -> Case[Response]:
    """Return the manifest's retrieveRejected answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        Response(),
        Failure(
            FailureKind.AUTHORIZATION,
            CONNECTOR_ID,
            "retrieveResponse",
            "OpenAI rejected the response lookup.",
        ),
    )


class Mock:
    """Script OpenAI operations, defaulting to the manifest's default answers."""

    def __init__(self, connection_name: str = "openai") -> None:
        self.create_response_script: Mutation[CreateRequest, Response] = Mutation(CREATE_RESPONSE_DEFINITION)
        self.create_response_script.default(create_response_stored_summary())
        self.retrieve_response_script: Query[RetrieveRequest, Response] = Query(RETRIEVE_RESPONSE_DEFINITION)
        self.retrieve_response_script.default(retrieve_response_stored_response())
        self._connection = new_connection_with_operations(
            self.create_response_script,
            self.retrieve_response_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "create_response_completed",
    "create_response_failed",
    "create_response_provider_rejected",
    "create_response_uncertain",
    "create_response_stored_summary",
    "create_response_incomplete_summary",
    "create_response_create_rejected",
    "retrieve_response_found",
    "retrieve_response_stored_response",
    "retrieve_response_not_found",
    "retrieve_response_rejected",
]
