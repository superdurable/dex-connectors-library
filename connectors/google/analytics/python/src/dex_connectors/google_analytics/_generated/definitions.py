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

CONNECTOR_ID = "google-analytics-4"
PROVIDER = "google-analytics-4"
RUN_REPORT_OPERATION_ID = "runReport"
GET_PROPERTY_OPERATION_ID = "getProperty"
LIST_AUDIENCES_OPERATION_ID = "listAudiences"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
REPORTED = "reported"

RUN_REPORT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, RUN_REPORT_OPERATION_ID),
    (
        BranchDefinition(REPORTED, "Google returned a report page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the report request.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized report JSON.", True),
        BranchDefinition(DEFECT, "Local report input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_PROPERTY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PROPERTY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Google returned property metadata.", False),
        BranchDefinition(NOT_FOUND, "The property does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized property JSON.", True),
        BranchDefinition(DEFECT, "Local property ID or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_AUDIENCES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_AUDIENCES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google returned an audience page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Google definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned malformed or oversized audience JSON.", True),
        BranchDefinition(DEFECT, "Local page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
