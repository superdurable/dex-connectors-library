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

CONNECTOR_ID = "google-pubsub"
PROVIDER = "google-pubsub"
LIST_TOPICS_OPERATION_ID = "listTopics"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TOPICS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TOPICS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Pub/Sub returned the selected topic page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Pub/Sub conclusively rejected the read.", True),
        BranchDefinition(INVALID_RESPONSE, "Pub/Sub returned malformed or oversized topic JSON.", True),
        BranchDefinition(DEFECT, "Local project or page input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
