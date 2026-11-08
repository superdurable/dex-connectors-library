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

CONNECTOR_ID = "activecampaign"
PROVIDER = "activecampaign"
LIST_CONTACTS_OPERATION_ID = "listContacts"
GET_CONTACT_OPERATION_ID = "getContact"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

LIST_CONTACTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONTACTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "ActiveCampaign returned the selected contact page.", False),
        BranchDefinition(PROVIDER_REJECTED, "ActiveCampaign conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "ActiveCampaign returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_CONTACT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CONTACT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "ActiveCampaign returned contact metadata.", False),
        BranchDefinition(NOT_FOUND, "No contact with this ID is visible to the API user.", True),
        BranchDefinition(PROVIDER_REJECTED, "ActiveCampaign conclusively rejected the request.", True),
        BranchDefinition(INVALID_RESPONSE, "ActiveCampaign returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
