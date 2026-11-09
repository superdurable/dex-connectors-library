# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import EmailIdentityDetails, GetEmailIdentityInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_EMAIL_IDENTITY_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_email_identity_found(value: EmailIdentityDetails, failure: Failure | None = None) -> Case[EmailIdentityDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_email_identity_found, '__module__', "dex_connectors.amazon_ses.sesmock")

def get_email_identity_missing(value: EmailIdentityDetails, failure: Failure | None = None) -> Case[EmailIdentityDetails]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_email_identity_missing, '__module__', "dex_connectors.amazon_ses.sesmock")

def get_email_identity_provider_rejected(value: EmailIdentityDetails, failure: Failure | None = None) -> Case[EmailIdentityDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_email_identity_provider_rejected, '__module__', "dex_connectors.amazon_ses.sesmock")

def get_email_identity_invalid_response(value: EmailIdentityDetails, failure: Failure | None = None) -> Case[EmailIdentityDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_email_identity_invalid_response, '__module__', "dex_connectors.amazon_ses.sesmock")

def get_email_identity_defect(value: EmailIdentityDetails, failure: Failure | None = None) -> Case[EmailIdentityDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_email_identity_defect, '__module__', "dex_connectors.amazon_ses.sesmock")

def get_email_identity_retry(failure: Failure, after_seconds: float = 0) -> Case[EmailIdentityDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_email_identity_retry, '__module__', "dex_connectors.amazon_ses.sesmock")

def get_email_identity_found_default() -> Case[EmailIdentityDetails]:
    return Case.branch_result(FOUND, EmailIdentityDetails(name='example.com', verification_status='SUCCESS'))

setattr(get_email_identity_found_default, '__module__', "dex_connectors.amazon_ses.sesmock")


class Mock:

    def __init__(self, connection_name: str = "amazon-ses") -> None:
        self.get_email_identity_script: Query[GetEmailIdentityInput, EmailIdentityDetails] = Query(GET_EMAIL_IDENTITY_DEFINITION)
        self.get_email_identity_script.default(get_email_identity_found_default())
        self._connection = new_connection_with_operations(
            get_email_identity=self.get_email_identity_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amazon_ses.sesmock")

__all__ = [
    "Mock",
    "get_email_identity_found",
    "get_email_identity_missing",
    "get_email_identity_provider_rejected",
    "get_email_identity_invalid_response",
    "get_email_identity_defect",
    "get_email_identity_retry",
    "get_email_identity_found_default",
]
