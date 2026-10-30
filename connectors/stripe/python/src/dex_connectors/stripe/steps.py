# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Stripe Step factories for Dex Flows."""

from collections.abc import Callable, Mapping
from typing import TypeVar

import dex
from dex_connectors.sdk import (
    MutationResult,
    MutationStep,
    MutationStepConfig,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from .connection import Connection
from .models import (
    CheckoutSession,
    CreateACHCheckoutSessionInput,
    CreateCardCheckoutSessionInput,
    CreateRefundInput,
    GetCheckoutSessionInput,
    GetPaymentIntentInput,
    PaymentIntent,
    Refund,
)

StepInput = TypeVar("StepInput")
Result = TypeVar("Result")


def new_create_card_checkout_session_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations,
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateCardCheckoutSessionInput],
    created: dex.StepTarget[MutationResult[CheckoutSession]],
    provider_rejected: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    defect: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CheckoutSession]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateCardCheckoutSessionInput, CheckoutSession]:
    """Create a card Checkout Step with the manifest's branch identities."""
    _validate_connection_name(connection, connection_name)
    branches = _provided_branches(
        {
            "created": created,
            "providerRejected": provider_rejected,
            "uncertain": uncertain,
            "invalidResponse": invalid_response,
            "defect": defect,
        }
    )
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=annotations,
            operation=connection.client.create_card_checkout_session(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_create_ach_checkout_session_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations,
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateACHCheckoutSessionInput],
    created: dex.StepTarget[MutationResult[CheckoutSession]],
    provider_rejected: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    defect: dex.StepTarget[MutationResult[CheckoutSession]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CheckoutSession]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateACHCheckoutSessionInput, CheckoutSession]:
    """Create a US bank account Checkout Step with manifest branch identities."""
    _validate_connection_name(connection, connection_name)
    branches = _provided_branches(
        {
            "created": created,
            "providerRejected": provider_rejected,
            "uncertain": uncertain,
            "invalidResponse": invalid_response,
            "defect": defect,
        }
    )
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=annotations,
            operation=connection.client.create_ach_checkout_session(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_get_checkout_session_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations,
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetCheckoutSessionInput],
    found: dex.StepTarget[QueryResult[CheckoutSession]],
    not_found: dex.StepTarget[QueryResult[CheckoutSession]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[CheckoutSession]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[CheckoutSession]] | None = None,
    defect: dex.StepTarget[QueryResult[CheckoutSession]] | None = None,
    result_attribute: dex.Attribute[QueryResult[CheckoutSession]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetCheckoutSessionInput, CheckoutSession]:
    """Create a Checkout Session Query Step with manifest branch identities."""
    _validate_connection_name(connection, connection_name)
    branches = _provided_branches(
        {
            "found": found,
            "notFound": not_found,
            "providerRejected": provider_rejected,
            "invalidResponse": invalid_response,
            "defect": defect,
        }
    )
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=annotations,
            operation=connection.client.get_checkout_session(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_get_payment_intent_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations,
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetPaymentIntentInput],
    found: dex.StepTarget[QueryResult[PaymentIntent]],
    not_found: dex.StepTarget[QueryResult[PaymentIntent]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[PaymentIntent]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[PaymentIntent]] | None = None,
    defect: dex.StepTarget[QueryResult[PaymentIntent]] | None = None,
    result_attribute: dex.Attribute[QueryResult[PaymentIntent]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetPaymentIntentInput, PaymentIntent]:
    """Create a PaymentIntent Query Step with manifest branch identities."""
    _validate_connection_name(connection, connection_name)
    branches = _provided_branches(
        {
            "found": found,
            "notFound": not_found,
            "providerRejected": provider_rejected,
            "invalidResponse": invalid_response,
            "defect": defect,
        }
    )
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=annotations,
            operation=connection.client.get_payment_intent(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_create_refund_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations,
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateRefundInput],
    refunded: dex.StepTarget[MutationResult[Refund]],
    provider_rejected: dex.StepTarget[MutationResult[Refund]] | None = None,
    uncertain: dex.StepTarget[MutationResult[Refund]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[Refund]] | None = None,
    defect: dex.StepTarget[MutationResult[Refund]] | None = None,
    result_attribute: dex.Attribute[MutationResult[Refund]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateRefundInput, Refund]:
    """Create a refund Mutation Step with the manifest's branch identities."""
    _validate_connection_name(connection, connection_name)
    branches = _provided_branches(
        {
            "refunded": refunded,
            "providerRejected": provider_rejected,
            "uncertain": uncertain,
            "invalidResponse": invalid_response,
            "defect": defect,
        }
    )
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=annotations,
            operation=connection.client.create_refund(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def _validate_connection_name(connection: Connection, connection_name: str) -> None:
    if connection is None:
        raise ValueError("Stripe connection is required")
    if connection_name != connection.reference.name:
        raise ValueError(
            f"Stripe Step connection_name {connection_name!r} must equal its connection name "
            f"{connection.reference.name!r}"
        )


def _provided_branches(
    targets: Mapping[str, dex.StepTarget[Result] | None],
) -> dict[str, dex.StepTarget[Result]]:
    return {branch_id: target for branch_id, target in targets.items() if target is not None}
