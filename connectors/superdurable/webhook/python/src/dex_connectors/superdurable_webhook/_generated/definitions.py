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

CONNECTOR_ID = "webhook"
PROVIDER = "webhook"
SEND_EVENT_OPERATION_ID = "sendEvent"
DEFECT = "defect"
DELIVERED = "delivered"
REJECTED = "rejected"

SEND_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_EVENT_OPERATION_ID),
    (
        BranchDefinition(DELIVERED, "The receiver answered 2xx and accepted the event.", False),
        BranchDefinition(REJECTED, "On the Step's first attempt the receiver answered 400, 401, 403, 404, 411, 413, 414, 415, or 422, so it refused the event; the Result carries the status code. Any other status, and a refusal on a later attempt that may follow a delivered event, is retried with the same webhook-id and fails the Flow when the retry budget runs out.", True),
        BranchDefinition(DEFECT, "On the Step's first attempt the connection has no valid deliveryUrl or signing secret, or the payload is not one JSON value within maxBodyBytes. A later attempt retries instead.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
