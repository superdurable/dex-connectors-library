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

CONNECTOR_ID = "baishancloud"
PROVIDER = "baishancloud"
DESCRIBE_DOMAIN_OPERATION_ID = "describeDomain"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

DESCRIBE_DOMAIN_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DESCRIBE_DOMAIN_OPERATION_ID),
    (
        BranchDefinition(FOUND, "BaishanCloud returned the requested CDN domain.", False),
        BranchDefinition(NOT_FOUND, "The authorized response has no matching domain.", True),
        BranchDefinition(PROVIDER_REJECTED, "BaishanCloud rejected the token, access, or domain.", True),
        BranchDefinition(INVALID_RESPONSE, "BaishanCloud returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local connection configuration or input is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
