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

CONNECTOR_ID = "quickbooks"
PROVIDER = "quickbooks"
FIND_CUSTOMER_OPERATION_ID = "findCustomer"
CREATE_CUSTOMER_OPERATION_ID = "createCustomer"
CREATE_INVOICE_OPERATION_ID = "createInvoice"
GET_INVOICE_OPERATION_ID = "getInvoice"
LIST_INVOICES_OPERATION_ID = "listInvoices"
SEND_INVOICE_OPERATION_ID = "sendInvoice"
RECORD_PAYMENT_OPERATION_ID = "recordPayment"
AMBIGUOUS = "ambiguous"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NAME_CONFLICT = "nameConflict"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
RECORDED = "recorded"
SENT = "sent"
UNCERTAIN = "uncertain"

FIND_CUSTOMER_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, FIND_CUSTOMER_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Exactly one customer matches.", False),
        BranchDefinition(NOT_FOUND, "No customer matches; inactive customers count only when the input includes them.", True),
        BranchDefinition(AMBIGUOUS, "Several customers share the email address; the output lists them so the application can choose.", True),
        BranchDefinition(PROVIDER_REJECTED, "QuickBooks conclusively rejected the query, such as a revoked connection or a company the app can no longer reach.", True),
        BranchDefinition(INVALID_RESPONSE, "QuickBooks returned an invalid or oversized customer list.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_CUSTOMER_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_CUSTOMER_OPERATION_ID),
    (
        BranchDefinition(CREATED, "QuickBooks created the customer, or returned the customer this Step already created.", False),
        BranchDefinition(NAME_CONFLICT, "A customer, vendor, or employee already uses the display name (QuickBooks error 6240); resolve it with findCustomer by display name, including a possible earlier attempt.", True),
        BranchDefinition(PROVIDER_REJECTED, "QuickBooks conclusively rejected the customer, such as an invalid field or a revoked connection; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "QuickBooks accepted the request but returned an unusable customer that read-back could not confirm; look it up with findCustomer before creating again.", True),
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
        BranchDefinition(CREATED, "QuickBooks created the invoice, or returned the invoice this Step already created, with its number, total, and balance.", False),
        BranchDefinition(PROVIDER_REJECTED, "QuickBooks conclusively rejected the invoice, such as an unknown or inactive customer or item, a duplicate document number, a closed period, or a revoked connection; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "QuickBooks accepted the request but returned an unusable invoice that read-back could not confirm; look it up by document number before creating again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
)

GET_INVOICE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_INVOICE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The invoice was read.", False),
        BranchDefinition(NOT_FOUND, "The invoice does not exist in the company or was deleted.", True),
        BranchDefinition(PROVIDER_REJECTED, "QuickBooks conclusively rejected the read, such as a revoked connection.", True),
        BranchDefinition(INVALID_RESPONSE, "QuickBooks returned an invalid or oversized invoice.", True),
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
        BranchDefinition(PROVIDER_REJECTED, "QuickBooks conclusively rejected the query, such as a revoked connection.", True),
        BranchDefinition(INVALID_RESPONSE, "QuickBooks returned an invalid or oversized page of invoices.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

SEND_INVOICE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_INVOICE_OPERATION_ID),
    (
        BranchDefinition(SENT, "QuickBooks emailed the invoice, or returned the delivery this Step already made, with EmailStatus EmailSent.", False),
        BranchDefinition(PROVIDER_REJECTED, "QuickBooks conclusively rejected the delivery, such as an invoice without an address, a deleted invoice, or a revoked connection; nothing was sent.", True),
        BranchDefinition(UNCERTAIN, "QuickBooks accepted the request but returned an unusable invoice that read-back could not confirm; check its EmailStatus with getInvoice before sending again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
)

RECORD_PAYMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, RECORD_PAYMENT_OPERATION_ID),
    (
        BranchDefinition(RECORDED, "QuickBooks recorded the payment, or returned the payment this Step already recorded, with the invoices it applies to.", False),
        BranchDefinition(PROVIDER_REJECTED, "QuickBooks conclusively rejected the payment, such as an invoice of another customer, an amount above the balance, an invalid deposit account, a closed period, or a revoked connection; nothing was recorded.", True),
        BranchDefinition(UNCERTAIN, "QuickBooks accepted the request but returned an unusable payment that read-back could not confirm; inspect the invoice before paying again.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=300)),
    ),
)
