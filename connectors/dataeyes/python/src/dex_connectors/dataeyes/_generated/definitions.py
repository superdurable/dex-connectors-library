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

CONNECTOR_ID = "dataeyes"
PROVIDER = "dataeyes"
READ_WEB_PAGE_OPERATION_ID = "readWebPage"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

READ_WEB_PAGE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, READ_WEB_PAGE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "DataEyes returned the public page content.", False),
        BranchDefinition(PROVIDER_REJECTED, "DataEyes rejected the Key or could not read this page.", True),
        BranchDefinition(INVALID_RESPONSE, "DataEyes returned malformed or oversized page data.", True),
        BranchDefinition(DEFECT, "The local URL or API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=10), 2, timedelta(seconds=60)),
    ),
)
