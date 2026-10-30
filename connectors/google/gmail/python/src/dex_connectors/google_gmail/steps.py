# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Gmail Step factories with static Dex connection identity."""

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
from .models import GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput

StepInput = TypeVar("StepInput")


def _validate_connection(connection: Connection, connection_name: str) -> None:
    if connection is None or connection_name != connection.reference.name:
        raise ValueError("Gmail Step ConnectionName must equal its connection name")


def _step_annotations(value: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(value, StepAnnotations):
        annotations = value
    else:
        if set(value) != {"groupId", "groupLabel", "explanation"} or any(
            not isinstance(field_value, str) for field_value in value.values()
        ):
            raise ValueError("Gmail Step annotations require groupId, groupLabel, and explanation")
        annotations = StepAnnotations(
            value["groupId"], value["groupLabel"], value["explanation"]
        )
    annotations.validate()
    return annotations


def new_get_message_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetMessageInput],
    read: dex.StepTarget[QueryResult[Message]],
    not_found: dex.StepTarget[QueryResult[Message]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Message]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Message]] | None = None,
    defect: dex.StepTarget[QueryResult[Message]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Message]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetMessageInput, Message]:
    """Create getMessage with one required read route and optional failure routes.

    Pass inline ``groupId``, ``groupLabel``, and ``explanation`` annotations
    for Dex source analysis, or a ``StepAnnotations`` instance at runtime.
    """
    _validate_connection(connection, connection_name)
    branches: dict[str, dex.StepTarget[QueryResult[Message]]] = {"read": read}
    for branch_id, target in (
        ("notFound", not_found),
        ("providerRejected", provider_rejected),
        ("invalidResponse", invalid_response),
        ("defect", defect),
    ):
        if target is not None:
            branches[branch_id] = target
    return QueryStep(QueryStepConfig(
        step_type=step_type,
        step_input_type=step_input_type,
        annotations=_step_annotations(annotations),
        operation=connection.get_message,
        connection=connection.reference,
        map_to_operation_input=map_to_operation_input,
        branches=branches,
        result_attribute=result_attribute,
        step_options_override=step_options_override,
    ))


def new_send_message_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SendMessageInput],
    sent: dex.StepTarget[MutationResult[SendMessageOutput]],
    provider_rejected: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SendMessageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SendMessageInput, SendMessageOutput]:
    """Create sendMessage with the manifest's bounded duplicate-risk retry policy.

    Pass inline ``groupId``, ``groupLabel``, and ``explanation`` annotations
    for Dex source analysis, or a ``StepAnnotations`` instance at runtime.
    """
    _validate_connection(connection, connection_name)
    branches: dict[str, dex.StepTarget[MutationResult[SendMessageOutput]]] = {"sent": sent}
    for branch_id, target in (("providerRejected", provider_rejected), ("defect", defect)):
        if target is not None:
            branches[branch_id] = target
    return MutationStep(MutationStepConfig(
        step_type=step_type,
        step_input_type=step_input_type,
        annotations=_step_annotations(annotations),
        operation=connection.send_message,
        connection=connection.reference,
        map_to_operation_input=map_to_operation_input,
        branches=branches,
        result_attribute=result_attribute,
        step_options_override=step_options_override,
    ))


def new_reply_to_message_step(
    *,
    step_type: str,
    step_input_type: object,
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ReplyToMessageInput],
    sent: dex.StepTarget[MutationResult[SendMessageOutput]],
    provider_rejected: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[SendMessageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SendMessageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, ReplyToMessageInput, SendMessageOutput]:
    """Create replyToMessage with the manifest's source response route.

    Pass inline ``groupId``, ``groupLabel``, and ``explanation`` annotations
    for Dex source analysis, or a ``StepAnnotations`` instance at runtime.
    """
    _validate_connection(connection, connection_name)
    branches: dict[str, dex.StepTarget[MutationResult[SendMessageOutput]]] = {"sent": sent}
    for branch_id, target in (
        ("providerRejected", provider_rejected),
        ("invalidResponse", invalid_response),
        ("defect", defect),
    ):
        if target is not None:
            branches[branch_id] = target
    return MutationStep(MutationStepConfig(
        step_type=step_type,
        step_input_type=step_input_type,
        annotations=_step_annotations(annotations),
        operation=connection.reply_to_message,
        connection=connection.reference,
        map_to_operation_input=map_to_operation_input,
        branches=branches,
        result_attribute=result_attribute,
        step_options_override=step_options_override,
    ))


__all__ = ["new_get_message_step", "new_send_message_step", "new_reply_to_message_step"]
