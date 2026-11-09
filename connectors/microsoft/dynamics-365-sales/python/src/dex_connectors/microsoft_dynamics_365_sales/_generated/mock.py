# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Contact, FindContactInput
from .definitions import CONNECTOR_ID, DEFECT, FIND_CONTACT_DEFINITION, FOUND, INVALID_RESPONSE, MISSING, MULTIPLE_MATCHES, PROVIDER_REJECTED

def find_contact_found(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_contact_found, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

def find_contact_missing(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(MISSING, value, failure)

setattr(find_contact_missing, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

def find_contact_multiple_matches(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(MULTIPLE_MATCHES, value, failure)

setattr(find_contact_multiple_matches, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

def find_contact_provider_rejected(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_contact_provider_rejected, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

def find_contact_invalid_response(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_contact_invalid_response, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

def find_contact_defect(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_contact_defect, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

def find_contact_retry(failure: Failure, after_seconds: float = 0) -> Case[Contact]:
    return Case.retry(failure, after_seconds)

setattr(find_contact_retry, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

def find_contact_found_default() -> Case[Contact]:
    return Case.branch_result(FOUND, Contact(contact_id='00000000-0000-0000-0000-000000000001', full_name='Ada Example', email_address='ada@example.com'))

setattr(find_contact_found_default, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-dynamics-365-sales") -> None:
        self.find_contact_script: Query[FindContactInput, Contact] = Query(FIND_CONTACT_DEFINITION)
        self.find_contact_script.default(find_contact_found_default())
        self._connection = new_connection_with_operations(
            find_contact=self.find_contact_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock")

__all__ = [
    "Mock",
    "find_contact_found",
    "find_contact_missing",
    "find_contact_multiple_matches",
    "find_contact_provider_rejected",
    "find_contact_invalid_response",
    "find_contact_defect",
    "find_contact_retry",
    "find_contact_found_default",
]
