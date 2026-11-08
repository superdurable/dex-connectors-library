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

CONNECTOR_ID = "productboard"
PROVIDER = "productboard"
LIST_FEATURES_OPERATION_ID = "listFeatures"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_FEATURES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_FEATURES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Productboard returned one feature page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Productboard conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "Productboard returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
