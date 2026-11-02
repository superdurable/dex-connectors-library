# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Contact, ContactWriteResult, CreateOrUpdateContactInput, GetContactInput
from .definitions import CONNECTOR_ID, CREATE_OR_UPDATE_CONTACT_DEFINITION, DEFECT, FOUND, GET_CONTACT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SAVED

def create_or_update_contact_saved(value: ContactWriteResult, failure: Failure | None = None) -> Case[ContactWriteResult]:
    return Case.branch_result(SAVED, value, failure)

setattr(create_or_update_contact_saved, '__module__', "dex_connectors.brevo.brevomock")

def create_or_update_contact_provider_rejected(value: ContactWriteResult, failure: Failure | None = None) -> Case[ContactWriteResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_or_update_contact_provider_rejected, '__module__', "dex_connectors.brevo.brevomock")

def create_or_update_contact_invalid_response(value: ContactWriteResult, failure: Failure | None = None) -> Case[ContactWriteResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_or_update_contact_invalid_response, '__module__', "dex_connectors.brevo.brevomock")

def create_or_update_contact_defect(value: ContactWriteResult, failure: Failure | None = None) -> Case[ContactWriteResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_or_update_contact_defect, '__module__', "dex_connectors.brevo.brevomock")

def create_or_update_contact_retry(failure: Failure, after_seconds: float = 0) -> Case[ContactWriteResult]:
    return Case.retry(failure, after_seconds)

setattr(create_or_update_contact_retry, '__module__', "dex_connectors.brevo.brevomock")

def create_or_update_contact_saved_default_contact() -> Case[ContactWriteResult]:
    return Case.branch_result(SAVED, ContactWriteResult(id=21, email='member@meridian.example', created=True))

setattr(create_or_update_contact_saved_default_contact, '__module__', "dex_connectors.brevo.brevomock")

def get_contact_found(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_contact_found, '__module__', "dex_connectors.brevo.brevomock")

def get_contact_not_found(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_contact_not_found, '__module__', "dex_connectors.brevo.brevomock")

def get_contact_provider_rejected(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_contact_provider_rejected, '__module__', "dex_connectors.brevo.brevomock")

def get_contact_invalid_response(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_contact_invalid_response, '__module__', "dex_connectors.brevo.brevomock")

def get_contact_defect(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_contact_defect, '__module__', "dex_connectors.brevo.brevomock")

def get_contact_retry(failure: Failure, after_seconds: float = 0) -> Case[Contact]:
    return Case.retry(failure, after_seconds)

setattr(get_contact_retry, '__module__', "dex_connectors.brevo.brevomock")

def get_contact_found_default_contact() -> Case[Contact]:
    return Case.branch_result(FOUND, Contact(id=21, email='member@meridian.example'))

setattr(get_contact_found_default_contact, '__module__', "dex_connectors.brevo.brevomock")


class Mock:

    def __init__(self, connection_name: str = "brevo") -> None:
        self.create_or_update_contact_script: Mutation[CreateOrUpdateContactInput, ContactWriteResult] = Mutation(CREATE_OR_UPDATE_CONTACT_DEFINITION)
        self.create_or_update_contact_script.default(create_or_update_contact_saved_default_contact())
        self.get_contact_script: Query[GetContactInput, Contact] = Query(GET_CONTACT_DEFINITION)
        self.get_contact_script.default(get_contact_found_default_contact())
        self._connection = new_connection_with_operations(
            create_or_update_contact=self.create_or_update_contact_script,
            get_contact=self.get_contact_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.brevo.brevomock")

__all__ = [
    "Mock",
    "create_or_update_contact_saved",
    "create_or_update_contact_provider_rejected",
    "create_or_update_contact_invalid_response",
    "create_or_update_contact_defect",
    "create_or_update_contact_retry",
    "create_or_update_contact_saved_default_contact",
    "get_contact_found",
    "get_contact_not_found",
    "get_contact_provider_rejected",
    "get_contact_invalid_response",
    "get_contact_defect",
    "get_contact_retry",
    "get_contact_found_default_contact",
]
