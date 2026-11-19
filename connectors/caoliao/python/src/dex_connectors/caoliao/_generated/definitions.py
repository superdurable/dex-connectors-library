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

CONNECTOR_ID = "caoliao"
PROVIDER = "caoliao"
LIST_QR_CODES_OPERATION_ID = "listQRCodes"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_QR_CODES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_QR_CODES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Caoliao returned a QR-code page.", False),
        BranchDefinition(PROVIDER_REJECTED, "Caoliao rejected credentials, access, or pagination input.", True),
        BranchDefinition(INVALID_RESPONSE, "Caoliao returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Local credentials or pagination input are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 3, timedelta(seconds=180)),
    ),
)
