# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FeatureMetadata, FeaturesPage, ListFeaturesInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_FEATURES_DEFINITION, PROVIDER_REJECTED

def list_features_listed(value: FeaturesPage, failure: Failure | None = None) -> Case[FeaturesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_features_listed, '__module__', "dex_connectors.productboard.productboardmock")

def list_features_provider_rejected(value: FeaturesPage, failure: Failure | None = None) -> Case[FeaturesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_features_provider_rejected, '__module__', "dex_connectors.productboard.productboardmock")

def list_features_invalid_response(value: FeaturesPage, failure: Failure | None = None) -> Case[FeaturesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_features_invalid_response, '__module__', "dex_connectors.productboard.productboardmock")

def list_features_defect(value: FeaturesPage, failure: Failure | None = None) -> Case[FeaturesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_features_defect, '__module__', "dex_connectors.productboard.productboardmock")

def list_features_retry(failure: Failure, after_seconds: float = 0) -> Case[FeaturesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_features_retry, '__module__', "dex_connectors.productboard.productboardmock")

def list_features_listed_default() -> Case[FeaturesPage]:
    return Case.branch_result(LISTED, FeaturesPage(features=(FeatureMetadata(id='123e4567-e89b-12d3-a456-426614174000', name='Example feature'),), next_cursor=''))

setattr(list_features_listed_default, '__module__', "dex_connectors.productboard.productboardmock")


class Mock:

    def __init__(self, connection_name: str = "productboard") -> None:
        self.list_features_script: Query[ListFeaturesInput, FeaturesPage] = Query(LIST_FEATURES_DEFINITION)
        self.list_features_script.default(list_features_listed_default())
        self._connection = new_connection_with_operations(
            list_features=self.list_features_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.productboard.productboardmock")

__all__ = [
    "Mock",
    "list_features_listed",
    "list_features_provider_rejected",
    "list_features_invalid_response",
    "list_features_defect",
    "list_features_retry",
    "list_features_listed_default",
]
