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

CONNECTOR_ID = "elevenlabs"
PROVIDER = "elevenlabs"
LIST_VOICES_OPERATION_ID = "listVoices"
CREATE_SPEECH_OPERATION_ID = "createSpeech"
CREATED = "created"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_VOICES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_VOICES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "ElevenLabs returned the requested result.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_SPEECH_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, CREATE_SPEECH_OPERATION_ID),
    (
        BranchDefinition(CREATED, "ElevenLabs returned the requested result.", False),
        BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=150),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(seconds=300)),
    ),
)
