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

CONNECTOR_ID = "xiaoetech"
PROVIDER = "xiaoetech"
LIST_ROOT_GOODS_CATEGORIES_OPERATION_ID = "listRootGoodsCategories"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_ROOT_GOODS_CATEGORIES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ROOT_GOODS_CATEGORIES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "XiaoeTech returned the root goods categories.", False),
        BranchDefinition(PROVIDER_REJECTED, "XiaoeTech rejected credentials, permissions, or the request.", True),
        BranchDefinition(INVALID_RESPONSE, "XiaoeTech returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or connection configuration are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=3600)),
    ),
)
