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

CONNECTOR_ID = "qichacha"
PROVIDER = "qichacha"
GET_ENTERPRISE_BASIC_DETAILS_OPERATION_ID = "getEnterpriseBasicDetails"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_ENTERPRISE_BASIC_DETAILS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ENTERPRISE_BASIC_DETAILS_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Qichacha returned enterprise registration details.", False),
        BranchDefinition(PROVIDER_REJECTED, "Qichacha rejected the credentials, API entitlement, quota, or search term.", True),
        BranchDefinition(INVALID_RESPONSE, "Qichacha returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=20), 2, timedelta(seconds=60)),
    ),
)
