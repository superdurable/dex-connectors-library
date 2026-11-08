# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ContactMetadata, ContactsPage, GetContactInput, ListContactsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CONTACT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_CONTACTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_contacts_listed(value: ContactsPage, failure: Failure | None = None) -> Case[ContactsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_contacts_listed, '__module__', "dex_connectors.insightly.insightlymock")

def list_contacts_provider_rejected(value: ContactsPage, failure: Failure | None = None) -> Case[ContactsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_contacts_provider_rejected, '__module__', "dex_connectors.insightly.insightlymock")

def list_contacts_invalid_response(value: ContactsPage, failure: Failure | None = None) -> Case[ContactsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_contacts_invalid_response, '__module__', "dex_connectors.insightly.insightlymock")

def list_contacts_defect(value: ContactsPage, failure: Failure | None = None) -> Case[ContactsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_contacts_defect, '__module__', "dex_connectors.insightly.insightlymock")

def list_contacts_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_contacts_retry, '__module__', "dex_connectors.insightly.insightlymock")

def list_contacts_listed_default() -> Case[ContactsPage]:
    return Case.branch_result(LISTED, ContactsPage(contacts=(ContactMetadata(id=45678, first_name='Ada', last_name='Lovelace', email_address='ada@example.com'),)))

setattr(list_contacts_listed_default, '__module__', "dex_connectors.insightly.insightlymock")

def get_contact_found(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_contact_found, '__module__', "dex_connectors.insightly.insightlymock")

def get_contact_not_found(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_contact_not_found, '__module__', "dex_connectors.insightly.insightlymock")

def get_contact_provider_rejected(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_contact_provider_rejected, '__module__', "dex_connectors.insightly.insightlymock")

def get_contact_invalid_response(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_contact_invalid_response, '__module__', "dex_connectors.insightly.insightlymock")

def get_contact_defect(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_contact_defect, '__module__', "dex_connectors.insightly.insightlymock")

def get_contact_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_contact_retry, '__module__', "dex_connectors.insightly.insightlymock")

def get_contact_found_default() -> Case[ContactMetadata]:
    return Case.branch_result(FOUND, ContactMetadata(id=45678, first_name='Ada', last_name='Lovelace', email_address='ada@example.com'))

setattr(get_contact_found_default, '__module__', "dex_connectors.insightly.insightlymock")


class Mock:

    def __init__(self, connection_name: str = "insightly") -> None:
        self.list_contacts_script: Query[ListContactsInput, ContactsPage] = Query(LIST_CONTACTS_DEFINITION)
        self.list_contacts_script.default(list_contacts_listed_default())
        self.get_contact_script: Query[GetContactInput, ContactMetadata] = Query(GET_CONTACT_DEFINITION)
        self.get_contact_script.default(get_contact_found_default())
        self._connection = new_connection_with_operations(
            list_contacts=self.list_contacts_script,
            get_contact=self.get_contact_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.insightly.insightlymock")

__all__ = [
    "Mock",
    "list_contacts_listed",
    "list_contacts_provider_rejected",
    "list_contacts_invalid_response",
    "list_contacts_defect",
    "list_contacts_retry",
    "list_contacts_listed_default",
    "get_contact_found",
    "get_contact_not_found",
    "get_contact_provider_rejected",
    "get_contact_invalid_response",
    "get_contact_defect",
    "get_contact_retry",
    "get_contact_found_default",
]
