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
        BranchDefinition(UNCERTAIN, "Stripe replayed a cached server error for this idempotency key, so no resend can confirm the Checkout Session; every other unknown outcome is resent under the same key.", True),
        BranchDefinition(INVALID_RESPONSE, "Stripe accepted the request but returned an invalid or oversized Checkout Session, which a resend under the same key would only replay.", True),
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
        BranchDefinition(UNCERTAIN, "Stripe replayed a cached server error for this idempotency key, so no resend can confirm the Checkout Session; every other unknown outcome is resent under the same key.", True),
        BranchDefinition(INVALID_RESPONSE, "Stripe accepted the request but returned an invalid or oversized Checkout Session, which a resend under the same key would only replay.", True),
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
        BranchDefinition(UNCERTAIN, "Stripe replayed a cached server error for this idempotency key and bounded read-back found no refund with this call ID; every other unknown outcome is resent under the same key.", True),
        BranchDefinition(INVALID_RESPONSE, "Stripe accepted the refund request but returned an invalid or oversized refund, which a resend under the same key would only replay.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
