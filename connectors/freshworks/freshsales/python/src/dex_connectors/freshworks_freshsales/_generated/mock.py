# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ContactMetadata, ContactViewMetadata, ContactViews, GetContactInput, ListContactViewsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CONTACT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_CONTACT_VIEWS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_contact_views_listed(value: ContactViews, failure: Failure | None = None) -> Case[ContactViews]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_contact_views_listed, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def list_contact_views_provider_rejected(value: ContactViews, failure: Failure | None = None) -> Case[ContactViews]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_contact_views_provider_rejected, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def list_contact_views_invalid_response(value: ContactViews, failure: Failure | None = None) -> Case[ContactViews]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_contact_views_invalid_response, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def list_contact_views_defect(value: ContactViews, failure: Failure | None = None) -> Case[ContactViews]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_contact_views_defect, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def list_contact_views_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactViews]:
    return Case.retry(failure, after_seconds)

setattr(list_contact_views_retry, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def list_contact_views_listed_default() -> Case[ContactViews]:
    return Case.branch_result(LISTED, ContactViews(views=(ContactViewMetadata(id=1, name='All Contacts'),)))

setattr(list_contact_views_listed_default, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def get_contact_found(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_contact_found, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def get_contact_not_found(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_contact_not_found, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def get_contact_provider_rejected(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_contact_provider_rejected, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def get_contact_invalid_response(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_contact_invalid_response, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def get_contact_defect(value: ContactMetadata, failure: Failure | None = None) -> Case[ContactMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_contact_defect, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def get_contact_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_contact_retry, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

def get_contact_found_default() -> Case[ContactMetadata]:
    return Case.branch_result(FOUND, ContactMetadata(id=144, first_name='Ada', last_name='Lovelace', email_address='ada@example.com'))

setattr(get_contact_found_default, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")


class Mock:

    def __init__(self, connection_name: str = "freshsales") -> None:
        self.list_contact_views_script: Query[ListContactViewsInput, ContactViews] = Query(LIST_CONTACT_VIEWS_DEFINITION)
        self.list_contact_views_script.default(list_contact_views_listed_default())
        self.get_contact_script: Query[GetContactInput, ContactMetadata] = Query(GET_CONTACT_DEFINITION)
        self.get_contact_script.default(get_contact_found_default())
        self._connection = new_connection_with_operations(
            list_contact_views=self.list_contact_views_script,
            get_contact=self.get_contact_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.freshworks_freshsales.freshsalesmock")

__all__ = [
    "Mock",
    "list_contact_views_listed",
    "list_contact_views_provider_rejected",
    "list_contact_views_invalid_response",
    "list_contact_views_defect",
    "list_contact_views_retry",
    "list_contact_views_listed_default",
    "get_contact_found",
    "get_contact_not_found",
    "get_contact_provider_rejected",
    "get_contact_invalid_response",
    "get_contact_defect",
    "get_contact_retry",
    "get_contact_found_default",
]
