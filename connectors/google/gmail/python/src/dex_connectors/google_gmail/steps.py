# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Gmail Step factories over the async Connector SDK."""

from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Generic, TypeVar

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    MutationResult,
    MutationStep,
    MutationStepConfig,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from .connection import Connection
from .models import GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput

StepInput = TypeVar("StepInput")


@dataclass(frozen=True, slots=True)
class _GmailStepIdentity(Generic[StepInput]):
    # Stable Dex Step type, unique within its Flow.
    step_type: str
    # Concrete Dex codec type for the Step input.
    step_input_type: object
    # Group and explanation for the Step's authoring surface.
    annotations: StepAnnotations
    # Three operations bound to the same Gmail provider.
    connection: Connection
    # Static Dex Web Connections name, equal to the connection reference name.
    connection_name: str


@dataclass(frozen=True, slots=True)
class GetMessageStepConfig(_GmailStepIdentity[StepInput], Generic[StepInput]):
    """Map a typed Flow value to getMessage and route its declared outcomes."""

    # Project the current Step input into a Gmail message identifier.
    map_to_operation_input: Callable[[StepInput], GetMessageInput]
    # Required destination after a successful read.
    read: dex.StepTarget[QueryResult[Message]]
    # Optional destination when the message does not exist.
    not_found: dex.StepTarget[QueryResult[Message]] | None = None
    # Optional destination for a definite provider refusal.
    provider_rejected: dex.StepTarget[QueryResult[Message]] | None = None
    # Optional destination for malformed or oversized content.
    invalid_response: dex.StepTarget[QueryResult[Message]] | None = None
    # Optional destination for local input or connection defects.
    defect: dex.StepTarget[QueryResult[Message]] | None = None
    # Connector-owned operation configuration controls.
    configuration_ui: ConnectorConfigurationUI = field(default_factory=lambda: ConnectorConfigurationUI(()))
    # Optional durable result attribute, registered in the consuming Flow schema.
    result_attribute: dex.Attribute[QueryResult[Message]] | None = None
    # Execute-only timeout, durability, or retry overrides.
    step_options_override: dex.StepOptions | None = None


@dataclass(frozen=True, slots=True)
class SendMessageStepConfig(_GmailStepIdentity[StepInput], Generic[StepInput]):
    """Map a typed Flow value to sendMessage and route its declared outcomes."""

    # Project the current Step input into a new Gmail message.
    map_to_operation_input: Callable[[StepInput], SendMessageInput]
    # Required destination after Gmail accepts the message.
    sent: dex.StepTarget[MutationResult[SendMessageOutput]]
    # Optional destination for a definite provider refusal.
    provider_rejected: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None
    # Optional destination for local input or connection defects.
    defect: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None
    # Connector-owned operation configuration controls.
    configuration_ui: ConnectorConfigurationUI = field(default_factory=lambda: ConnectorConfigurationUI(()))
    # Optional durable result attribute, registered in the consuming Flow schema.
    result_attribute: dex.Attribute[MutationResult[SendMessageOutput]] | None = None
    # Execute-only timeout, durability, or retry overrides.
    step_options_override: dex.StepOptions | None = None


@dataclass(frozen=True, slots=True)
class ReplyToMessageStepConfig(_GmailStepIdentity[StepInput], Generic[StepInput]):
    """Map a typed Flow value to replyToMessage and route its declared outcomes."""

    # Project the current Step input into a source ID and reply body.
    map_to_operation_input: Callable[[StepInput], ReplyToMessageInput]
    # Required destination after Gmail accepts the reply.
    sent: dex.StepTarget[MutationResult[SendMessageOutput]]
    # Optional destination for a definite provider refusal.
    provider_rejected: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None
    # Optional destination for an invalid source response before the send.
    invalid_response: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None
    # Optional destination for local input or connection defects.
    defect: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None
    # Connector-owned operation configuration controls.
    configuration_ui: ConnectorConfigurationUI = field(default_factory=lambda: ConnectorConfigurationUI(()))
    # Optional durable result attribute, registered in the consuming Flow schema.
    result_attribute: dex.Attribute[MutationResult[SendMessageOutput]] | None = None
    # Execute-only timeout, durability, or retry overrides.
    step_options_override: dex.StepOptions | None = None


def _validate_connection(identity: _GmailStepIdentity[StepInput]) -> None:
    if identity.connection is None or identity.connection_name != identity.connection.reference.name:
        raise ValueError("Gmail Step ConnectionName must equal its connection name")


def _validate_send_durability(options: dex.StepOptions | None) -> None:
    if options is not None and options.execute_durability not in (
        dex.StepDurability.DEFAULT, dex.StepDurability.SYNC
    ):
        raise ValueError("Gmail sends require synchronous Execute for the single-dispatch heartbeat")


def new_get_message_step(
    config: GetMessageStepConfig[StepInput],
) -> QueryStep[StepInput, GetMessageInput, Message]:
    """Construct a typed getMessage Step; unwired optional branches fail the Flow."""
    _validate_connection(config)
    branches: dict[str, dex.StepTarget[QueryResult[Message]]] = {"read": config.read}
    for branch_id, target in (
        ("notFound", config.not_found),
        ("providerRejected", config.provider_rejected),
        ("invalidResponse", config.invalid_response),
        ("defect", config.defect),
    ):
        if target is not None:
            branches[branch_id] = target
    return QueryStep(
        QueryStepConfig(
            config.step_type,
            config.step_input_type,
            config.annotations,
            config.connection.get_message,
            config.connection.reference,
            config.map_to_operation_input,
            branches,
            configuration_ui=config.configuration_ui,
            result_attribute=config.result_attribute,
            step_options_override=config.step_options_override,
        )
    )


def new_send_message_step(
    config: SendMessageStepConfig[StepInput],
) -> MutationStep[StepInput, SendMessageInput, SendMessageOutput]:
    """Construct a typed sendMessage Step with its bounded duplicate-risk retry policy."""
    _validate_connection(config)
    _validate_send_durability(config.step_options_override)
    branches: dict[str, dex.StepTarget[MutationResult[SendMessageOutput]]] = {"sent": config.sent}
    for branch_id, target in (("providerRejected", config.provider_rejected), ("defect", config.defect)):
        if target is not None:
            branches[branch_id] = target
    return MutationStep(
        MutationStepConfig(
            config.step_type,
            config.step_input_type,
            config.annotations,
            config.connection.send_message,
            config.connection.reference,
            config.map_to_operation_input,
            branches,
            configuration_ui=config.configuration_ui,
            result_attribute=config.result_attribute,
            step_options_override=config.step_options_override,
        )
    )


def new_reply_to_message_step(
    config: ReplyToMessageStepConfig[StepInput],
) -> MutationStep[StepInput, ReplyToMessageInput, SendMessageOutput]:
    """Construct a typed replyToMessage Step with the same send read-back fence."""
    _validate_connection(config)
    _validate_send_durability(config.step_options_override)
    branches: dict[str, dex.StepTarget[MutationResult[SendMessageOutput]]] = {"sent": config.sent}
    for branch_id, target in (
        ("providerRejected", config.provider_rejected),
        ("invalidResponse", config.invalid_response),
        ("defect", config.defect),
    ):
        if target is not None:
            branches[branch_id] = target
    return MutationStep(
        MutationStepConfig(
            config.step_type,
            config.step_input_type,
            config.annotations,
            config.connection.reply_to_message,
            config.connection.reference,
            config.map_to_operation_input,
            branches,
            configuration_ui=config.configuration_ui,
            result_attribute=config.result_attribute,
            step_options_override=config.step_options_override,
        )
    )


__all__ = [
    "GetMessageStepConfig",
    "SendMessageStepConfig",
    "ReplyToMessageStepConfig",
    "new_get_message_step",
    "new_send_message_step",
    "new_reply_to_message_step",
]
