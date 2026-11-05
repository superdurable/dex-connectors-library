# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factory for Gemini generateContent."""

from collections.abc import Callable, Mapping
from typing import TypeVar

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from .connection import Connection
from .models import GenerateContentRequest, GenerateContentResponse

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_generate_content_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GenerateContentRequest],
    generated: dex.StepTarget[QueryResult[GenerateContentResponse]],
    truncated: dex.StepTarget[QueryResult[GenerateContentResponse]] | None = None,
    blocked: dex.StepTarget[QueryResult[GenerateContentResponse]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[GenerateContentResponse]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[GenerateContentResponse]] | None = None,
    defect: dex.StepTarget[QueryResult[GenerateContentResponse]] | None = None,
    configuration_ui: ConnectorConfigurationUI | None = None,
    result_attribute: dex.Attribute[QueryResult[GenerateContentResponse]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GenerateContentRequest, GenerateContentResponse]:
    """Create the typed generation Step; unwired optional outcomes fail the Flow.

    configuration_ui may bind the modelPicker unit's model port to the operation input's /model.
    """
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.generate_content(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("generated", generated),
                ("truncated", truncated),
                ("blocked", blocked),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            configuration_ui=configuration_ui or ConnectorConfigurationUI(()),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def _require_connection(connection: Connection, connection_name: str) -> None:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Gemini Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Gemini Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Gemini Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
