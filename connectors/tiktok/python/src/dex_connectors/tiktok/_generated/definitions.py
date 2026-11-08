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

CONNECTOR_ID = "tiktok-business"
PROVIDER = "tiktok-business"
LIST_CAMPAIGNS_OPERATION_ID = "listCampaigns"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_CAMPAIGNS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CAMPAIGNS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "TikTok returned a campaign page, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "TikTok conclusively rejected the query or token.", True),
        BranchDefinition(INVALID_RESPONSE, "TikTok returned malformed or oversized campaign JSON.", True),
        BranchDefinition(DEFECT, "Advertiser ID, pagination, or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
