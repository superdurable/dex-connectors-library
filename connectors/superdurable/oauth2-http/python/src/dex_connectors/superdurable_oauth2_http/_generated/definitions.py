# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RepeatOnUnknown,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "oauth2-http"
PROVIDER = "oauth2-http"
SEND_REQUEST_OPERATION_ID = "sendRequest"
POST_NON_IDEMPOTENT_OPERATION_ID = "postNonIdempotent"
COMPLETED = "completed"
DEFECT = "defect"
REJECTED = "rejected"
UNCERTAIN = "uncertain"

SEND_REQUEST_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_REQUEST_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "The API answered 2xx; the Result carries the status code and body.", False),
        BranchDefinition(REJECTED, "The API answered a redirect or a definite refusal (400, 401, 403, 404, 411, 413, 414, 415, or 422), or the connection's authorization could not be renewed. The Result carries the status code, which is zero when no request was sent.", True),
        BranchDefinition(UNCERTAIN, "A POST without idempotencyKeyHeader was sent, but its answer was lost or cut off, so the API may have processed it; it is not repeated. Every other unknown outcome is sent again.", True),
        BranchDefinition(DEFECT, "The input or the connection settings are invalid, so no request was sent.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
    repeat_on_unknown=RepeatOnUnknown.SAFE,
)

POST_NON_IDEMPOTENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, POST_NON_IDEMPOTENT_OPERATION_ID),
    (
        BranchDefinition(COMPLETED, "The API answered 2xx; the Result carries the status code and body.", False),
        BranchDefinition(REJECTED, "The API refused the only POST of this Step execution with a definite refusal (400, 401, 403, 404, 411, 413, 414, 415, or 422), or the connection's authorization could not be renewed before it was sent. The Result carries the status code, which is zero when no request was sent.", True),
        BranchDefinition(UNCERTAIN, "The POST may have reached the API, but no usable answer arrived: the connection closed or timed out, the API answered a redirect, a 5xx other than 503, or another status that does not prove a refusal, or an earlier attempt may have sent it. It is never sent again; check the API before acting.", True),
        BranchDefinition(DEFECT, "The input or the connection settings are invalid, so no request was sent.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
    repeat_on_unknown=RepeatOnUnknown.UNSAFE,
)
