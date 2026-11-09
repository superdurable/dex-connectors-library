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

CONNECTOR_ID = "posthog"
PROVIDER = "posthog"
CAPTURE_EVENT_OPERATION_ID = "captureEvent"
LIST_INSIGHTS_OPERATION_ID = "listInsights"
ACCEPTED = "accepted"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"
QUOTA_LIMITED = "quotaLimited"

CAPTURE_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CAPTURE_EVENT_OPERATION_ID),
    (
        BranchDefinition(ACCEPTED, "PostHog accepted the event for processing, without confirming ingestion.", False),
        BranchDefinition(QUOTA_LIMITED, "PostHog reported a billing quota limit and may not ingest the event.", True),
        BranchDefinition(PROVIDER_REJECTED, "PostHog conclusively rejected the request.", True),
        BranchDefinition(DEFECT, "Local event input or project token is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_INSIGHTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_INSIGHTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "PostHog returned the insight page, possibly empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "PostHog denied the query or project access.", True),
        BranchDefinition(INVALID_RESPONSE, "PostHog returned malformed or oversized page JSON.", True),
        BranchDefinition(DEFECT, "Local project ID, page input, or personal API key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
