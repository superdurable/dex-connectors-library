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

CONNECTOR_ID = "okki"
PROVIDER = "okki"
LIST_CUSTOMER_FIELDS_OPERATION_ID = "listCustomerFields"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_CUSTOMER_FIELDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CUSTOMER_FIELDS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "OKKI returned the customer field definitions.", False),
        BranchDefinition(PROVIDER_REJECTED, "OKKI conclusively rejected the query or authorization.", True),
        BranchDefinition(INVALID_RESPONSE, "OKKI returned malformed or oversized field JSON.", True),
        BranchDefinition(DEFECT, "The local connection credentials are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
