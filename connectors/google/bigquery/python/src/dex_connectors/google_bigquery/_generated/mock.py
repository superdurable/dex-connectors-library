# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Dataset, DatasetsPage, ListDatasetsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DATASETS_DEFINITION, PROVIDER_REJECTED

def list_datasets_listed(value: DatasetsPage, failure: Failure | None = None) -> Case[DatasetsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_datasets_listed, '__module__', "dex_connectors.google_bigquery.bigquerymock")

def list_datasets_provider_rejected(value: DatasetsPage, failure: Failure | None = None) -> Case[DatasetsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_datasets_provider_rejected, '__module__', "dex_connectors.google_bigquery.bigquerymock")

def list_datasets_invalid_response(value: DatasetsPage, failure: Failure | None = None) -> Case[DatasetsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_datasets_invalid_response, '__module__', "dex_connectors.google_bigquery.bigquerymock")

def list_datasets_defect(value: DatasetsPage, failure: Failure | None = None) -> Case[DatasetsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_datasets_defect, '__module__', "dex_connectors.google_bigquery.bigquerymock")

def list_datasets_retry(failure: Failure, after_seconds: float = 0) -> Case[DatasetsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_datasets_retry, '__module__', "dex_connectors.google_bigquery.bigquerymock")

def list_datasets_listed_default() -> Case[DatasetsPage]:
    return Case.branch_result(LISTED, DatasetsPage(datasets=(Dataset(id='sample_dataset', project_id='sample-project', friendly_name='Sample Dataset'),), next_page_token=''))

setattr(list_datasets_listed_default, '__module__', "dex_connectors.google_bigquery.bigquerymock")


class Mock:

    def __init__(self, connection_name: str = "google-bigquery") -> None:
        self.list_datasets_script: Query[ListDatasetsInput, DatasetsPage] = Query(LIST_DATASETS_DEFINITION)
        self.list_datasets_script.default(list_datasets_listed_default())
        self._connection = new_connection_with_operations(
            list_datasets=self.list_datasets_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_bigquery.bigquerymock")

__all__ = [
    "Mock",
    "list_datasets_listed",
    "list_datasets_provider_rejected",
    "list_datasets_invalid_response",
    "list_datasets_defect",
    "list_datasets_retry",
    "list_datasets_listed_default",
]
