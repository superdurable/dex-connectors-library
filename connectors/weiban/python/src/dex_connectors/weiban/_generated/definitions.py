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

CONNECTOR_ID = "weiban"
PROVIDER = "weiban"
LIST_TAGS_OPERATION_ID = "listTags"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TAGS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TAGS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Weiban returned the first tag page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Weiban rejected the credentials or query.", True),
        BranchDefinition(INVALID_RESPONSE, "Weiban returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
