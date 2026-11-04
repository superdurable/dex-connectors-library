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

CONNECTOR_ID = "notion"
PROVIDER = "notion"
SEARCH_OPERATION_ID = "search"
QUERY_DATABASE_OPERATION_ID = "queryDatabase"
GET_PAGE_OPERATION_ID = "getPage"
CREATE_PAGE_OPERATION_ID = "createPage"
UPDATE_PAGE_PROPERTIES_OPERATION_ID = "updatePageProperties"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_MATCH = "noMatch"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
QUERIED = "queried"
SEARCHED = "searched"
UNCERTAIN = "uncertain"
UPDATED = "updated"

SEARCH_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of title matches was read; Notion search matches titles only, is not guaranteed to be exhaustive, and indexes new pages with a delay.", False),
        BranchDefinition(NO_MATCH, "The first page of results is empty: no shared page or data source title contains the query, or the object was never shared with the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Notion conclusively rejected the search, such as an invalid token, a stale cursor, or a blocked connection.", True),
        BranchDefinition(INVALID_RESPONSE, "Notion returned an invalid or oversized search page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

QUERY_DATABASE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, QUERY_DATABASE_OPERATION_ID),
    (
        BranchDefinition(QUERIED, "One page of matching rows was read; it can be empty.", False),
        BranchDefinition(NOT_FOUND, "The database or data source does not exist or is not shared with the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Notion conclusively rejected the query, such as a filter on a missing property or a type mismatch.", True),
        BranchDefinition(INVALID_RESPONSE, "Notion returned an invalid or oversized query page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid, including a database ID whose database holds several data sources.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_PAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PAGE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The page and its bounded content were read.", False),
        BranchDefinition(NOT_FOUND, "The page does not exist or is not shared with the connection.", True),
        BranchDefinition(PROVIDER_REJECTED, "Notion conclusively rejected the page read.", True),
        BranchDefinition(INVALID_RESPONSE, "Notion returned an invalid or oversized page or block list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_PAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_PAGE_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Notion created the page and returned or confirmed its ID.", False),
        BranchDefinition(NOT_FOUND, "The database or data source does not exist or is not shared with the connection; nothing was created.", True),
        BranchDefinition(PROVIDER_REJECTED, "Notion conclusively rejected the page, such as a missing property, a value of the wrong type, missing Insert content capability, or a workspace limit; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The request was dispatched but its outcome cannot be confirmed; Notion has no idempotency key, so the page may exist and must not be created again automatically.", True),
        BranchDefinition(INVALID_RESPONSE, "Notion returned an invalid or oversized database while the connector resolved its data source; nothing was created.", True),
        BranchDefinition(DEFECT, "Local input, credential authorization, or connector definition is invalid, including a database ID whose database holds several data sources.", True),
    ),
    StepDefaults(
        timedelta(seconds=120),
        1,
        heartbeat_timeout=timedelta(seconds=120),
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
)

UPDATE_PAGE_PROPERTIES_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_PAGE_PROPERTIES_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Notion applied the property values and returned the page.", False),
        BranchDefinition(NOT_FOUND, "The page does not exist or is not shared with the connection; nothing was changed.", True),
        BranchDefinition(PROVIDER_REJECTED, "Notion conclusively rejected the update, such as a missing property, a value of the wrong type, a locked or archived page, or missing Update content capability.", True),
        BranchDefinition(INVALID_RESPONSE, "Notion applied the update but returned an invalid or oversized page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=90),
        2,
        heartbeat_timeout=timedelta(seconds=90),
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
