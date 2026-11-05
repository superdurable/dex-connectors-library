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

CONNECTOR_ID = "oauth2-http"
PROVIDER = "oauth2-http"
SEND_REQUEST_OPERATION_ID = "sendRequest"
COMPLETED = "completed"
DEFECT = "defect"
REJECTED = "rejected"
UNCERTAIN = "uncertain"

SEND_REQUEST_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_REQUEST_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "The API answered 2xx; the Result carries the status code and body.", False),
        BranchDefinition(REJECTED, "The API answered a redirect or a 4xx other than 408 and 429, or the connection's authorization could not be renewed. The Result carries the status code, which is zero when no request was sent.", True),
        BranchDefinition(UNCERTAIN, "The request may have reached the API, but a method that is not safe to repeat got no usable answer: the connection closed, timed out, or the API answered 5xx.", True),
        BranchDefinition(DEFECT, "The input or the connection settings are invalid, so no request was sent.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
