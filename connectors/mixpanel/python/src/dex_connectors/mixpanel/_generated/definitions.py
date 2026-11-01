# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation identities, branches, and Step defaults."""

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

CONNECTOR_ID = "mixpanel"
PROVIDER = "mixpanel"
TRACK_EVENT_OPERATION_ID = "trackEvent"
EXPORT_EVENTS_OPERATION_ID = "exportEvents"
DEFECT = "defect"
EXPORTED = "exported"
IMPORTED = "imported"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

TRACK_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, TRACK_EVENT_OPERATION_ID),
    (
        BranchDefinition(IMPORTED, "Mixpanel accepted the event for ingestion.", False),
        BranchDefinition(PROVIDER_REJECTED, "Mixpanel rejected the event or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Mixpanel returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

EXPORT_EVENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, EXPORT_EVENTS_OPERATION_ID),
    (
        BranchDefinition(EXPORTED, "Mixpanel returned raw events, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Mixpanel rejected the export or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Mixpanel returned malformed or oversized JSONL.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
