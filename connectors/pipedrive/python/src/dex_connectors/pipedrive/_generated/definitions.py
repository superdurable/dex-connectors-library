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

CONNECTOR_ID = "pipedrive"
PROVIDER = "pipedrive"
SEARCH_OBJECTS_OPERATION_ID = "searchObjects"
GET_OBJECT_OPERATION_ID = "getObject"
LIST_OBJECTS_OPERATION_ID = "listObjects"
UPSERT_OBJECT_OPERATION_ID = "upsertObject"
CREATE_OBJECT_OPERATION_ID = "createObject"
UPDATE_OBJECT_OPERATION_ID = "updateObject"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
MULTIPLE_MATCHES = "multipleMatches"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
UNCERTAIN = "uncertain"
UPDATED = "updated"
UPSERTED = "upserted"

SEARCH_OBJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_OBJECTS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "Pipedrive returned one bounded page of matching partial records; the page may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Pipedrive conclusively rejected the search, such as a revoked token, a missing scope or an invalid term.", True),
        BranchDefinition(INVALID_RESPONSE, "Pipedrive returned an invalid or oversized search page.", True),
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
        BranchDefinition(NOT_FOUND, "No record of that object type has the ID, or the connection's user cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Pipedrive conclusively rejected the read, such as a revoked token, or a missing scope.", True),
        BranchDefinition(INVALID_RESPONSE, "Pipedrive returned an invalid or oversized record.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_OBJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_OBJECTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Pipedrive returned one bounded page of full records; the page may be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Pipedrive conclusively rejected the list, such as a revoked token, a missing scope or an expired cursor.", True),
        BranchDefinition(INVALID_RESPONSE, "Pipedrive returned an invalid or oversized page.", True),
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
        BranchDefinition(UPSERTED, "Pipedrive created the record or updated the one record that carries the identity value.", False),
        BranchDefinition(MULTIPLE_MATCHES, "More than one record carries the identity value, so the connector changed nothing; the Result lists their IDs.", True),
        BranchDefinition(PROVIDER_REJECTED, "Pipedrive conclusively rejected the search or the write, such as an invalid field value, a revoked token, or a missing scope; nothing was created.", True),
        BranchDefinition(INVALID_RESPONSE, "Pipedrive returned an invalid or oversized search page or updated record.", True),
        BranchDefinition(UNCERTAIN, "A create sent in this or an earlier attempt was not confirmed, and identity read-back finds no record or cannot complete; the record may exist, so the connector does not create it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

CREATE_OBJECT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_OBJECT_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Pipedrive created the record and returned it.", False),
        BranchDefinition(PROVIDER_REJECTED, "Pipedrive conclusively rejected the create, such as a missing title or name, an unknown stage or owner, or a revoked token; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The create was sent but its outcome cannot be confirmed, so the record may exist; the connector never sends it again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

UPDATE_OBJECT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_OBJECT_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Pipedrive updated the record and returned it.", False),
        BranchDefinition(NOT_FOUND, "No record of that object type has the ID, or the connection's user cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Pipedrive conclusively rejected the update, such as an invalid field value, an unknown stage or owner, or a revoked token.", True),
        BranchDefinition(INVALID_RESPONSE, "Pipedrive applied or rejected the update but returned an invalid or oversized record.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
