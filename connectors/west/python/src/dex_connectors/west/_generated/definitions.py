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

CONNECTOR_ID = "west"
PROVIDER = "west"
LIST_DOMAINS_OPERATION_ID = "listDomains"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DOMAINS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DOMAINS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "West.cn returned a domain page.", False),
        BranchDefinition(PROVIDER_REJECTED, "West.cn rejected credentials or reseller access.", True),
        BranchDefinition(INVALID_RESPONSE, "West.cn returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or page options are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
