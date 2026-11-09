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

CONNECTOR_ID = "microsoft-advertising"
PROVIDER = "microsoft-advertising"
LIST_CAMPAIGNS_OPERATION_ID = "listCampaigns"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_CAMPAIGNS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CAMPAIGNS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Microsoft Advertising returned the account's campaigns.", False),
        BranchDefinition(PROVIDER_REJECTED, "Microsoft Advertising conclusively rejected the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Microsoft Advertising returned malformed or oversized campaign JSON.", True),
        BranchDefinition(DEFECT, "The account ID or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
