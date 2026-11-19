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

CONNECTOR_ID = "stripe"
PROVIDER = "stripe"
CREATE_CARD_CHECKOUT_SESSION_OPERATION_ID = "createCardCheckoutSession"
CREATE_ACH_CHECKOUT_SESSION_OPERATION_ID = "createACHCheckoutSession"
GET_CHECKOUT_SESSION_OPERATION_ID = "getCheckoutSession"
GET_PAYMENT_INTENT_OPERATION_ID = "getPaymentIntent"
CREATE_REFUND_OPERATION_ID = "createRefund"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
REFUNDED = "refunded"
UNCERTAIN = "uncertain"

CREATE_CARD_CHECKOUT_SESSION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_CARD_CHECKOUT_SESSION_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Stripe created the Checkout Session.", False),
        BranchDefinition(PROVIDER_REJECTED, "Stripe conclusively rejected the Checkout Session request.", True),
        BranchDefinition(UNCERTAIN, "Stripe replayed a cached server error for this idempotency key, or accepted the request but returned an invalid or oversized Checkout Session, so no resend can confirm the Session, or, on a later attempt that may follow a created Session, the connection credentials are unusable; every other unknown outcome, including a credential broker outage, is resent under the same key.", True),
        BranchDefinition(INVALID_RESPONSE, "Kept for compatibility and no longer selected: an invalid or oversized Checkout Session after Stripe accepted the request selects uncertain.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_ACH_CHECKOUT_SESSION_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_ACH_CHECKOUT_SESSION_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Stripe created the Checkout Session.", False),
        BranchDefinition(PROVIDER_REJECTED, "Stripe conclusively rejected the Checkout Session request.", True),
        BranchDefinition(UNCERTAIN, "Stripe replayed a cached server error for this idempotency key, or accepted the request but returned an invalid or oversized Checkout Session, so no resend can confirm the Session, or, on a later attempt that may follow a created Session, the connection credentials are unusable; every other unknown outcome, including a credential broker outage, is resent under the same key.", True),
        BranchDefinition(INVALID_RESPONSE, "Kept for compatibility and no longer selected: an invalid or oversized Checkout Session after Stripe accepted the request selects uncertain.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_CHECKOUT_SESSION_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CHECKOUT_SESSION_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The Checkout Session exists and was returned.", False),
        BranchDefinition(NOT_FOUND, "The Checkout Session does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Stripe conclusively rejected the Checkout Session query.", True),
        BranchDefinition(INVALID_RESPONSE, "Stripe returned an invalid or oversized Checkout Session response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_PAYMENT_INTENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PAYMENT_INTENT_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The PaymentIntent exists and was returned.", False),
        BranchDefinition(NOT_FOUND, "Stripe reported resource_missing for the PaymentIntent.", True),
        BranchDefinition(PROVIDER_REJECTED, "Stripe conclusively rejected the PaymentIntent query.", True),
        BranchDefinition(INVALID_RESPONSE, "Stripe returned an invalid or oversized PaymentIntent response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_REFUND_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_REFUND_OPERATION_ID),
    (
        BranchDefinition(REFUNDED, "Stripe accepted the refund; read its status to tell succeeded from pending.", False),
        BranchDefinition(PROVIDER_REJECTED, "Stripe conclusively rejected the refund with a 4xx error such as charge_already_refunded or an amount that is too large.", True),
        BranchDefinition(UNCERTAIN, "Stripe replayed a cached server error for this idempotency key and bounded read-back found no refund with this call ID, or Stripe accepted the refund but returned an invalid or oversized refund, which a resend would only replay, or, on a later attempt that may follow a created refund, the connection credentials are unusable; every other unknown outcome, including a credential broker outage, is resent under the same key.", True),
        BranchDefinition(INVALID_RESPONSE, "Kept for compatibility and no longer selected: an invalid or oversized refund after Stripe accepted the request selects uncertain.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
