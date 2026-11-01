# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Brevo contact operations."""

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
from .models import Contact, ContactWriteResult, CreateOrUpdateContactInput, GetContactInput

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_get_contact_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetContactInput],
    found: dex.StepTarget[QueryResult[Contact]],
    not_found: dex.StepTarget[QueryResult[Contact]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Contact]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Contact]] | None = None,
    defect: dex.StepTarget[QueryResult[Contact]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Contact]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetContactInput, Contact]:
    """Create the typed contact Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.get_contact(),
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


def new_create_or_update_contact_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateOrUpdateContactInput],
    saved: dex.StepTarget[MutationResult[ContactWriteResult]],
    provider_rejected: dex.StepTarget[MutationResult[ContactWriteResult]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[ContactWriteResult]] | None = None,
    defect: dex.StepTarget[MutationResult[ContactWriteResult]] | None = None,
    result_attribute: dex.Attribute[MutationResult[ContactWriteResult]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateOrUpdateContactInput, ContactWriteResult]:
    """Create the typed contact upsert Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.create_or_update_contact(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("saved", saved),
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
        raise ValueError("Brevo Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Brevo Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Brevo Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
