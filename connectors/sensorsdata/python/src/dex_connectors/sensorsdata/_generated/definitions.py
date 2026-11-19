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

CONNECTOR_ID = "sensorsdata"
PROVIDER = "sensorsdata"
LIST_CAMPAIGNS_OPERATION_ID = "listCampaigns"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_CAMPAIGNS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CAMPAIGNS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Sensors Data returned a campaign page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Sensors Data denied the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Sensors Data returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
