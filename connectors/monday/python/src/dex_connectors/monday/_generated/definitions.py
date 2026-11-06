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

CONNECTOR_ID = "monday"
PROVIDER = "monday"
LIST_ITEMS_OPERATION_ID = "listItems"
GET_ITEM_OPERATION_ID = "getItem"
CREATE_ITEM_OPERATION_ID = "createItem"
UPDATE_ITEM_COLUMN_VALUES_OPERATION_ID = "updateItemColumnValues"
ADD_UPDATE_OPERATION_ID = "addUpdate"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"
UPDATED = "updated"

LIST_ITEMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ITEMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of the board's active items that match the filter was read; it can be empty.", False),
        BranchDefinition(NOT_FOUND, "The board does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "monday.com conclusively rejected the read, such as a filter on a missing column, an expired cursor, missing permission, or an exhausted daily call limit.", True),
        BranchDefinition(INVALID_RESPONSE, "monday.com returned an invalid or oversized page of items.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_ITEM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ITEM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The item was read.", False),
        BranchDefinition(NOT_FOUND, "The item does not exist, the connection cannot see it, or it is archived or deleted and inactive items were not requested.", True),
        BranchDefinition(PROVIDER_REJECTED, "monday.com conclusively rejected the item read.", True),
        BranchDefinition(INVALID_RESPONSE, "monday.com returned an invalid or oversized item.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_ITEM_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_ITEM_OPERATION_ID),
    (
        BranchDefinition(CREATED, "monday.com created the item, or replayed the item this Step already created, and returned it.", False),
        BranchDefinition(NOT_FOUND, "The board or group does not exist or the connection cannot see it; nothing was created.", True),
        BranchDefinition(PROVIDER_REJECTED, "monday.com conclusively rejected the item, such as an invalid column value, a missing status label, the board's item limit, or missing permission; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "An earlier create may have been accepted, but its result cannot be confirmed; the item must not be created again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        heartbeat_timeout=timedelta(seconds=20),
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 10, timedelta(seconds=300)),
    ),
)

UPDATE_ITEM_COLUMN_VALUES_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_ITEM_COLUMN_VALUES_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "monday.com set the column values and returned the item.", False),
        BranchDefinition(NOT_FOUND, "The board or item does not exist or the connection cannot see it; nothing was changed.", True),
        BranchDefinition(PROVIDER_REJECTED, "monday.com conclusively rejected the change, such as an invalid column value, a missing status label, or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "monday.com returned an invalid or oversized item, so the values may have been set; repeating the Step is safe.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_UPDATE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_UPDATE_OPERATION_ID),
    (
        BranchDefinition(ADDED, "monday.com added the update, or replayed the update this Step already added, and returned it.", False),
        BranchDefinition(NOT_FOUND, "The item does not exist or the connection cannot see it; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "monday.com conclusively rejected the update, such as missing permission; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "An earlier update may have been accepted, but its result cannot be confirmed; the update must not be added again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        heartbeat_timeout=timedelta(seconds=20),
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 10, timedelta(seconds=300)),
    ),
)
