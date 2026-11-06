# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FindRowInput, FindRowOutput, GetValuesInput, GetValuesOutput, UpsertRowInput, UpsertRowOutput
from .definitions import CONNECTOR_ID, PROVIDER, CONFLICT, DEFECT, FIND_ROW_DEFINITION, FOUND, GET_VALUES_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, READ, UNCERTAIN, UPSERTED, UPSERT_ROW_DEFINITION

def get_values_read(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(READ, value, failure)

setattr(get_values_read, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_not_found(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_values_not_found, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_provider_rejected(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_values_provider_rejected, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_invalid_response(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_values_invalid_response, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_defect(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_values_defect, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_retry(failure: Failure, after_seconds: float = 0) -> Case[GetValuesOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_values_retry, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_contacts_range() -> Case[GetValuesOutput]:
    return Case.branch_result(READ, GetValuesOutput(range='Contacts!A1:C3', major_dimension='ROWS', values=(('email', 'name', 'status'), ('ada@example.test', 'Ada Lovelace', 'active'), ('grace@example.test', 'Grace Hopper', 'pending'))))

setattr(get_values_contacts_range, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_range_not_found() -> Case[GetValuesOutput]:
    return Case.branch_result(NOT_FOUND, GetValuesOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getValues", "The spreadsheet or range was not found."))

setattr(get_values_range_not_found, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def get_values_read_rejected() -> Case[GetValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, GetValuesOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getValues", "Google Sheets rejected the range read."))

setattr(get_values_read_rejected, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_found(value: FindRowOutput, failure: Failure | None = None) -> Case[FindRowOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(find_row_found, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_not_found(value: FindRowOutput, failure: Failure | None = None) -> Case[FindRowOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(find_row_not_found, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_conflict(value: FindRowOutput, failure: Failure | None = None) -> Case[FindRowOutput]:
    return Case.branch_result(CONFLICT, value, failure)

setattr(find_row_conflict, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_provider_rejected(value: FindRowOutput, failure: Failure | None = None) -> Case[FindRowOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(find_row_provider_rejected, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_invalid_response(value: FindRowOutput, failure: Failure | None = None) -> Case[FindRowOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(find_row_invalid_response, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_defect(value: FindRowOutput, failure: Failure | None = None) -> Case[FindRowOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(find_row_defect, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_retry(failure: Failure, after_seconds: float = 0) -> Case[FindRowOutput]:
    return Case.retry(failure, after_seconds)

setattr(find_row_retry, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_contact_found() -> Case[FindRowOutput]:
    return Case.branch_result(FOUND, FindRowOutput(row_number=2, values={'email': 'ada@example.test', 'name': 'Ada Lovelace', 'status': 'active'}))

setattr(find_row_contact_found, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_contact_not_found() -> Case[FindRowOutput]:
    return Case.branch_result(NOT_FOUND, FindRowOutput())

setattr(find_row_contact_not_found, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_duplicate_contacts() -> Case[FindRowOutput]:
    return Case.branch_result(CONFLICT, FindRowOutput(conflicting_rows=(2, 4)), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "findRow", "More than one contact row has the requested email key."))

setattr(find_row_duplicate_contacts, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def find_row_find_rejected() -> Case[FindRowOutput]:
    return Case.branch_result(PROVIDER_REJECTED, FindRowOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "findRow", "Google Sheets rejected the row lookup."))

setattr(find_row_find_rejected, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_upserted(value: UpsertRowOutput, failure: Failure | None = None) -> Case[UpsertRowOutput]:
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_row_upserted, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_conflict(value: UpsertRowOutput, failure: Failure | None = None) -> Case[UpsertRowOutput]:
    return Case.branch_result(CONFLICT, value, failure)

setattr(upsert_row_conflict, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_provider_rejected(value: UpsertRowOutput, failure: Failure | None = None) -> Case[UpsertRowOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_row_provider_rejected, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_invalid_response(value: UpsertRowOutput, failure: Failure | None = None) -> Case[UpsertRowOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_row_invalid_response, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_uncertain(value: UpsertRowOutput, failure: Failure) -> Case[UpsertRowOutput]:
    return Case.uncertain(value, failure)

setattr(upsert_row_uncertain, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_defect(value: UpsertRowOutput, failure: Failure | None = None) -> Case[UpsertRowOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_row_defect, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertRowOutput]:
    return Case.retry(failure, after_seconds)

setattr(upsert_row_retry, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_contact_upserted() -> Case[UpsertRowOutput]:
    return Case.branch_result(UPSERTED, UpsertRowOutput(action='updated', row_number=2, updated_range='Contacts!A2:C2'))

setattr(upsert_row_contact_upserted, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_duplicate_key() -> Case[UpsertRowOutput]:
    return Case.branch_result(CONFLICT, UpsertRowOutput(), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "upsertRow", "More than one contact row has the requested email key."))

setattr(upsert_row_duplicate_key, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

def upsert_row_write_rejected() -> Case[UpsertRowOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpsertRowOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "upsertRow", "Google Sheets rejected the contact upsert."))

setattr(upsert_row_write_rejected, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")


class Mock:

    def __init__(self, connection_name: str = "google-sheets") -> None:
        self.get_values_script: Query[GetValuesInput, GetValuesOutput] = Query(GET_VALUES_DEFINITION)
        self.get_values_script.default(get_values_contacts_range())
        self.find_row_script: Query[FindRowInput, FindRowOutput] = Query(FIND_ROW_DEFINITION)
        self.find_row_script.default(find_row_contact_found())
        self.upsert_row_script: Mutation[UpsertRowInput, UpsertRowOutput] = Mutation(UPSERT_ROW_DEFINITION)
        self.upsert_row_script.default(upsert_row_contact_upserted())
        self._connection = new_connection_with_operations(
            get_values=self.get_values_script,
            find_row=self.find_row_script,
            upsert_row=self.upsert_row_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_spreadsheet.spreadsheetmock")

__all__ = [
    "Mock",
    "get_values_read",
    "get_values_not_found",
    "get_values_provider_rejected",
    "get_values_invalid_response",
    "get_values_defect",
    "get_values_retry",
    "get_values_contacts_range",
    "get_values_range_not_found",
    "get_values_read_rejected",
    "find_row_found",
    "find_row_not_found",
    "find_row_conflict",
    "find_row_provider_rejected",
    "find_row_invalid_response",
    "find_row_defect",
    "find_row_retry",
    "find_row_contact_found",
    "find_row_contact_not_found",
    "find_row_duplicate_contacts",
    "find_row_find_rejected",
    "upsert_row_upserted",
    "upsert_row_conflict",
    "upsert_row_provider_rejected",
    "upsert_row_invalid_response",
    "upsert_row_uncertain",
    "upsert_row_defect",
    "upsert_row_retry",
    "upsert_row_contact_upserted",
    "upsert_row_duplicate_key",
    "upsert_row_write_rejected",
]
