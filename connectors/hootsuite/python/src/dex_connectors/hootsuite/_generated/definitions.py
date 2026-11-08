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

CONNECTOR_ID = "hootsuite"
PROVIDER = "hootsuite"
LIST_SOCIAL_PROFILES_OPERATION_ID = "listSocialProfiles"
LIST_SCHEDULED_POSTS_OPERATION_ID = "listScheduledPosts"
SCHEDULE_POST_OPERATION_ID = "schedulePost"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"
SCHEDULED = "scheduled"
UNCERTAIN = "uncertain"

LIST_SOCIAL_PROFILES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SOCIAL_PROFILES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Hootsuite returned the visible profiles.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hootsuite definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Hootsuite returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_SCHEDULED_POSTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SCHEDULED_POSTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Hootsuite returned one scheduled message page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hootsuite definitively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Hootsuite returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SCHEDULE_POST_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SCHEDULE_POST_OPERATION_ID),
    (
        BranchDefinition(SCHEDULED, "Hootsuite accepted the scheduled message and returned its ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Hootsuite definitively rejected the schedule request.", True),
        BranchDefinition(UNCERTAIN, "The request may have reached Hootsuite but no message ID was confirmed; inspect Planner before resending.", True),
        BranchDefinition(DEFECT, "Local post input, credentials, or dispatch checkpoint is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
