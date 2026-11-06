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

CONNECTOR_ID = "confluence"
PROVIDER = "atlassian"
SEARCH_PAGES_OPERATION_ID = "searchPages"
GET_PAGE_OPERATION_ID = "getPage"
CREATE_PAGE_OPERATION_ID = "createPage"
UPDATE_PAGE_OPERATION_ID = "updatePage"
ADD_COMMENT_OPERATION_ID = "addComment"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SEARCHED = "searched"
TITLE_CONFLICT = "titleConflict"
UNCERTAIN = "uncertain"
UPDATED = "updated"
VERSION_CONFLICT = "versionConflict"

SEARCH_PAGES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_PAGES_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "One page of matching pages was read; Confluence search is eventually consistent, so a page created or changed seconds ago can be missing.", False),
        BranchDefinition(PROVIDER_REJECTED, "Confluence conclusively rejected the search, such as unparsable CQL, an expired cursor, or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Confluence returned an invalid or oversized search page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(FOUND, "The page was read.", False),
        BranchDefinition(NOT_FOUND, "The page does not exist or the connection cannot view it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Confluence conclusively rejected the page read.", True),
        BranchDefinition(INVALID_RESPONSE, "Confluence returned an invalid or oversized page.", True),
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
        BranchDefinition(CREATED, "The page exists with the requested title, created by this Step execution, including a create an earlier attempt sent.", False),
        BranchDefinition(TITLE_CONFLICT, "Another page in the space already has this title, so nothing was created; the Value names that page and its current version.", True),
        BranchDefinition(NOT_FOUND, "The space or parent page does not exist or the connection cannot view it; nothing was created.", True),
        BranchDefinition(PROVIDER_REJECTED, "Confluence conclusively rejected the page, such as an invalid body or missing Add page permission; nothing was created.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPDATE_PAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_PAGE_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "The page is at the requested version with this Step execution's change, including one an earlier attempt sent.", False),
        BranchDefinition(VERSION_CONFLICT, "The page is no longer at the version before the requested one, because another edit was saved first; nothing was changed and the Value names the current version.", True),
        BranchDefinition(NOT_FOUND, "The page does not exist or the connection cannot view it; nothing was changed.", True),
        BranchDefinition(PROVIDER_REJECTED, "Confluence conclusively rejected the update, such as an invalid body, missing Edit permission, or a space that requires approval before publishing; nothing was changed.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_COMMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_COMMENT_OPERATION_ID),
    (
        BranchDefinition(ADDED, "The comment is on the page, including one an earlier attempt of this Step execution sent.", False),
        BranchDefinition(NOT_FOUND, "The page does not exist or the connection cannot view it; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Confluence conclusively rejected the comment, such as an invalid body or missing Add comments permission; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "An earlier attempt sent the comment and the page's comments do not confirm it; Confluence has no idempotency key, so it is never sent again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
