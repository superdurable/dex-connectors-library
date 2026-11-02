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

CONNECTOR_ID = "pagerduty"
PROVIDER = "pagerduty"
LIST_INCIDENTS_OPERATION_ID = "listIncidents"
GET_INCIDENT_OPERATION_ID = "getIncident"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_INCIDENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_INCIDENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "PagerDuty returned the requested result.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_INCIDENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_INCIDENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "PagerDuty returned the requested result.", False),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
