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

CONNECTOR_ID = "mailgun"
PROVIDER = "mailgun"
GET_EVENTS_OPERATION_ID = "getEvents"
LIST_DOMAINS_OPERATION_ID = "listDomains"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"

GET_EVENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_EVENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Mailgun returned the requested result.", False),
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

LIST_DOMAINS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_DOMAINS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Mailgun returned the requested result.", False),
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
