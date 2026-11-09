# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ContactSummary, GetContactInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CONTACT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_contact_found(value: ContactSummary, failure: Failure | None = None) -> Case[ContactSummary]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_contact_found, '__module__', "dex_connectors.creatio.creatiomock")

def get_contact_not_found(value: ContactSummary, failure: Failure | None = None) -> Case[ContactSummary]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_contact_not_found, '__module__', "dex_connectors.creatio.creatiomock")

def get_contact_provider_rejected(value: ContactSummary, failure: Failure | None = None) -> Case[ContactSummary]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_contact_provider_rejected, '__module__', "dex_connectors.creatio.creatiomock")

def get_contact_invalid_response(value: ContactSummary, failure: Failure | None = None) -> Case[ContactSummary]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_contact_invalid_response, '__module__', "dex_connectors.creatio.creatiomock")

def get_contact_defect(value: ContactSummary, failure: Failure | None = None) -> Case[ContactSummary]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_contact_defect, '__module__', "dex_connectors.creatio.creatiomock")

def get_contact_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactSummary]:
    return Case.retry(failure, after_seconds)

setattr(get_contact_retry, '__module__', "dex_connectors.creatio.creatiomock")

def get_contact_found_default() -> Case[ContactSummary]:
    return Case.branch_result(FOUND, ContactSummary(id='90a68db5-cb3a-4f64-bb60-c0a934ee7600', name='Ada Lovelace', email='ada@example.com'))

setattr(get_contact_found_default, '__module__', "dex_connectors.creatio.creatiomock")


class Mock:

    def __init__(self, connection_name: str = "creatio") -> None:
        self.get_contact_script: Query[GetContactInput, ContactSummary] = Query(GET_CONTACT_DEFINITION)
        self.get_contact_script.default(get_contact_found_default())
        self._connection = new_connection_with_operations(
            get_contact=self.get_contact_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.creatio.creatiomock")

__all__ = [
    "Mock",
    "get_contact_found",
    "get_contact_not_found",
    "get_contact_provider_rejected",
    "get_contact_invalid_response",
    "get_contact_defect",
    "get_contact_retry",
    "get_contact_found_default",
]
