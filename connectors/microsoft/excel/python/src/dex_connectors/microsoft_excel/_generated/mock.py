# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AppendTableRowsInput, AppendTableRowsOutput, GetTableRowsInput, GetTableRowsOutput, GetValuesInput, GetValuesOutput, TableRow, UpdateValuesInput, UpdateValuesOutput
from .definitions import CONNECTOR_ID, PROVIDER, APPENDED, APPEND_TABLE_ROWS_DEFINITION, COLUMNS_MISMATCH, DEFECT, GET_TABLE_ROWS_DEFINITION, GET_VALUES_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, READ, TOO_LARGE, UNCERTAIN, UPDATED, UPDATE_VALUES_DEFINITION

def get_table_rows_read(value: GetTableRowsOutput, failure: Failure | None = None) -> Case[GetTableRowsOutput]:
    return Case.branch_result(READ, value, failure)

setattr(get_table_rows_read, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_not_found(value: GetTableRowsOutput, failure: Failure | None = None) -> Case[GetTableRowsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_table_rows_not_found, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_too_large(value: GetTableRowsOutput, failure: Failure | None = None) -> Case[GetTableRowsOutput]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(get_table_rows_too_large, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_provider_rejected(value: GetTableRowsOutput, failure: Failure | None = None) -> Case[GetTableRowsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_table_rows_provider_rejected, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_invalid_response(value: GetTableRowsOutput, failure: Failure | None = None) -> Case[GetTableRowsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_table_rows_invalid_response, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_defect(value: GetTableRowsOutput, failure: Failure | None = None) -> Case[GetTableRowsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_table_rows_defect, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_retry(failure: Failure, after_seconds: float = 0) -> Case[GetTableRowsOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_table_rows_retry, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_approval_policy_rows() -> Case[GetTableRowsOutput]:
    return Case.branch_result(READ, GetTableRowsOutput(columns=('Category', 'MaxAutoApproveUsd', 'Approver'), rows=(TableRow(index=0, values={'Category': 'travel', 'MaxAutoApproveUsd': 500, 'Approver': 'lead@contoso.com'}), TableRow(index=1, values={'Category': 'hardware', 'MaxAutoApproveUsd': 1500, 'Approver': 'it@contoso.com'}))))

setattr(get_table_rows_approval_policy_rows, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_policy_table_missing() -> Case[GetTableRowsOutput]:
    return Case.branch_result(NOT_FOUND, GetTableRowsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getTableRows", "Workbook or table was not found or is not visible to the connection."))

setattr(get_table_rows_policy_table_missing, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_policy_table_too_large() -> Case[GetTableRowsOutput]:
    return Case.branch_result(TOO_LARGE, GetTableRowsOutput(), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "getTableRows", "Table holds more cells than the configured cell limit."))

setattr(get_table_rows_policy_table_too_large, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_table_rows_policy_read_forbidden() -> Case[GetTableRowsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, GetTableRowsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getTableRows", "Provider rejected the policy table read."))

setattr(get_table_rows_policy_read_forbidden, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_read(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(READ, value, failure)

setattr(get_values_read, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_not_found(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_values_not_found, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_too_large(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(get_values_too_large, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_provider_rejected(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_values_provider_rejected, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_invalid_response(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_values_invalid_response, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_defect(value: GetValuesOutput, failure: Failure | None = None) -> Case[GetValuesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_values_defect, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_retry(failure: Failure, after_seconds: float = 0) -> Case[GetValuesOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_values_retry, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_approved_summary() -> Case[GetValuesOutput]:
    return Case.branch_result(READ, GetValuesOutput(address='Summary!A2:E2', row_count=1, column_count=5, values=(('REQ-1001', 'Travel', 420, 'approved', '2026-10-26T10:00:00Z'),), text=(('REQ-1001', 'Travel', '420', 'approved', '2026-10-26T10:00:00Z'),)))

setattr(get_values_approved_summary, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_summary_worksheet_missing() -> Case[GetValuesOutput]:
    return Case.branch_result(NOT_FOUND, GetValuesOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getValues", "Workbook or worksheet was not found or is not visible to the connection."))

setattr(get_values_summary_worksheet_missing, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_summary_response_too_large() -> Case[GetValuesOutput]:
    return Case.branch_result(TOO_LARGE, GetValuesOutput(), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "getValues", "Provider response exceeds the configured response limit."))

setattr(get_values_summary_response_too_large, '__module__', "dex_connectors.microsoft_excel.excelmock")

def get_values_summary_read_forbidden() -> Case[GetValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, GetValuesOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getValues", "Provider rejected the summary read."))

setattr(get_values_summary_read_forbidden, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_updated(value: UpdateValuesOutput, failure: Failure | None = None) -> Case[UpdateValuesOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_values_updated, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_not_found(value: UpdateValuesOutput, failure: Failure | None = None) -> Case[UpdateValuesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_values_not_found, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_provider_rejected(value: UpdateValuesOutput, failure: Failure | None = None) -> Case[UpdateValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_values_provider_rejected, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_defect(value: UpdateValuesOutput, failure: Failure | None = None) -> Case[UpdateValuesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_values_defect, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateValuesOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_values_retry, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_approved_summary_written() -> Case[UpdateValuesOutput]:
    return Case.branch_result(UPDATED, UpdateValuesOutput(address='Summary!A2:E2', row_count=1, column_count=5))

setattr(update_values_approved_summary_written, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_summary_write_missing() -> Case[UpdateValuesOutput]:
    return Case.branch_result(NOT_FOUND, UpdateValuesOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateValues", "Workbook or worksheet was not found or is not visible to the connection."))

setattr(update_values_summary_write_missing, '__module__', "dex_connectors.microsoft_excel.excelmock")

def update_values_summary_protected() -> Case[UpdateValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateValuesOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateValues", "Provider rejected the write."))

setattr(update_values_summary_protected, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_appended(value: AppendTableRowsOutput, failure: Failure | None = None) -> Case[AppendTableRowsOutput]:
    return Case.branch_result(APPENDED, value, failure)

setattr(append_table_rows_appended, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_not_found(value: AppendTableRowsOutput, failure: Failure | None = None) -> Case[AppendTableRowsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(append_table_rows_not_found, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_columns_mismatch(value: AppendTableRowsOutput, failure: Failure | None = None) -> Case[AppendTableRowsOutput]:
    return Case.branch_result(COLUMNS_MISMATCH, value, failure)

setattr(append_table_rows_columns_mismatch, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_provider_rejected(value: AppendTableRowsOutput, failure: Failure | None = None) -> Case[AppendTableRowsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(append_table_rows_provider_rejected, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_invalid_response(value: AppendTableRowsOutput, failure: Failure | None = None) -> Case[AppendTableRowsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(append_table_rows_invalid_response, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_uncertain(value: AppendTableRowsOutput, failure: Failure) -> Case[AppendTableRowsOutput]:
    return Case.uncertain(value, failure)

setattr(append_table_rows_uncertain, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_defect(value: AppendTableRowsOutput, failure: Failure | None = None) -> Case[AppendTableRowsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(append_table_rows_defect, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_retry(failure: Failure, after_seconds: float = 0) -> Case[AppendTableRowsOutput]:
    return Case.retry(failure, after_seconds)

setattr(append_table_rows_retry, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_approved_decision_logged() -> Case[AppendTableRowsOutput]:
    return Case.branch_result(APPENDED, AppendTableRowsOutput(appended_keys=('REQ-1001',), already_present_keys=()))

setattr(append_table_rows_approved_decision_logged, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_decision_table_missing() -> Case[AppendTableRowsOutput]:
    return Case.branch_result(NOT_FOUND, AppendTableRowsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "appendTableRows", "Workbook or table was not found or is not visible to the connection."))

setattr(append_table_rows_decision_table_missing, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_decision_columns_mismatch() -> Case[AppendTableRowsOutput]:
    return Case.branch_result(COLUMNS_MISMATCH, AppendTableRowsOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "appendTableRows", "The table has no column with the key column's name."))

setattr(append_table_rows_decision_columns_mismatch, '__module__', "dex_connectors.microsoft_excel.excelmock")

def append_table_rows_decision_append_forbidden() -> Case[AppendTableRowsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AppendTableRowsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "appendTableRows", "Provider rejected the append; nothing was written."))

setattr(append_table_rows_decision_append_forbidden, '__module__', "dex_connectors.microsoft_excel.excelmock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-excel") -> None:
        self.get_table_rows_script: Query[GetTableRowsInput, GetTableRowsOutput] = Query(GET_TABLE_ROWS_DEFINITION)
        self.get_table_rows_script.default(get_table_rows_approval_policy_rows())
        self.get_values_script: Query[GetValuesInput, GetValuesOutput] = Query(GET_VALUES_DEFINITION)
        self.get_values_script.default(get_values_approved_summary())
        self.update_values_script: Mutation[UpdateValuesInput, UpdateValuesOutput] = Mutation(UPDATE_VALUES_DEFINITION)
        self.update_values_script.default(update_values_approved_summary_written())
        self.append_table_rows_script: Mutation[AppendTableRowsInput, AppendTableRowsOutput] = Mutation(APPEND_TABLE_ROWS_DEFINITION)
        self.append_table_rows_script.default(append_table_rows_approved_decision_logged())
        self._connection = new_connection_with_operations(
            get_table_rows=self.get_table_rows_script,
            get_values=self.get_values_script,
            update_values=self.update_values_script,
            append_table_rows=self.append_table_rows_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_excel.excelmock")

__all__ = [
    "Mock",
    "get_table_rows_read",
    "get_table_rows_not_found",
    "get_table_rows_too_large",
    "get_table_rows_provider_rejected",
    "get_table_rows_invalid_response",
    "get_table_rows_defect",
    "get_table_rows_retry",
    "get_table_rows_approval_policy_rows",
    "get_table_rows_policy_table_missing",
    "get_table_rows_policy_table_too_large",
    "get_table_rows_policy_read_forbidden",
    "get_values_read",
    "get_values_not_found",
    "get_values_too_large",
    "get_values_provider_rejected",
    "get_values_invalid_response",
    "get_values_defect",
    "get_values_retry",
    "get_values_approved_summary",
    "get_values_summary_worksheet_missing",
    "get_values_summary_response_too_large",
    "get_values_summary_read_forbidden",
    "update_values_updated",
    "update_values_not_found",
    "update_values_provider_rejected",
    "update_values_defect",
    "update_values_retry",
    "update_values_approved_summary_written",
    "update_values_summary_write_missing",
    "update_values_summary_protected",
    "append_table_rows_appended",
    "append_table_rows_not_found",
    "append_table_rows_columns_mismatch",
    "append_table_rows_provider_rejected",
    "append_table_rows_invalid_response",
    "append_table_rows_uncertain",
    "append_table_rows_defect",
    "append_table_rows_retry",
    "append_table_rows_approved_decision_logged",
    "append_table_rows_decision_table_missing",
    "append_table_rows_decision_columns_mismatch",
    "append_table_rows_decision_append_forbidden",
]
