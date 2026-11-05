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

CONNECTOR_ID = "xero"
PROVIDER = "xero"
LIST_CONTACTS_OPERATION_ID = "listContacts"
FIND_CONTACT_BY_EMAIL_OPERATION_ID = "findContactByEmail"
GET_INVOICE_OPERATION_ID = "getInvoice"
LIST_INVOICES_OPERATION_ID = "listInvoices"
CREATE_INVOICE_OPERATION_ID = "createInvoice"
RECORD_PAYMENT_OPERATION_ID = "recordPayment"
AMBIGUOUS = "ambiguous"
CREATED = "created"
DAILY_LIMIT_REACHED = "dailyLimitReached"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
RECORDED = "recorded"
UNCERTAIN = "uncertain"

LIST_CONTACTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CONTACTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of matching contacts was read; it can be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Xero conclusively rejected the read, such as a revoked connection, a missing accounting.contacts.read scope, or a query over Xero's volume threshold.", True),
        BranchDefinition(DAILY_LIMIT_REACHED, "Xero's daily API limit for the organisation is exhausted; the Receipt carries retryAfterSeconds until it resets.", True),
        BranchDefinition(INVALID_RESPONSE, "Xero returned an invalid or oversized page of contacts.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

FIND_CONTACT_BY_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CONTACT_BY_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Exactly one contact has the email address.", False),
        BranchDefinition(NOT_FOUND, "No active contact has the email address.", True),
        BranchDefinition(AMBIGUOUS, "Several contacts have the email address; the output lists them so the application can choose.", True),
        BranchDefinition(PROVIDER_REJECTED, "Xero conclusively rejected the lookup, such as a revoked connection or a missing accounting.contacts.read scope.", True),
        BranchDefinition(DAILY_LIMIT_REACHED, "Xero's daily API limit for the organisation is exhausted; the Receipt carries retryAfterSeconds until it resets.", True),
        BranchDefinition(INVALID_RESPONSE, "Xero returned an invalid or oversized contact list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_INVOICE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_INVOICE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The invoice was read.", False),
        BranchDefinition(NOT_FOUND, "The invoice does not exist in the organisation.", True),
        BranchDefinition(PROVIDER_REJECTED, "Xero conclusively rejected the read, such as a revoked connection or a missing accounting.invoices scope.", True),
        BranchDefinition(DAILY_LIMIT_REACHED, "Xero's daily API limit for the organisation is exhausted; the Receipt carries retryAfterSeconds until it resets.", True),
        BranchDefinition(INVALID_RESPONSE, "Xero returned an invalid or oversized invoice.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_INVOICES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_INVOICES_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of matching invoices was read; it can be empty.", False),
        BranchDefinition(PROVIDER_REJECTED, "Xero conclusively rejected the read, such as a revoked connection, a missing accounting.invoices scope, or a query over Xero's volume threshold.", True),
        BranchDefinition(DAILY_LIMIT_REACHED, "Xero's daily API limit for the organisation is exhausted; the Receipt carries retryAfterSeconds until it resets.", True),
        BranchDefinition(INVALID_RESPONSE, "Xero returned an invalid or oversized page of invoices.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_INVOICE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_INVOICE_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Xero created the invoice, or returned the invoice this Step already created, with its number and totals.", False),
        BranchDefinition(PROVIDER_REJECTED, "Xero conclusively rejected the invoice, such as an unknown contact or account code, a locked period, a plan's invoice limit, or a missing scope; nothing was created.", True),
        BranchDefinition(DAILY_LIMIT_REACHED, "Xero's daily API limit for the organisation is exhausted before the request ran, so nothing was created; the Receipt carries retryAfterSeconds.", True),
        BranchDefinition(UNCERTAIN, "Xero may have created the invoice but answered with an internal error, which it caches under the key, or with an unusable invoice; read it back by reference before creating again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=240)),
    ),
)

RECORD_PAYMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, RECORD_PAYMENT_OPERATION_ID),
    (
        BranchDefinition(RECORDED, "Xero recorded the payment, or returned the payment this Step already recorded, with the invoice's remaining amount due.", False),
        BranchDefinition(PROVIDER_REJECTED, "Xero conclusively rejected the payment, such as an amount above the amount due, an invoice that is not approved, an account that cannot take payments, or a locked period; nothing was recorded.", True),
        BranchDefinition(DAILY_LIMIT_REACHED, "Xero's daily API limit for the organisation is exhausted before the request ran, so nothing was recorded; the Receipt carries retryAfterSeconds.", True),
        BranchDefinition(UNCERTAIN, "Xero may have recorded the payment but answered with an internal error, which it caches under the key, or with an unusable payment; read the invoice back before paying again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=240)),
    ),
)
