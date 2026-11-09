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

CONNECTOR_ID = "freshbooks"
PROVIDER = "freshbooks"
GET_INVOICE_OPERATION_ID = "getInvoice"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_INVOICE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_INVOICE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "FreshBooks returned the selected invoice.", False),
        BranchDefinition(MISSING, "FreshBooks returned no invoice for this ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "FreshBooks conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "FreshBooks returned malformed or oversized invoice JSON.", True),
        BranchDefinition(DEFECT, "The account ID, invoice ID, or OAuth credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
