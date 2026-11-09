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

CONNECTOR_ID = "surveymonkey"
PROVIDER = "surveymonkey"
LIST_SURVEYS_OPERATION_ID = "listSurveys"
GET_RESPONSE_OPERATION_ID = "getResponse"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_SURVEYS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SURVEYS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "SurveyMonkey returned the selected survey page.", False),
        BranchDefinition(PROVIDER_REJECTED, "SurveyMonkey conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "SurveyMonkey returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_RESPONSE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_RESPONSE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "SurveyMonkey returned response metadata.", False),
        BranchDefinition(NOT_FOUND, "The survey response does not exist or is inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "SurveyMonkey conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "SurveyMonkey returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
