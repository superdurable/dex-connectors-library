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

CONNECTOR_ID = "whatsapp-business"
PROVIDER = "whatsapp-business"
LIST_PHONE_NUMBERS_OPERATION_ID = "listPhoneNumbers"
SEND_TEXT_MESSAGE_OPERATION_ID = "sendTextMessage"
SEND_TEMPLATE_MESSAGE_OPERATION_ID = "sendTemplateMessage"
DEFECT = "defect"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
SENT = "sent"
UNCERTAIN = "uncertain"

LIST_PHONE_NUMBERS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PHONE_NUMBERS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Meta returned one phone-number page.", False),
        BranchDefinition(NOT_FOUND, "The WABA is missing or inaccessible.", True),
        BranchDefinition(PROVIDER_REJECTED, "Meta conclusively rejected the lookup.", True),
        BranchDefinition(INVALID_RESPONSE, "Meta returned malformed or oversized JSON.", True),
        BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEND_TEXT_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_TEXT_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Meta returned a WhatsApp message ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Meta definitively rejected the message.", True),
        BranchDefinition(UNCERTAIN, "The send outcome cannot be confirmed; do not resend blindly.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEND_TEMPLATE_MESSAGE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_TEMPLATE_MESSAGE_OPERATION_ID),
    (
        BranchDefinition(SENT, "Meta returned a WhatsApp message ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Meta definitively rejected the template send.", True),
        BranchDefinition(UNCERTAIN, "The send outcome cannot be confirmed; do not resend blindly.", True),
        BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
