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

CONNECTOR_ID = "hubspot"
PROVIDER = "hubspot"
SEARCH_OBJECTS_OPERATION_ID = "searchObjects"
GET_OBJECT_OPERATION_ID = "getObject"
UPSERT_OBJECT_OPERATION_ID = "upsertObject"
UPDATE_OBJECT_OPERATION_ID = "updateObject"
CONFLICT = "conflict"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
UPDATED = "updated"
UPSERTED = "upserted"

SEARCH_OBJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_OBJECTS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "HubSpot returned one bounded page of matching records; the page may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "HubSpot conclusively rejected the search, such as an unknown property, a missing scope, a rejected token, or an exhausted daily limit.", True),
        BranchDefinition(INVALID_RESPONSE, "HubSpot returned an invalid or oversized search response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_OBJECT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_OBJECT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The record exists and was returned.", False),
        BranchDefinition(NOT_FOUND, "No unarchived record of that object type has the record ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "HubSpot conclusively rejected the read, such as an unknown property, a missing scope, a rejected token, or an exhausted daily limit.", True),
        BranchDefinition(INVALID_RESPONSE, "HubSpot returned an invalid or oversized record response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPSERT_OBJECT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_OBJECT_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "HubSpot created the record or updated the record that owns the unique value.", False),
        BranchDefinition(CONFLICT, "The properties conflict with another record, such as an email that another contact already owns.", True),
        BranchDefinition(PROVIDER_REJECTED, "HubSpot conclusively rejected the upsert, such as an invalid property value, a property that is not unique, a missing scope, a rejected token, or an exhausted daily limit.", True),
        BranchDefinition(INVALID_RESPONSE, "HubSpot applied or rejected the upsert but returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_OBJECT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_OBJECT_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "HubSpot updated the record and returned it.", False),
        BranchDefinition(NOT_FOUND, "No unarchived record of that object type has the record ID.", True),
        BranchDefinition(CONFLICT, "The properties conflict with another record, such as an email that another contact already owns.", True),
        BranchDefinition(PROVIDER_REJECTED, "HubSpot conclusively rejected the update, such as an invalid property value, a missing scope, a rejected token, or an exhausted daily limit.", True),
        BranchDefinition(INVALID_RESPONSE, "HubSpot applied or rejected the update but returned an invalid or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
