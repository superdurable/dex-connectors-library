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

CONNECTOR_ID = "bmob"
PROVIDER = "bmob"
COUNT_CLASS_OBJECTS_OPERATION_ID = "countClassObjects"
COUNTED = "counted"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

COUNT_CLASS_OBJECTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, COUNT_CLASS_OBJECTS_OPERATION_ID),
    (
        BranchDefinition(COUNTED, "Bmob returned the class object count.", False),
        BranchDefinition(PROVIDER_REJECTED, "Bmob rejected credentials, class access, or the class name.", True),
        BranchDefinition(INVALID_RESPONSE, "Bmob returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or connection configuration is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
