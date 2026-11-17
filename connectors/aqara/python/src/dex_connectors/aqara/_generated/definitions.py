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

CONNECTOR_ID = "aqara"
PROVIDER = "aqara"
LIST_DEVICES_OPERATION_ID = "listDevices"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_DEVICES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DEVICES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Aqara returned one page of devices, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Aqara rejected authorization, permission, or device query.", True),
        BranchDefinition(INVALID_RESPONSE, "Aqara returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
