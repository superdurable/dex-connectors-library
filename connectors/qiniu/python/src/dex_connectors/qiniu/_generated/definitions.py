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

CONNECTOR_ID = "qiniu"
PROVIDER = "qiniu"
LIST_BUCKETS_OPERATION_ID = "listBuckets"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_BUCKETS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_BUCKETS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Qiniu returned zero or more bucket names.", False),
        BranchDefinition(PROVIDER_REJECTED, "Qiniu rejected the credentials or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Qiniu returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Connection credentials or local configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
