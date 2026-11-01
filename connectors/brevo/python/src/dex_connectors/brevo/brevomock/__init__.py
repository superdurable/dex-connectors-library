# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Brevo scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import (
    CONNECTOR_ID,
    CREATE_OR_UPDATE_CONTACT_DEFINITION,
    FOUND,
    GET_CONTACT_DEFINITION,
    NOT_FOUND,
    SAVED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import Contact, ContactWriteResult, CreateOrUpdateContactInput, GetContactInput


def create_or_update_contact_saved(
    value: ContactWriteResult, failure: Failure | None = None
) -> Case[ContactWriteResult]:
    """Script the saved route."""
    return Case.branch_result(SAVED, value, failure)


def create_or_update_contact_default_answer() -> Case[ContactWriteResult]:
    """Return the manifest's createOrUpdateContact default answer."""
    return create_or_update_contact_saved(ContactWriteResult("member@meridian.example", True, 21))


def get_contact_found(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_contact_not_found(value: Contact, failure: Failure | None = None) -> Case[Contact]:
    """Script a missing contact."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_contact_default_answer() -> Case[Contact]:
    """Return the manifest's getContact default answer."""
    return get_contact_found(Contact(21, "member@meridian.example"))


class Mock:
    """Script Brevo operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "brevo") -> None:
        self.create_or_update_contact_script: Mutation[CreateOrUpdateContactInput, ContactWriteResult] = (
            Mutation(CREATE_OR_UPDATE_CONTACT_DEFINITION)
        )
        self.create_or_update_contact_script.default(create_or_update_contact_default_answer())
        self.get_contact_script: Query[GetContactInput, Contact] = Query(GET_CONTACT_DEFINITION)
        self.get_contact_script.default(get_contact_default_answer())
        self._connection = new_connection_with_operations(
            self.create_or_update_contact_script,
            self.get_contact_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "create_or_update_contact_saved",
    "create_or_update_contact_default_answer",
    "get_contact_found",
    "get_contact_not_found",
    "get_contact_default_answer",
]
