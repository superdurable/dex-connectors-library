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

CONNECTOR_ID = "udesk"
PROVIDER = "udesk"
LIST_TICKET_STATUSES_OPERATION_ID = "listTicketStatuses"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

LIST_TICKET_STATUSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TICKET_STATUSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Udesk returned zero or more ticket statuses.", False),
        BranchDefinition(PROVIDER_REJECTED, "Udesk rejected authentication or the query.", True),
        BranchDefinition(INVALID_RESPONSE, "Udesk returned malformed or oversized data.", True),
        BranchDefinition(DEFECT, "Input or connection settings are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
