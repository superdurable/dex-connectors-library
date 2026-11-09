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

CONNECTOR_ID = "google-tag-manager"
PROVIDER = "google-tag-manager"
LIST_ACCOUNTS_OPERATION_ID = "listAccounts"
LIST_CONTAINERS_OPERATION_ID = "listContainers"
LIST_TAGS_OPERATION_ID = "listTags"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_ACCOUNTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ACCOUNTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned an account page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized account JSON.", True),
        BranchDefinition(DEFECT, "Local page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_CONTAINERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONTAINERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned a container page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized container JSON.", True),
        BranchDefinition(DEFECT, "Local account or page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_TAGS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TAGS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned a tag page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized tag JSON.", True),
        BranchDefinition(DEFECT, "Local workspace or page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
