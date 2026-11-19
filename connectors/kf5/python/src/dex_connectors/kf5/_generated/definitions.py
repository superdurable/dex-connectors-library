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

CONNECTOR_ID = "kf5"
PROVIDER = "kf5"
GET_TICKET_OPERATION_ID = "getTicket"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NO_DATA = "noData"
PROVIDER_REJECTED = "providerRejected"

GET_TICKET_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_TICKET_OPERATION_ID),
    (
        BranchDefinition(FOUND, "KF5 returned ticket status metadata.", False),
        BranchDefinition(NO_DATA, "KF5 did not find the ticket.", True),
        BranchDefinition(PROVIDER_REJECTED, "KF5 rejected authorization, permission, or the request.", True),
        BranchDefinition(INVALID_RESPONSE, "KF5 returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The local tenant, credentials, or ticket ID is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
