# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListObjectMetadataInput, ListObjectMetadataOutput, ObjectMetadata
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_OBJECT_METADATA_DEFINITION, PROVIDER_REJECTED

def list_object_metadata_listed(value: ListObjectMetadataOutput, failure: Failure | None = None) -> Case[ListObjectMetadataOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_object_metadata_listed, '__module__', "dex_connectors.fxiaoke.fxiaokemock")

def list_object_metadata_provider_rejected(value: ListObjectMetadataOutput, failure: Failure | None = None) -> Case[ListObjectMetadataOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_object_metadata_provider_rejected, '__module__', "dex_connectors.fxiaoke.fxiaokemock")

def list_object_metadata_invalid_response(value: ListObjectMetadataOutput, failure: Failure | None = None) -> Case[ListObjectMetadataOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_object_metadata_invalid_response, '__module__', "dex_connectors.fxiaoke.fxiaokemock")

def list_object_metadata_defect(value: ListObjectMetadataOutput, failure: Failure | None = None) -> Case[ListObjectMetadataOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_object_metadata_defect, '__module__', "dex_connectors.fxiaoke.fxiaokemock")

def list_object_metadata_retry(failure: Failure, after_seconds: float = 0) -> Case[ListObjectMetadataOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_object_metadata_retry, '__module__', "dex_connectors.fxiaoke.fxiaokemock")

def list_object_metadata_account_object() -> Case[ListObjectMetadataOutput]:
    return Case.branch_result(LISTED, ListObjectMetadataOutput(objects=(ObjectMetadata(api_name='AccountObj', display_name='客户', active=True),)))

setattr(list_object_metadata_account_object, '__module__', "dex_connectors.fxiaoke.fxiaokemock")


class Mock:

    def __init__(self, connection_name: str = "fxiaoke") -> None:
        self.list_object_metadata_script: Query[ListObjectMetadataInput, ListObjectMetadataOutput] = Query(LIST_OBJECT_METADATA_DEFINITION)
        self.list_object_metadata_script.default(list_object_metadata_account_object())
        self._connection = new_connection_with_operations(
            list_object_metadata=self.list_object_metadata_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.fxiaoke.fxiaokemock")

__all__ = [
    "Mock",
    "list_object_metadata_listed",
    "list_object_metadata_provider_rejected",
    "list_object_metadata_invalid_response",
    "list_object_metadata_defect",
    "list_object_metadata_retry",
    "list_object_metadata_account_object",
]
