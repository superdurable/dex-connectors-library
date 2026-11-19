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

CONNECTOR_ID = "yunda"
PROVIDER = "yunda"
GET_WAYBILL_BALANCE_OPERATION_ID = "getWaybillBalance"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_WAYBILL_BALANCE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_WAYBILL_BALANCE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Yunda returned the Baima waybill balance.", False),
        BranchDefinition(PROVIDER_REJECTED, "Yunda rejected credentials or the balance query.", True),
        BranchDefinition(INVALID_RESPONSE, "Yunda returned malformed or oversized balance data.", True),
        BranchDefinition(DEFECT, "Local credentials are missing or invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
