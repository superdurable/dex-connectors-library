# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetDatasetDetailsInput, GetDatasetDetailsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_DATASET_DETAILS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_dataset_details_found(value: GetDatasetDetailsOutput, failure: Failure | None = None) -> Case[GetDatasetDetailsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_dataset_details_found, '__module__', "dex_connectors.fastgpt.fastgptmock")

def get_dataset_details_provider_rejected(value: GetDatasetDetailsOutput, failure: Failure | None = None) -> Case[GetDatasetDetailsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_dataset_details_provider_rejected, '__module__', "dex_connectors.fastgpt.fastgptmock")

def get_dataset_details_invalid_response(value: GetDatasetDetailsOutput, failure: Failure | None = None) -> Case[GetDatasetDetailsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_dataset_details_invalid_response, '__module__', "dex_connectors.fastgpt.fastgptmock")

def get_dataset_details_defect(value: GetDatasetDetailsOutput, failure: Failure | None = None) -> Case[GetDatasetDetailsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_dataset_details_defect, '__module__', "dex_connectors.fastgpt.fastgptmock")

def get_dataset_details_retry(failure: Failure, after_seconds: float = 0) -> Case[GetDatasetDetailsOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_dataset_details_retry, '__module__', "dex_connectors.fastgpt.fastgptmock")

def get_dataset_details_sample_dataset() -> Case[GetDatasetDetailsOutput]:
    return Case.branch_result(FOUND, GetDatasetDetailsOutput(id='65abc9bd9d1448617cba5e6c', name='Example Knowledge Base', type='dataset', status='active'))

setattr(get_dataset_details_sample_dataset, '__module__', "dex_connectors.fastgpt.fastgptmock")


class Mock:

    def __init__(self, connection_name: str = "fastgpt") -> None:
        self.get_dataset_details_script: Query[GetDatasetDetailsInput, GetDatasetDetailsOutput] = Query(GET_DATASET_DETAILS_DEFINITION)
        self.get_dataset_details_script.default(get_dataset_details_sample_dataset())
        self._connection = new_connection_with_operations(
            get_dataset_details=self.get_dataset_details_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.fastgpt.fastgptmock")

__all__ = [
    "Mock",
    "get_dataset_details_found",
    "get_dataset_details_provider_rejected",
    "get_dataset_details_invalid_response",
    "get_dataset_details_defect",
    "get_dataset_details_retry",
    "get_dataset_details_sample_dataset",
]
