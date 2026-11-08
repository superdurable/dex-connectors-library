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

CONNECTOR_ID = "odoo"
PROVIDER = "odoo"
SEARCH_PARTNERS_OPERATION_ID = "searchPartners"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

SEARCH_PARTNERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_PARTNERS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Odoo returned a bounded list of matching company partners.", False),
        BranchDefinition(PROVIDER_REJECTED, "Odoo conclusively rejected the search.", True),
        BranchDefinition(INVALID_RESPONSE, "Odoo returned malformed or oversized partner data.", True),
        BranchDefinition(DEFECT, "Local search input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
