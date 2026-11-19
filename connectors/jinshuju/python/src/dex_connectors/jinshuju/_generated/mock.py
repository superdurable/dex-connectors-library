# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FormMetadata, ListFormsPageInput, ListFormsPageOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_FORMS_PAGE_DEFINITION, PROVIDER_REJECTED

def list_forms_page_listed(value: ListFormsPageOutput, failure: Failure | None = None) -> Case[ListFormsPageOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_forms_page_listed, '__module__', "dex_connectors.jinshuju.jinshujumock")

def list_forms_page_provider_rejected(value: ListFormsPageOutput, failure: Failure | None = None) -> Case[ListFormsPageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_forms_page_provider_rejected, '__module__', "dex_connectors.jinshuju.jinshujumock")

def list_forms_page_invalid_response(value: ListFormsPageOutput, failure: Failure | None = None) -> Case[ListFormsPageOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_forms_page_invalid_response, '__module__', "dex_connectors.jinshuju.jinshujumock")

def list_forms_page_defect(value: ListFormsPageOutput, failure: Failure | None = None) -> Case[ListFormsPageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_forms_page_defect, '__module__', "dex_connectors.jinshuju.jinshujumock")

def list_forms_page_retry(failure: Failure, after_seconds: float = 0) -> Case[ListFormsPageOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_forms_page_retry, '__module__', "dex_connectors.jinshuju.jinshujumock")

def list_forms_page_one_form() -> Case[ListFormsPageOutput]:
    return Case.branch_result(LISTED, ListFormsPageOutput(total=1, count=1, forms=(FormMetadata(name='需求调研表', token='a1B2C3'),), next=''))

setattr(list_forms_page_one_form, '__module__', "dex_connectors.jinshuju.jinshujumock")


class Mock:

    def __init__(self, connection_name: str = "jinshuju") -> None:
        self.list_forms_page_script: Query[ListFormsPageInput, ListFormsPageOutput] = Query(LIST_FORMS_PAGE_DEFINITION)
        self.list_forms_page_script.default(list_forms_page_one_form())
        self._connection = new_connection_with_operations(
            list_forms_page=self.list_forms_page_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.jinshuju.jinshujumock")

__all__ = [
    "Mock",
    "list_forms_page_listed",
    "list_forms_page_provider_rejected",
    "list_forms_page_invalid_response",
    "list_forms_page_defect",
    "list_forms_page_retry",
    "list_forms_page_one_form",
]
