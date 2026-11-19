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

CONNECTOR_ID = "variflight"
PROVIDER = "variflight"
GET_FLIGHT_STATUS_OPERATION_ID = "getFlightStatus"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_FLIGHT_STATUS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FLIGHT_STATUS_OPERATION_ID),
    (
        BranchDefinition(FOUND, "VariFlight returned flight status records.", False),
        BranchDefinition(NO_DATA, "VariFlight has no matching flight data.", True),
        BranchDefinition(PROVIDER_REJECTED, "VariFlight rejected authentication, permission, or input.", True),
        BranchDefinition(INVALID_RESPONSE, "VariFlight returned malformed or oversized flight data.", True),
        BranchDefinition(DEFECT, "Local credentials, flight number, or date are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=20), 3, timedelta(seconds=300)),
    ),
)
