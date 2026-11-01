# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Close lead operations."""

from collections.abc import Callable, Mapping
from typing import TypeVar

import dex
from dex_connectors.sdk import QueryResult, QueryStep, QueryStepConfig, StepAnnotations

from .connection import Connection
from .models import GetLeadInput, Lead, LeadPage, ListLeadsInput

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_list_leads_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListLeadsInput],
    listed: dex.StepTarget[QueryResult[LeadPage]],
    provider_rejected: dex.StepTarget[QueryResult[LeadPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[LeadPage]] | None = None,
    defect: dex.StepTarget[QueryResult[LeadPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[LeadPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListLeadsInput, LeadPage]:
    """Create the typed lead page Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.list_leads(),
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


def new_get_lead_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetLeadInput],
    found: dex.StepTarget[QueryResult[Lead]],
    not_found: dex.StepTarget[QueryResult[Lead]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Lead]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Lead]] | None = None,
    defect: dex.StepTarget[QueryResult[Lead]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Lead]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetLeadInput, Lead]:
    """Create the typed lead lookup Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.get_lead(),
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
        raise ValueError("Close Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Close Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Close Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
