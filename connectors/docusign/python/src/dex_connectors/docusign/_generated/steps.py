# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

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

from ..connection import Connection
from ..models import CombinedDocument, CreateEnvelopeFromTemplateInput, CreatedEnvelope, DownloadCombinedDocumentInput, Envelope, EnvelopeRecipients, GetEnvelopeInput, ListEnvelopeRecipientsInput, VoidEnvelopeInput, VoidedEnvelope

StepInput = TypeVar("StepInput")

def new_create_envelope_from_template_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateEnvelopeFromTemplateInput],
    created: dex.StepTarget[MutationResult[CreatedEnvelope]],
    provider_rejected: dex.StepTarget[MutationResult[CreatedEnvelope]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreatedEnvelope]] | None = None,
    defect: dex.StepTarget[MutationResult[CreatedEnvelope]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreatedEnvelope]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateEnvelopeFromTemplateInput, CreatedEnvelope]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("DocuSign eSignature Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("DocuSign eSignature Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("DocuSign eSignature Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_envelope_from_template"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_envelope_from_template_step, '__module__', 'dex_connectors.docusign.steps')

def new_get_envelope_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetEnvelopeInput],
    found: dex.StepTarget[QueryResult[Envelope]],
    not_found: dex.StepTarget[QueryResult[Envelope]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Envelope]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Envelope]] | None = None,
    defect: dex.StepTarget[QueryResult[Envelope]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Envelope]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetEnvelopeInput, Envelope]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("DocuSign eSignature Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("DocuSign eSignature Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("DocuSign eSignature Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "get_envelope"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_envelope_step, '__module__', 'dex_connectors.docusign.steps')

def new_list_envelope_recipients_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListEnvelopeRecipientsInput],
    listed: dex.StepTarget[QueryResult[EnvelopeRecipients]],
    not_found: dex.StepTarget[QueryResult[EnvelopeRecipients]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[EnvelopeRecipients]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[EnvelopeRecipients]] | None = None,
    defect: dex.StepTarget[QueryResult[EnvelopeRecipients]] | None = None,
    result_attribute: dex.Attribute[QueryResult[EnvelopeRecipients]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListEnvelopeRecipientsInput, EnvelopeRecipients]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("DocuSign eSignature Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("DocuSign eSignature Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("DocuSign eSignature Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_envelope_recipients"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_envelope_recipients_step, '__module__', 'dex_connectors.docusign.steps')

def new_void_envelope_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], VoidEnvelopeInput],
    voided: dex.StepTarget[MutationResult[VoidedEnvelope]],
    not_voidable: dex.StepTarget[MutationResult[VoidedEnvelope]] | None = None,
    not_found: dex.StepTarget[MutationResult[VoidedEnvelope]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[VoidedEnvelope]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[VoidedEnvelope]] | None = None,
    defect: dex.StepTarget[MutationResult[VoidedEnvelope]] | None = None,
    result_attribute: dex.Attribute[MutationResult[VoidedEnvelope]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, VoidEnvelopeInput, VoidedEnvelope]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("DocuSign eSignature Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("DocuSign eSignature Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("DocuSign eSignature Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "voided": voided,
    }
    if not_voidable is not None:
        branches["notVoidable"] = not_voidable
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "void_envelope"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_void_envelope_step, '__module__', 'dex_connectors.docusign.steps')

def new_download_combined_document_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], DownloadCombinedDocumentInput],
    downloaded: dex.StepTarget[QueryResult[CombinedDocument]],
    not_found: dex.StepTarget[QueryResult[CombinedDocument]] | None = None,
    too_large: dex.StepTarget[QueryResult[CombinedDocument]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[CombinedDocument]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[CombinedDocument]] | None = None,
    defect: dex.StepTarget[QueryResult[CombinedDocument]] | None = None,
    result_attribute: dex.Attribute[QueryResult[CombinedDocument]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, DownloadCombinedDocumentInput, CombinedDocument]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("DocuSign eSignature Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("DocuSign eSignature Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("DocuSign eSignature Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "downloaded": downloaded,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if too_large is not None:
        branches["tooLarge"] = too_large
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "download_combined_document"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_download_combined_document_step, '__module__', 'dex_connectors.docusign.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
