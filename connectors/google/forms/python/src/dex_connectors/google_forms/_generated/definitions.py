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

CONNECTOR_ID = "google-forms"
PROVIDER = "google"
GET_FORM_OPERATION_ID = "getForm"
LIST_RESPONSES_OPERATION_ID = "listResponses"
GET_RESPONSE_OPERATION_ID = "getResponse"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

GET_FORM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FORM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Google Forms returned the form.", False),
        BranchDefinition(NOT_FOUND, "The form does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the read, such as a revoked grant or an account that cannot open the form.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized form.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_RESPONSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_RESPONSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Google Forms returned one page of responses, which can be empty.", False),
        BranchDefinition(NOT_FOUND, "The form does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the listing, such as an account that cannot read the form's responses or an expired page token.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized page of responses.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
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
        BranchDefinition(FOUND, "Google Forms returned the response.", False),
        BranchDefinition(NOT_FOUND, "The form or the response does not exist or the connection cannot see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Google conclusively rejected the read, such as an account that cannot read the form's responses.", True),
        BranchDefinition(INVALID_RESPONSE, "Google returned an invalid or oversized response record.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
