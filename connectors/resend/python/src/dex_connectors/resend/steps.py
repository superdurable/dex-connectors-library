# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Resend email operations."""

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
from .models import Email, GetEmailInput, SendEmailInput, SentEmail

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_send_email_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SendEmailInput],
    sent: dex.StepTarget[MutationResult[SentEmail]],
    provider_rejected: dex.StepTarget[MutationResult[SentEmail]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[SentEmail]] | None = None,
    defect: dex.StepTarget[MutationResult[SentEmail]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SentEmail]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SendEmailInput, SentEmail]:
    """Create the typed send Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.send_email(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("sent", sent),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_get_email_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetEmailInput],
    found: dex.StepTarget[QueryResult[Email]],
    not_found: dex.StepTarget[QueryResult[Email]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Email]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Email]] | None = None,
    defect: dex.StepTarget[QueryResult[Email]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Email]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetEmailInput, Email]:
    """Create the typed lookup Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.get_email(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("found", found),
                ("notFound", not_found),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def _require_connection(connection: Connection, connection_name: str) -> None:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Resend Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Resend Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Resend Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
