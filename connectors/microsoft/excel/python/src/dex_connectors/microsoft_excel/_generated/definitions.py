# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "microsoft-excel"
PROVIDER = "microsoft"
GET_TABLE_ROWS_OPERATION_ID = "getTableRows"
GET_VALUES_OPERATION_ID = "getValues"
UPDATE_VALUES_OPERATION_ID = "updateValues"
APPEND_TABLE_ROWS_OPERATION_ID = "appendTableRows"
APPENDED = "appended"
COLUMNS_MISMATCH = "columnsMismatch"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
READ = "read"
TOO_LARGE = "tooLarge"
UNCERTAIN = "uncertain"
UPDATED = "updated"

GET_TABLE_ROWS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TABLE_ROWS_OPERATION_ID),
    (
        BranchDefinition(READ, "The table's columns and data rows were read within the configured limits.", False),
        BranchDefinition(NOT_FOUND, "The workbook, or the table in it, does not exist or the connection cannot see it.", True),
        BranchDefinition(TOO_LARGE, "The table holds more cells than the configured limit or its response exceeds the response limit.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft conclusively rejected the read, such as a revoked grant, a missing permission, or a workbook Excel cannot open.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft returned an invalid table response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_VALUES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_VALUES_OPERATION_ID),
    (
        BranchDefinition(READ, "The range was read.", False),
        BranchDefinition(NOT_FOUND, "The workbook, or the worksheet in it, does not exist or the connection cannot see it.", True),
        BranchDefinition(TOO_LARGE, "The range response exceeds the configured response limit.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft conclusively rejected the read, such as a revoked grant, a missing permission, or an address Excel rejects.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft returned an invalid range response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_VALUES_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_VALUES_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The range holds the requested values, written by this attempt or an earlier attempt of the same Step execution.", False),
        BranchDefinition(NOT_FOUND, "The workbook, or the worksheet in it, does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft conclusively rejected the write, such as a protected worksheet, merged cells, or a missing permission.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

APPEND_TABLE_ROWS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, APPEND_TABLE_ROWS_OPERATION_ID),
    (
        BranchDefinition(APPENDED, "The table holds every requested key: this attempt appended the missing rows, or an earlier attempt or Flow already had.", False),
        BranchDefinition(NOT_FOUND, "The workbook, or the table in it, does not exist or the connection cannot see it; nothing was written.", True),
        BranchDefinition(COLUMNS_MISMATCH, "The table has no column with the key column's name or with a name a row uses; nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft conclusively rejected the append or the key lookup, such as a protected worksheet or a missing permission; nothing was written.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft returned an invalid or oversized response before the append was sent.", True),
        BranchDefinition(UNCERTAIN, "The append was sent but its outcome cannot be confirmed and the table does not show its rows yet; the connector never sends it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 5, timedelta(seconds=300)),
    ),
)
