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

CONNECTOR_ID = "zhihuiya"
PROVIDER = "zhihuiya"
GET_PATENT_VALUATION_OPERATION_ID = "getPatentValuation"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_PATENT_VALUATION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PATENT_VALUATION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Zhihuiya returned one patent valuation.", False),
        BranchDefinition(NO_DATA, "Zhihuiya returned no patent valuation.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zhihuiya rejected authorization, permission, or the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Zhihuiya returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local API Key or publication number is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
