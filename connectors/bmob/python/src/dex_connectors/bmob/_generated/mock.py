# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CountClassObjectsInput, CountClassObjectsOutput
from .definitions import CONNECTOR_ID, COUNTED, COUNT_CLASS_OBJECTS_DEFINITION, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED

def count_class_objects_counted(value: CountClassObjectsOutput, failure: Failure | None = None) -> Case[CountClassObjectsOutput]:
    return Case.branch_result(COUNTED, value, failure)

setattr(count_class_objects_counted, '__module__', "dex_connectors.bmob.bmobmock")

def count_class_objects_provider_rejected(value: CountClassObjectsOutput, failure: Failure | None = None) -> Case[CountClassObjectsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(count_class_objects_provider_rejected, '__module__', "dex_connectors.bmob.bmobmock")

def count_class_objects_invalid_response(value: CountClassObjectsOutput, failure: Failure | None = None) -> Case[CountClassObjectsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(count_class_objects_invalid_response, '__module__', "dex_connectors.bmob.bmobmock")

def count_class_objects_defect(value: CountClassObjectsOutput, failure: Failure | None = None) -> Case[CountClassObjectsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(count_class_objects_defect, '__module__', "dex_connectors.bmob.bmobmock")

def count_class_objects_retry(failure: Failure, after_seconds: float = 0) -> Case[CountClassObjectsOutput]:
    return Case.retry(failure, after_seconds)

setattr(count_class_objects_retry, '__module__', "dex_connectors.bmob.bmobmock")

def count_class_objects_sample_count() -> Case[CountClassObjectsOutput]:
    return Case.branch_result(COUNTED, CountClassObjectsOutput(count=42))

setattr(count_class_objects_sample_count, '__module__', "dex_connectors.bmob.bmobmock")


class Mock:

    def __init__(self, connection_name: str = "bmob") -> None:
        self.count_class_objects_script: Query[CountClassObjectsInput, CountClassObjectsOutput] = Query(COUNT_CLASS_OBJECTS_DEFINITION)
        self.count_class_objects_script.default(count_class_objects_sample_count())
        self._connection = new_connection_with_operations(
            count_class_objects=self.count_class_objects_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.bmob.bmobmock")

__all__ = [
    "Mock",
    "count_class_objects_counted",
    "count_class_objects_provider_rejected",
    "count_class_objects_invalid_response",
    "count_class_objects_defect",
    "count_class_objects_retry",
    "count_class_objects_sample_count",
]
