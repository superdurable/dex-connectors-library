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
        BranchDefinition(REJECTED, "The receiver answered a redirect or a 4xx other than 408, 409, 425, and 429, so it refused the event; the Result carries the status code. A 409 is retried with the same webhook-id instead, and fails the Flow when the retry budget runs out.", True),
        BranchDefinition(DEFECT, "The connection has no valid deliveryUrl or signing secret, or the payload is not one JSON value within maxBodyBytes.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
