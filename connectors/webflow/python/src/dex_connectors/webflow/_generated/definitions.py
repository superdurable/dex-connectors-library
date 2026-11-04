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

CONNECTOR_ID = "webflow"
PROVIDER = "webflow"
LIST_COLLECTION_ITEMS_OPERATION_ID = "listCollectionItems"
GET_COLLECTION_ITEM_OPERATION_ID = "getCollectionItem"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_COLLECTION_ITEMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_COLLECTION_ITEMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "List a page of staged CMS collection items.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_COLLECTION_ITEM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_COLLECTION_ITEM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Get one staged CMS collection item.", False),
        BranchDefinition(NOT_FOUND, "The requested provider resource does not exist or is not visible.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input, configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
