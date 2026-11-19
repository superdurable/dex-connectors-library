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

CONNECTOR_ID = "unisound"
PROVIDER = "unisound"
LIST_SYSTEM_VOICES_OPERATION_ID = "listSystemVoices"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"

LIST_SYSTEM_VOICES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_SYSTEM_VOICES_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Unisound returned the system voice catalog.", False),
        BranchDefinition(PROVIDER_REJECTED, "Unisound rejected the Key or request.", True),
        BranchDefinition(INVALID_RESPONSE, "Unisound returned malformed or oversized voice data.", True),
        BranchDefinition(DEFECT, "The local API Key is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
