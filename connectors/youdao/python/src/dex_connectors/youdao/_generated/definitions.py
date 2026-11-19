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

CONNECTOR_ID = "youdao"
PROVIDER = "youdao"
TRANSLATE_TEXT_OPERATION_ID = "translateText"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
PROVIDER_REJECTED = "providerRejected"
TRANSLATED = "translated"

TRANSLATE_TEXT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, TRANSLATE_TEXT_OPERATION_ID),
    (
        BranchDefinition(TRANSLATED, "Youdao returned a translation.", False),
        BranchDefinition(PROVIDER_REJECTED, "Youdao conclusively rejected the translation.", True),
        BranchDefinition(INVALID_RESPONSE, "Youdao returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local text, language, or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=20), 3, timedelta(seconds=300)),
    ),
)
