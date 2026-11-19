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

CONNECTOR_ID = "pingpp"
PROVIDER = "pingpp"
GET_CHARGE_STATUS_OPERATION_ID = "getChargeStatus"
DEFECT = "defect"
FETCHED = "fetched"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_CHARGE_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CHARGE_STATUS_OPERATION_ID),
    (
        BranchDefinition(FETCHED, "Ping++ returned the Charge state.", False),
        BranchDefinition(NOT_FOUND, "The Charge ID does not exist for this API Key.", True),
        BranchDefinition(PROVIDER_REJECTED, "Ping++ rejected the API Key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Ping++ returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local Charge ID or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
