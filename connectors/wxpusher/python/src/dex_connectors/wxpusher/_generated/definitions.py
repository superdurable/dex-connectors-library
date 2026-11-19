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

CONNECTOR_ID = "wxpusher"
PROVIDER = "wxpusher"
GET_SUBSCRIBER_COUNT_OPERATION_ID = "getSubscriberCount"
COUNTED = "counted"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

GET_SUBSCRIBER_COUNT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_SUBSCRIBER_COUNT_OPERATION_ID),
    (
        BranchDefinition(COUNTED, "WxPusher returned the subscriber-record count.", False),
        BranchDefinition(PROVIDER_REJECTED, "WxPusher rejected the token or query.", True),
        BranchDefinition(INVALID_RESPONSE, "WxPusher returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "The connection is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
