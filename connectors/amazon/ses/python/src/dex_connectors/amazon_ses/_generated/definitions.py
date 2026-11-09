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

CONNECTOR_ID = "amazon-ses"
PROVIDER = "amazon-ses"
GET_EMAIL_IDENTITY_OPERATION_ID = "getEmailIdentity"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
MISSING = "missing"
PROVIDER_REJECTED = "providerRejected"

GET_EMAIL_IDENTITY_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_EMAIL_IDENTITY_OPERATION_ID),
    (
        BranchDefinition(FOUND, "AWS returned the identity verification status.", False),
        BranchDefinition(MISSING, "The identity does not exist in the selected Region.", True),
        BranchDefinition(PROVIDER_REJECTED, "AWS conclusively rejected the request or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "AWS returned malformed or oversized identity metadata.", True),
        BranchDefinition(DEFECT, "The identity, endpoint, Region, or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
