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

CONNECTOR_ID = "airtable"
PROVIDER = "airtable"
LIST_RECORDS_OPERATION_ID = "listRecords"
GET_RECORD_OPERATION_ID = "getRecord"
UPSERT_RECORDS_OPERATION_ID = "upsertRecords"
UPDATE_RECORDS_OPERATION_ID = "updateRecords"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
OFFSET_EXPIRED = "offsetExpired"
PROVIDER_REJECTED = "providerRejected"
UPDATED = "updated"
UPSERTED = "upserted"

LIST_RECORDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_RECORDS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Airtable returned one bounded page of matching records; the page may be empty.", False),
        BranchDefinition(OFFSET_EXPIRED, "Airtable no longer recognizes the offset cursor, so the listing must restart from the first page.", True),
        BranchDefinition(PROVIDER_REJECTED, "Airtable conclusively rejected the list, such as an unknown base, table, view, or field, an invalid formula, a missing scope, or a rejected token.", True),
        BranchDefinition(INVALID_RESPONSE, "Airtable returned an invalid or oversized list response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_RECORD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_RECORD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The record exists and was returned.", False),
        BranchDefinition(NOT_FOUND, "No record in the base has the record ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Airtable conclusively rejected the read, such as an unknown base or table, a missing scope, or a rejected token.", True),
        BranchDefinition(INVALID_RESPONSE, "Airtable returned an invalid or oversized record response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPSERT_RECORDS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_RECORDS_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "Airtable created each record without a match and updated each record that matched.", False),
        BranchDefinition(PROVIDER_REJECTED, "Airtable conclusively rejected the upsert, such as several existing records matching one merge key, an invalid cell value, a computed merge field, a missing scope, or a rejected token.", True),
        BranchDefinition(INVALID_RESPONSE, "Airtable applied or rejected the upsert but returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_RECORDS_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_RECORDS_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Airtable updated every record and returned it.", False),
        BranchDefinition(NOT_FOUND, "A record ID does not exist in the table.", True),
        BranchDefinition(PROVIDER_REJECTED, "Airtable conclusively rejected the update, such as an invalid cell value, a computed field, a missing scope, or a rejected token.", True),
        BranchDefinition(INVALID_RESPONSE, "Airtable applied or rejected the update but returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
