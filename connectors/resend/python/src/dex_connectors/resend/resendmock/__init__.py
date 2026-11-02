# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Manifest-backed Resend scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_EMAIL_DEFINITION,
    NOT_FOUND,
    PROVIDER_REJECTED,
    SEND_EMAIL_DEFINITION,
    SENT,
)
from ..connection import Connection, new_connection_with_operations
from ..models import Email, GetEmailInput, SendEmailInput, SentEmail


def send_email_sent(value: SentEmail, failure: Failure | None = None) -> Case[SentEmail]:
    """Script the successful sent route."""
    return Case.branch_result(SENT, value, failure)


def send_email_provider_rejected(value: SentEmail, failure: Failure | None = None) -> Case[SentEmail]:
    """Script a definite provider rejection of the send."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


def send_email_retry(failure: Failure, after_seconds: float = 0) -> Case[SentEmail]:
    """Request another Dex Execute attempt for the send."""
    return Case.retry(failure, after_seconds)


def send_email_default() -> Case[SentEmail]:
    """Return the manifest's sendEmailDefault answer."""
    return send_email_sent(SentEmail("5f9f777d-04e0-482a-bb65-088bb5bd44a4"))


def get_email_found(value: Email, failure: Failure | None = None) -> Case[Email]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_email_not_found(value: Email, failure: Failure | None = None) -> Case[Email]:
    """Script a missing email."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_email_default() -> Case[Email]:
    """Return the manifest's getEmailDefault answer."""
    return get_email_found(
        Email(
            "5f9f777d-04e0-482a-bb65-088bb5bd44a4",
            "Acme <hello@example.test>",
            ("reader@example.test",),
            "Welcome",
            "2026-10-26T12:00:00Z",
        )
    )


class Mock:
    """Script Resend operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "resend") -> None:
        self.send_email_script: Mutation[SendEmailInput, SentEmail] = Mutation(SEND_EMAIL_DEFINITION)
        self.send_email_script.default(send_email_default())
        self.get_email_script: Query[GetEmailInput, Email] = Query(GET_EMAIL_DEFINITION)
        self.get_email_script.default(get_email_default())
        self._connection = new_connection_with_operations(
            self.send_email_script, self.get_email_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "send_email_sent",
    "send_email_provider_rejected",
    "send_email_retry",
    "send_email_default",
    "get_email_found",
    "get_email_not_found",
    "get_email_default",
]
