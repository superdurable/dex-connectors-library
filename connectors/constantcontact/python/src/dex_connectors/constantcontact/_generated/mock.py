# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ContactMetadata, GetContactInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CONTACT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_contact_found(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_contact_found, '__module__', "dex_connectors.constantcontact.constantcontactmock")

def get_contact_not_found(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_contact_not_found, '__module__', "dex_connectors.constantcontact.constantcontactmock")

def get_contact_provider_rejected(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_contact_provider_rejected, '__module__', "dex_connectors.constantcontact.constantcontactmock")

def get_contact_invalid_response(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_contact_invalid_response, '__module__', "dex_connectors.constantcontact.constantcontactmock")

def get_contact_defect(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_contact_defect, '__module__', "dex_connectors.constantcontact.constantcontactmock")

def get_contact_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_contact_retry, '__module__', "dex_connectors.constantcontact.constantcontactmock")

def get_contact_found_default() -> Case[ContactMetadata]:
    return Case.branch_result(FOUND, ContactMetadata(contact_id='1898ae62-4752-11e9-9c8a-fa163e6b01c1', email_address='reader@example.com', first_name='Avery', last_name='Reader', email_permission='explicit'))

setattr(get_contact_found_default, '__module__', "dex_connectors.constantcontact.constantcontactmock")

def get_contact_contact_missing() -> Case[ContactMetadata]:
    return Case.branch_result(NOT_FOUND, ContactMetadata(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getContact", "Constant Contact did not find the contact."))

setattr(get_contact_contact_missing, '__module__', "dex_connectors.constantcontact.constantcontactmock")


class Mock:

    def __init__(self, connection_name: str = "constantcontact") -> None:
        self.get_contact_script: Query[GetContactInput, ContactMetadata] = Query(GET_CONTACT_DEFINITION)
        self.get_contact_script.default(get_contact_found_default())
        self._connection = new_connection_with_operations(
            get_contact=self.get_contact_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.constantcontact.constantcontactmock")

__all__ = [
    "Mock",
    "get_contact_found",
    "get_contact_not_found",
    "get_contact_provider_rejected",
    "get_contact_invalid_response",
    "get_contact_defect",
    "get_contact_retry",
    "get_contact_found_default",
    "get_contact_contact_missing",
]
