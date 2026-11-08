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

CONNECTOR_ID = "adobe-acrobat-sign"
PROVIDER = "adobe-acrobat-sign"
GET_AGREEMENT_OPERATION_ID = "getAgreement"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_AGREEMENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_AGREEMENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Acrobat Sign returned the agreement.", False),
        BranchDefinition(NOT_FOUND, "Acrobat Sign did not expose the agreement.", True),
        BranchDefinition(PROVIDER_REJECTED, "Acrobat Sign conclusively rejected discovery or lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Acrobat Sign returned malformed, unsafe, or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
