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

CONNECTOR_ID = "sage-intacct"
PROVIDER = "sage-intacct"
GET_INVOICE_OPERATION_ID = "getInvoice"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_INVOICE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_INVOICE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Sage Intacct returned the requested invoice.", False),
        BranchDefinition(NOT_FOUND, "No accessible invoice has this system key.", True),
        BranchDefinition(PROVIDER_REJECTED, "Sage Intacct conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Sage Intacct returned malformed or oversized invoice JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
