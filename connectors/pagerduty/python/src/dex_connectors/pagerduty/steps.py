# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for PagerDuty incident operations."""

from collections.abc import Callable, Mapping
from typing import TypeVar

import dex
from dex_connectors.sdk import QueryResult, QueryStep, QueryStepConfig, StepAnnotations

from .connection import Connection
from .models import GetIncidentInput, Incident, IncidentPage, ListIncidentsInput

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_list_incidents_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListIncidentsInput],
    listed: dex.StepTarget[QueryResult[IncidentPage]],
    not_listed: dex.StepTarget[QueryResult[IncidentPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[IncidentPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[IncidentPage]] | None = None,
    defect: dex.StepTarget[QueryResult[IncidentPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[IncidentPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListIncidentsInput, IncidentPage]:
    """Create the typed incident list Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.list_incidents(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("listed", listed),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_get_incident_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetIncidentInput],
    found: dex.StepTarget[QueryResult[Incident]],
    not_found: dex.StepTarget[QueryResult[Incident]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Incident]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Incident]] | None = None,
    defect: dex.StepTarget[QueryResult[Incident]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Incident]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetIncidentInput, Incident]:
    """Create the typed incident lookup Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.get_incident(),
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
        raise ValueError("PagerDuty Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("PagerDuty Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("PagerDuty Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
