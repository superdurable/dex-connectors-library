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

CONNECTOR_ID = "zoho-crm"
PROVIDER = "zoho"
FIND_RECORDS_OPERATION_ID = "findRecords"
GET_RECORD_OPERATION_ID = "getRecord"
LIST_MODIFIED_RECORDS_OPERATION_ID = "listModifiedRecords"
UPSERT_RECORD_OPERATION_ID = "upsertRecord"
UPDATE_RECORD_OPERATION_ID = "updateRecord"
LIST_MODULE_FIELDS_OPERATION_ID = "listModuleFields"
CONFLICT = "conflict"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
RECORD_REJECTED = "recordRejected"
UPDATED = "updated"
UPSERTED = "upserted"

FIND_RECORDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_RECORDS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "At least one record matched; the page holds the matches and reports whether more remain.", False),
        BranchDefinition(NOT_FOUND, "No record of the module matched every condition.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoho CRM conclusively rejected the query, such as an unknown module or field, a value of the wrong type, a missing scope, or a revoked authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho CRM returned an invalid or oversized page of records.", True),
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
        BranchDefinition(FOUND, "Zoho CRM returned the record.", False),
        BranchDefinition(NOT_FOUND, "No record of the module has the ID, or the record was deleted or is not visible to the connection's user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoho CRM conclusively rejected the read, such as an unknown module or field, a missing scope, or a revoked authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho CRM returned an invalid or oversized record.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_MODIFIED_RECORDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MODIFIED_RECORDS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of changed records was returned with the cursor to continue from; the page may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zoho CRM conclusively rejected the listing, such as an unknown module or field, a missing scope, or a revoked authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho CRM returned an invalid or oversized page, such as a record without its modified time.", True),
        BranchDefinition(DEFECT, "Local input, cursor, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPSERT_RECORD_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_RECORD_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "Zoho CRM created the record or updated the record that holds the duplicate-check value.", False),
        BranchDefinition(CONFLICT, "Another record already holds a value of a unique field the upsert sets, after one resend; nothing was written.", True),
        BranchDefinition(RECORD_REJECTED, "Zoho CRM rejected the field values, such as MANDATORY_NOT_FOUND, INVALID_DATA, or RECORD_LOCKED; nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoho CRM conclusively refused the request, such as an unknown module, a user without create or edit permission, a missing scope, or a revoked authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho CRM answered with an invalid or oversized response, so the record may have been written.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_RECORD_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_RECORD_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Zoho CRM applied the field values to the record.", False),
        BranchDefinition(NOT_FOUND, "No record of the module has the ID, or it is not visible to the connection's user; nothing was written.", True),
        BranchDefinition(CONFLICT, "Another record already holds a value of a unique field the update sets; nothing was written.", True),
        BranchDefinition(RECORD_REJECTED, "Zoho CRM rejected the field values, such as INVALID_DATA, RECORD_LOCKED, or a blueprint that forbids the change; nothing was written.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoho CRM conclusively refused the request, such as an unknown module, a user without edit permission, a missing scope, or a revoked authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho CRM answered with an invalid or oversized response, so the update may have been applied.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_MODULE_FIELDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MODULE_FIELDS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Zoho CRM returned the module's fields.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zoho CRM conclusively rejected the request, such as an unknown module, a missing ZohoCRM.settings.fields.READ scope, or a revoked authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoho CRM returned an invalid or oversized field list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
