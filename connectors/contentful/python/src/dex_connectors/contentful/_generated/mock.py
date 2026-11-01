# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetEntryInput, GetEntryOutput, ListEntriesInput, ListEntriesOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ENTRY_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ENTRIES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def get_entry_found(value: GetEntryOutput, failure: Failure | None = None) -> Case[GetEntryOutput]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_entry_found, '__module__', "dex_connectors.contentful.contentfulmock")

def get_entry_not_found(value: GetEntryOutput, failure: Failure | None = None) -> Case[GetEntryOutput]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_entry_not_found, '__module__', "dex_connectors.contentful.contentfulmock")

def get_entry_provider_rejected(value: GetEntryOutput, failure: Failure | None = None) -> Case[GetEntryOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_entry_provider_rejected, '__module__', "dex_connectors.contentful.contentfulmock")

def get_entry_invalid_response(value: GetEntryOutput, failure: Failure | None = None) -> Case[GetEntryOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_entry_invalid_response, '__module__', "dex_connectors.contentful.contentfulmock")

def get_entry_defect(value: GetEntryOutput, failure: Failure | None = None) -> Case[GetEntryOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_entry_defect, '__module__', "dex_connectors.contentful.contentfulmock")

def get_entry_retry(failure: Failure, after_seconds: float = 0) -> Case[GetEntryOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_entry_retry, '__module__', "dex_connectors.contentful.contentfulmock")

def get_entry_default_get_entry() -> Case[GetEntryOutput]:
    """Return the manifest defaultGetEntry outcome."""
    return Case.branch_result(FOUND, GetEntryOutput(data={'sys': {'id': 'entry-meridian', 'type': 'Entry'}, 'fields': {'title': 'Meridian'}}))

setattr(get_entry_default_get_entry, '__module__', "dex_connectors.contentful.contentfulmock")

def list_entries_listed(value: ListEntriesOutput, failure: Failure | None = None) -> Case[ListEntriesOutput]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(list_entries_listed, '__module__', "dex_connectors.contentful.contentfulmock")

def list_entries_provider_rejected(value: ListEntriesOutput, failure: Failure | None = None) -> Case[ListEntriesOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_entries_provider_rejected, '__module__', "dex_connectors.contentful.contentfulmock")

def list_entries_invalid_response(value: ListEntriesOutput, failure: Failure | None = None) -> Case[ListEntriesOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_entries_invalid_response, '__module__', "dex_connectors.contentful.contentfulmock")

def list_entries_defect(value: ListEntriesOutput, failure: Failure | None = None) -> Case[ListEntriesOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(list_entries_defect, '__module__', "dex_connectors.contentful.contentfulmock")

def list_entries_retry(failure: Failure, after_seconds: float = 0) -> Case[ListEntriesOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(list_entries_retry, '__module__', "dex_connectors.contentful.contentfulmock")

def list_entries_default_list_entries() -> Case[ListEntriesOutput]:
    """Return the manifest defaultListEntries outcome."""
    return Case.branch_result(LISTED, ListEntriesOutput(data={'items': [{'sys': {'id': 'entry-meridian', 'type': 'Entry'}, 'fields': {'title': 'Meridian'}}], 'total': 1, 'skip': 0, 'limit': 100}))

setattr(list_entries_default_list_entries, '__module__', "dex_connectors.contentful.contentfulmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "contentful") -> None:
        self.get_entry_script: Query[GetEntryInput, GetEntryOutput] = Query(GET_ENTRY_DEFINITION)
        self.get_entry_script.default(get_entry_default_get_entry())
        self.list_entries_script: Query[ListEntriesInput, ListEntriesOutput] = Query(LIST_ENTRIES_DEFINITION)
        self.list_entries_script.default(list_entries_default_list_entries())
        self._connection = new_connection_with_operations(
            get_entry=self.get_entry_script,
            list_entries=self.list_entries_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.contentful.contentfulmock")

__all__ = [
    "Mock",
    "get_entry_found",
    "get_entry_not_found",
    "get_entry_provider_rejected",
    "get_entry_invalid_response",
    "get_entry_defect",
    "get_entry_retry",
    "get_entry_default_get_entry",
    "list_entries_listed",
    "list_entries_provider_rejected",
    "list_entries_invalid_response",
    "list_entries_defect",
    "list_entries_retry",
    "list_entries_default_list_entries",
]
