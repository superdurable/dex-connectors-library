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

CONNECTOR_ID = "klaviyo"
PROVIDER = "klaviyo"
CREATE_EVENT_OPERATION_ID = "createEvent"
GET_PROFILE_OPERATION_ID = "getProfile"
ACCEPTED = "accepted"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

CREATE_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_EVENT_OPERATION_ID),
    (
        BranchDefinition(ACCEPTED, "Klaviyo accepted the deduplicated event for asynchronous processing.", False),
        BranchDefinition(PROVIDER_REJECTED, "Klaviyo rejected the event or key.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_PROFILE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PROFILE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Klaviyo returned the profile.", False),
        BranchDefinition(NOT_FOUND, "No profile has that ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Klaviyo rejected the request or key.", True),
        BranchDefinition(INVALID_RESPONSE, "Klaviyo returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
