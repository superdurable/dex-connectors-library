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

CONNECTOR_ID = "chinaz"
PROVIDER = "chinaz"
GET_SITE_TDK_OPERATION_ID = "getSiteTDK"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_SITE_TDK_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SITE_TDK_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Chinaz returned TDK metadata for the domain.", False),
        BranchDefinition(NOT_FOUND, "Chinaz could not find site metadata for the domain.", True),
        BranchDefinition(PROVIDER_REJECTED, "Chinaz rejected the key, plan, or domain.", True),
        BranchDefinition(INVALID_RESPONSE, "Chinaz returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration, key, or domain is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
