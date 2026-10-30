# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Stripe scenarios for deterministic connector tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import (
    CheckoutSession,
    CreateACHCheckoutSessionInput,
    CreateCardCheckoutSessionInput,
    CreateRefundInput,
    GetCheckoutSessionInput,
    GetPaymentIntentInput,
    PaymentIntent,
    Refund,
)
from ..operations import (
    CREATE_ACH_CHECKOUT_SESSION_DEFINITION,
    CREATE_CARD_CHECKOUT_SESSION_DEFINITION,
    CREATE_REFUND_DEFINITION,
    GET_CHECKOUT_SESSION_DEFINITION,
    GET_PAYMENT_INTENT_DEFINITION,
)


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "stripe", operation, message)


def create_card_checkout_session_case(name: str) -> Case[CheckoutSession]:
    """Return a named createCardCheckoutSession scenario from connector.yaml."""
    operation = "createCardCheckoutSession"
    if name == "createdCardSession":
        return Case.branch_result(
            "created",
            CheckoutSession(
                "cs_test_card123",
                "https://checkout.stripe.com/c/pay/cs_test_card123",
                "registration-card-42",
                "unpaid",
                "open",
                currency="usd",
                amount_total=7500,
                metadata={"registration_id": "registration-card-42"},
            ),
        )
    if name == "invalidCardRequest":
        return Case.branch_result(
            "providerRejected",
            CheckoutSession(),
            _failure(operation, FailureKind.PROVIDER_REJECTION, "Stripe rejected the request"),
        )
    if name == "unconfirmedCardSession":
        return Case.uncertain(
            CheckoutSession(),
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                "Stripe Checkout Session outcome cannot be confirmed after a server error",
            ),
        )
    if name == "malformedCardSession":
        return Case.branch_result(
            "invalidResponse",
            CheckoutSession(),
            _failure(
                operation,
                FailureKind.PROTOCOL,
                "Stripe response is invalid: Stripe returned an invalid Checkout Session",
            ),
        )
    if name == "invalidCardCurrency":
        return Case.branch_result(
            "defect",
            CheckoutSession(),
            _failure(operation, FailureKind.VALIDATION, "currency must be a lowercase three-letter code"),
        )
    raise ValueError("unknown createCardCheckoutSession mock scenario")


def create_ach_checkout_session_case(name: str) -> Case[CheckoutSession]:
    """Return a named createACHCheckoutSession scenario from connector.yaml."""
    operation = "createACHCheckoutSession"
    if name == "createdSession":
        return Case.branch_result(
            "created",
            CheckoutSession(
                "cs_test_a1b2c3",
                "https://checkout.stripe.com/c/pay/cs_test_a1b2c3",
                "registration-42",
                "unpaid",
                "open",
                currency="usd",
                amount_total=7500,
                metadata={"registration_id": "registration-42"},
            ),
        )
    if name == "invalidRequest":
        return Case.branch_result(
            "providerRejected",
            CheckoutSession(),
            _failure(operation, FailureKind.PROVIDER_REJECTION, "Stripe rejected the request"),
        )
    if name == "unconfirmedSession":
        return Case.uncertain(
            CheckoutSession(),
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                "Stripe Checkout Session outcome cannot be confirmed after a server error",
            ),
        )
    if name == "malformedSession":
        return Case.branch_result(
            "invalidResponse",
            CheckoutSession(),
            _failure(
                operation,
                FailureKind.PROTOCOL,
                "Stripe response is invalid: Stripe returned an invalid Checkout Session",
            ),
        )
    if name == "invalidCurrency":
        return Case.branch_result(
            "defect",
            CheckoutSession(),
            _failure(operation, FailureKind.VALIDATION, "currency must be a lowercase three-letter code"),
        )
    raise ValueError("unknown createACHCheckoutSession mock scenario")


def get_checkout_session_case(name: str) -> Case[CheckoutSession]:
    """Return a named getCheckoutSession scenario from connector.yaml."""
    operation = "getCheckoutSession"
    if name == "paidSession":
        return Case.branch_result(
            "found",
            CheckoutSession(
                "cs_test_a1b2c3",
                payment_status="paid",
                status="complete",
                payment_intent_id="pi_3Pabc123",
                currency="usd",
                amount_total=7500,
            ),
        )
    if name == "missingSession":
        return Case.branch_result(
            "notFound",
            CheckoutSession(),
            _failure(operation, FailureKind.NOT_FOUND, "Stripe Checkout Session was not found"),
        )
    if name == "unauthorizedKey":
        return Case.branch_result(
            "providerRejected",
            CheckoutSession(),
            _failure(operation, FailureKind.AUTHENTICATION, "Stripe rejected the request"),
        )
    if name == "malformedSession":
        return Case.branch_result(
            "invalidResponse",
            CheckoutSession(),
            _failure(
                operation,
                FailureKind.PROTOCOL,
                "Stripe response is invalid: Stripe returned an invalid Checkout Session",
            ),
        )
    if name == "invalidSessionID":
        return Case.branch_result(
            "defect",
            CheckoutSession(),
            _failure(operation, FailureKind.VALIDATION, "Stripe Checkout Session ID is required"),
        )
    raise ValueError("unknown getCheckoutSession mock scenario")


def get_payment_intent_case(name: str) -> Case[PaymentIntent]:
    """Return a named getPaymentIntent scenario from connector.yaml."""
    operation = "getPaymentIntent"
    if name == "partiallyRefundedPayment":
        return Case.branch_result(
            "found",
            PaymentIntent("pi_3Pabc123", 7500, "usd", "succeeded", 2500, "person@example.com", "ch_3Pabc123"),
        )
    if name == "missingPaymentIntent":
        return Case.branch_result(
            "notFound",
            PaymentIntent(),
            _failure(
                operation, FailureKind.NOT_FOUND, "Stripe PaymentIntent was not found: resource_missing"
            ),
        )
    if name == "unauthorizedKey":
        return Case.branch_result(
            "providerRejected",
            PaymentIntent(),
            _failure(operation, FailureKind.AUTHENTICATION, "Stripe rejected the request"),
        )
    if name == "malformedPaymentIntent":
        return Case.branch_result(
            "invalidResponse",
            PaymentIntent(),
            _failure(
                operation,
                FailureKind.PROTOCOL,
                "Stripe response is invalid: Stripe returned an invalid PaymentIntent",
            ),
        )
    if name == "invalidPaymentIntentID":
        return Case.branch_result(
            "defect",
            PaymentIntent(),
            _failure(operation, FailureKind.VALIDATION, "Stripe PaymentIntent ID must be a pi_ identifier"),
        )
    raise ValueError("unknown getPaymentIntent mock scenario")


def create_refund_case(name: str) -> Case[Refund]:
    """Return a named createRefund scenario from connector.yaml."""
    operation = "createRefund"
    if name == "succeededRefund":
        return Case.branch_result("refunded", Refund("re_3Pabc123", "succeeded", 2500, "usd"))
    if name == "alreadyRefunded":
        return Case.branch_result(
            "providerRejected",
            Refund(),
            _failure(
                operation,
                FailureKind.PROVIDER_REJECTION,
                "Stripe rejected the refund: charge_already_refunded: "
                "Charge ch_3Pabc123 has already been refunded.",
            ),
        )
    if name == "unconfirmedRefund":
        return Case.uncertain(
            Refund(),
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                "Stripe refund outcome cannot be confirmed after a server error",
            ),
        )
    if name == "malformedRefund":
        return Case.branch_result(
            "invalidResponse",
            Refund(),
            _failure(
                operation,
                FailureKind.PROTOCOL,
                "Stripe response is invalid: Stripe returned an invalid refund",
            ),
        )
    if name == "invalidAmount":
        return Case.branch_result(
            "defect", Refund(), _failure(operation, FailureKind.VALIDATION, "refund amount must be positive")
        )
    raise ValueError("unknown createRefund mock scenario")


class _MockOperations:
    def __init__(self, mock: "Mock") -> None:
        self._mock = mock

    def create_card_checkout_session(self) -> Mutation[CreateCardCheckoutSessionInput, CheckoutSession]:
        return self._mock.create_card_checkout_session

    def create_ach_checkout_session(self) -> Mutation[CreateACHCheckoutSessionInput, CheckoutSession]:
        return self._mock.create_ach_checkout_session

    def get_checkout_session(self) -> Query[GetCheckoutSessionInput, CheckoutSession]:
        return self._mock.get_checkout_session

    def get_payment_intent(self) -> Query[GetPaymentIntentInput, PaymentIntent]:
        return self._mock.get_payment_intent

    def create_refund(self) -> Mutation[CreateRefundInput, Refund]:
        return self._mock.create_refund


class Mock:
    """Script all five Stripe operations, defaulting to manifest happy paths."""

    def __init__(self, connection_name: str) -> None:
        if not connection_name.strip():
            raise ValueError("Stripe mock connection name is required")
        self.connection_name = connection_name
        self.create_card_checkout_session: Mutation[CreateCardCheckoutSessionInput, CheckoutSession] = (
            Mutation(CREATE_CARD_CHECKOUT_SESSION_DEFINITION)
        )
        self.create_ach_checkout_session: Mutation[CreateACHCheckoutSessionInput, CheckoutSession] = Mutation(
            CREATE_ACH_CHECKOUT_SESSION_DEFINITION
        )
        self.get_checkout_session: Query[GetCheckoutSessionInput, CheckoutSession] = Query(
            GET_CHECKOUT_SESSION_DEFINITION
        )
        self.get_payment_intent: Query[GetPaymentIntentInput, PaymentIntent] = Query(
            GET_PAYMENT_INTENT_DEFINITION
        )
        self.create_refund: Mutation[CreateRefundInput, Refund] = Mutation(CREATE_REFUND_DEFINITION)
        self.create_card_checkout_session.default(create_card_checkout_session_case("createdCardSession"))
        self.create_ach_checkout_session.default(create_ach_checkout_session_case("createdSession"))
        self.get_checkout_session.default(get_checkout_session_case("paidSession"))
        self.get_payment_intent.default(get_payment_intent_case("partiallyRefundedPayment"))
        self.create_refund.default(create_refund_case("succeededRefund"))
        self._connection = new_connection_with_operations(
            _MockOperations(self), ConnectionRef("stripe", connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection

    def assert_no_failures(self) -> None:
        """Assert every scripted operation was answered and report writes succeeded."""
        for operation in (
            self.create_card_checkout_session,
            self.create_ach_checkout_session,
            self.get_checkout_session,
            self.get_payment_intent,
            self.create_refund,
        ):
            operation.assert_no_failures()


__all__ = [
    "Mock",
    "create_ach_checkout_session_case",
    "create_card_checkout_session_case",
    "create_refund_case",
    "get_checkout_session_case",
    "get_payment_intent_case",
]
