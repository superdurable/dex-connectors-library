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
from ..models import ContactPage, CreateInvoiceInput, CreateInvoiceOutput, FindContactByEmailInput, FindContactByEmailOutput, GetInvoiceInput, Invoice, InvoicePage, ListContactsInput, ListInvoicesInput, RecordPaymentInput, RecordPaymentOutput

StepInput = TypeVar("StepInput")

def new_list_contacts_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListContactsInput],
    listed: dex.StepTarget[QueryResult[ContactPage]],
    provider_rejected: dex.StepTarget[QueryResult[ContactPage]] | None = None,
    daily_limit_reached: dex.StepTarget[QueryResult[ContactPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ContactPage]] | None = None,
    defect: dex.StepTarget[QueryResult[ContactPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ContactPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListContactsInput, ContactPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Xero Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Xero Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Xero Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if daily_limit_reached is not None:
        branches["dailyLimitReached"] = daily_limit_reached
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_contacts"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_contacts_step, '__module__', 'dex_connectors.xero.steps')

def new_find_contact_by_email_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], FindContactByEmailInput],
    found: dex.StepTarget[QueryResult[FindContactByEmailOutput]],
    not_found: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    ambiguous: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    daily_limit_reached: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[FindContactByEmailOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, FindContactByEmailInput, FindContactByEmailOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Xero Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Xero Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Xero Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if ambiguous is not None:
        branches["ambiguous"] = ambiguous
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if daily_limit_reached is not None:
        branches["dailyLimitReached"] = daily_limit_reached
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "find_contact_by_email"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_find_contact_by_email_step, '__module__', 'dex_connectors.xero.steps')

def new_get_invoice_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetInvoiceInput],
    found: dex.StepTarget[QueryResult[Invoice]],
    not_found: dex.StepTarget[QueryResult[Invoice]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Invoice]] | None = None,
    daily_limit_reached: dex.StepTarget[QueryResult[Invoice]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Invoice]] | None = None,
    defect: dex.StepTarget[QueryResult[Invoice]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Invoice]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetInvoiceInput, Invoice]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Xero Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Xero Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Xero Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if daily_limit_reached is not None:
        branches["dailyLimitReached"] = daily_limit_reached
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "get_invoice"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_invoice_step, '__module__', 'dex_connectors.xero.steps')

def new_list_invoices_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListInvoicesInput],
    listed: dex.StepTarget[QueryResult[InvoicePage]],
    provider_rejected: dex.StepTarget[QueryResult[InvoicePage]] | None = None,
    daily_limit_reached: dex.StepTarget[QueryResult[InvoicePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[InvoicePage]] | None = None,
    defect: dex.StepTarget[QueryResult[InvoicePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[InvoicePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListInvoicesInput, InvoicePage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Xero Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Xero Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Xero Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if daily_limit_reached is not None:
        branches["dailyLimitReached"] = daily_limit_reached
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_invoices"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_invoices_step, '__module__', 'dex_connectors.xero.steps')

def new_create_invoice_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateInvoiceInput],
    created: dex.StepTarget[MutationResult[CreateInvoiceOutput]],
    provider_rejected: dex.StepTarget[MutationResult[CreateInvoiceOutput]] | None = None,
    daily_limit_reached: dex.StepTarget[MutationResult[CreateInvoiceOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreateInvoiceOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateInvoiceOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateInvoiceOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateInvoiceInput, CreateInvoiceOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Xero Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Xero Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Xero Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if daily_limit_reached is not None:
        branches["dailyLimitReached"] = daily_limit_reached
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_invoice"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_invoice_step, '__module__', 'dex_connectors.xero.steps')

def new_record_payment_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], RecordPaymentInput],
    recorded: dex.StepTarget[MutationResult[RecordPaymentOutput]],
    provider_rejected: dex.StepTarget[MutationResult[RecordPaymentOutput]] | None = None,
    daily_limit_reached: dex.StepTarget[MutationResult[RecordPaymentOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[RecordPaymentOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[RecordPaymentOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[RecordPaymentOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, RecordPaymentInput, RecordPaymentOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Xero Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Xero Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Xero Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "recorded": recorded,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if daily_limit_reached is not None:
        branches["dailyLimitReached"] = daily_limit_reached
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "record_payment"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_record_payment_step, '__module__', 'dex_connectors.xero.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
