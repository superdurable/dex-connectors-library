# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListBucketsInput, ListBucketsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_BUCKETS_DEFINITION, PROVIDER_REJECTED

def list_buckets_listed(value: ListBucketsOutput, failure: Failure | None = None) -> Case[ListBucketsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_buckets_listed, '__module__', "dex_connectors.qiniu.qiniumock")

def list_buckets_provider_rejected(value: ListBucketsOutput, failure: Failure | None = None) -> Case[ListBucketsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_buckets_provider_rejected, '__module__', "dex_connectors.qiniu.qiniumock")

def list_buckets_invalid_response(value: ListBucketsOutput, failure: Failure | None = None) -> Case[ListBucketsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_buckets_invalid_response, '__module__', "dex_connectors.qiniu.qiniumock")

def list_buckets_defect(value: ListBucketsOutput, failure: Failure | None = None) -> Case[ListBucketsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_buckets_defect, '__module__', "dex_connectors.qiniu.qiniumock")

def list_buckets_retry(failure: Failure, after_seconds: float = 0) -> Case[ListBucketsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_buckets_retry, '__module__', "dex_connectors.qiniu.qiniumock")

def list_buckets_two_buckets() -> Case[ListBucketsOutput]:
    return Case.branch_result(LISTED, ListBucketsOutput(buckets=('demo-images', 'archive-data')))

setattr(list_buckets_two_buckets, '__module__', "dex_connectors.qiniu.qiniumock")


class Mock:

    def __init__(self, connection_name: str = "qiniu") -> None:
        self.list_buckets_script: Query[ListBucketsInput, ListBucketsOutput] = Query(LIST_BUCKETS_DEFINITION)
        self.list_buckets_script.default(list_buckets_two_buckets())
        self._connection = new_connection_with_operations(
            list_buckets=self.list_buckets_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.qiniu.qiniumock")

__all__ = [
    "Mock",
    "list_buckets_listed",
    "list_buckets_provider_rejected",
    "list_buckets_invalid_response",
    "list_buckets_defect",
    "list_buckets_retry",
    "list_buckets_two_buckets",
]
