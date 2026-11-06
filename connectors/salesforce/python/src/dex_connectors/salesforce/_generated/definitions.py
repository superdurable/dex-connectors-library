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

CONNECTOR_ID = "salesforce"
PROVIDER = "salesforce"
QUERY_RECORDS_OPERATION_ID = "queryRecords"
GET_RECORD_OPERATION_ID = "getRecord"
UPSERT_RECORD_BY_EXTERNAL_ID_OPERATION_ID = "upsertRecordByExternalId"
UPDATE_RECORD_OPERATION_ID = "updateRecord"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MULTIPLE_MATCHES = "multipleMatches"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
QUERY_REJECTED = "queryRejected"
RECORD_REJECTED = "recordRejected"
UPDATED = "updated"
UPSERTED = "upserted"

QUERY_RECORDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_RECORDS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Salesforce returned matching records or reported that more pages remain.", False),
        BranchDefinition(NOT_FOUND, "No record matched and Salesforce reported no further pages.", True),
        BranchDefinition(QUERY_REJECTED, "Salesforce rejected the SOQL statement or page cursor, such as MALFORMED_QUERY, INVALID_FIELD, INVALID_TYPE, or an expired INVALID_QUERY_LOCATOR.", True),
        BranchDefinition(PROVIDER_REJECTED, "Salesforce conclusively refused the request, such as a revoked session or a user without API or object access.", True),
        BranchDefinition(INVALID_RESPONSE, "Salesforce returned an invalid or oversized query page.", True),
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
        BranchDefinition(FOUND, "Salesforce returned the record.", False),
        BranchDefinition(NOT_FOUND, "The record does not exist, was deleted, or the sObject type is unknown to the connection's user.", True),
        BranchDefinition(PROVIDER_REJECTED, "Salesforce conclusively refused the read, such as an unknown field name or a user without object or field access.", True),
        BranchDefinition(INVALID_RESPONSE, "Salesforce returned an invalid or oversized record.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPSERT_RECORD_BY_EXTERNAL_ID_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_RECORD_BY_EXTERNAL_ID_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "Salesforce created the record or updated the one record that carries the external ID value.", False),
        BranchDefinition(MULTIPLE_MATCHES, "More than one record carries the external ID value, so Salesforce changed nothing.", True),
        BranchDefinition(RECORD_REJECTED, "Salesforce rejected the record values, such as REQUIRED_FIELD_MISSING, INVALID_FIELD, FIELD_CUSTOM_VALIDATION_EXCEPTION, or DUPLICATES_DETECTED.", True),
        BranchDefinition(PROVIDER_REJECTED, "Salesforce conclusively refused the request, such as a revoked session, an unknown sObject or external ID field, or a user without create or edit access.", True),
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
        BranchDefinition(UPDATED, "Salesforce applied the field values to the record.", False),
        BranchDefinition(NOT_FOUND, "The record does not exist, was deleted, or the sObject type is unknown to the connection's user.", True),
        BranchDefinition(RECORD_REJECTED, "Salesforce rejected the field values, such as INVALID_FIELD, FIELD_CUSTOM_VALIDATION_EXCEPTION, DUPLICATE_VALUE, or ENTITY_IS_LOCKED.", True),
        BranchDefinition(PROVIDER_REJECTED, "Salesforce conclusively refused the request, such as a revoked session or a user without edit access.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
