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

CONNECTOR_ID = "typeform"
PROVIDER = "typeform"
LIST_FORMS_OPERATION_ID = "listForms"
GET_FORM_OPERATION_ID = "getForm"
LIST_RESPONSES_OPERATION_ID = "listResponses"
UPSERT_WEBHOOK_OPERATION_ID = "upsertWebhook"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UPSERTED = "upserted"

LIST_FORMS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_FORMS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Typeform returned one page of forms and the page count.", False),
        BranchDefinition(PROVIDER_REJECTED, "Typeform conclusively rejected the request, such as an invalid token, a missing forms:read scope, or an unknown workspace.", True),
        BranchDefinition(INVALID_RESPONSE, "Typeform returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_FORM_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_FORM_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Typeform returned the form.", False),
        BranchDefinition(NOT_FOUND, "The form does not exist or the token's account does not own it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Typeform conclusively rejected the request, such as an invalid token or a missing forms:read scope.", True),
        BranchDefinition(INVALID_RESPONSE, "Typeform returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_RESPONSES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_RESPONSES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "Typeform returned one page of responses and the total count.", False),
        BranchDefinition(NOT_FOUND, "The form does not exist or the token's account does not own it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Typeform conclusively rejected the request, such as an invalid token, a missing responses:read scope, or an invalid filter.", True),
        BranchDefinition(INVALID_RESPONSE, "Typeform returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

UPSERT_WEBHOOK_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPSERT_WEBHOOK_OPERATION_ID),
    (
        BranchDefinition(UPSERTED, "Typeform stored the webhook for this form and tag with the requested URL and enabled state.", False),
        BranchDefinition(NOT_FOUND, "The form does not exist or the token's account does not own it.", True),
        BranchDefinition(PROVIDER_REJECTED, "Typeform conclusively rejected the webhook, such as a URL that is not HTTPS, a plan without webhooks, or a token without the webhooks:write scope.", True),
        BranchDefinition(INVALID_RESPONSE, "Typeform answered 2xx with a malformed or oversized body, so it may have stored the webhook without this Result describing it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration such as a blank webhook_secret, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
