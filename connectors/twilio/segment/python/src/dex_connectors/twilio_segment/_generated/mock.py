# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSourceInput, SourceProfile
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SOURCE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_source_found(value: SourceProfile, failure: Failure | None = None) -> Case[SourceProfile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_source_found, '__module__', "dex_connectors.twilio_segment.segmentmock")

def get_source_not_found(value: SourceProfile, failure: Failure | None = None) -> Case[SourceProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_source_not_found, '__module__', "dex_connectors.twilio_segment.segmentmock")

def get_source_provider_rejected(value: SourceProfile, failure: Failure | None = None) -> Case[SourceProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_source_provider_rejected, '__module__', "dex_connectors.twilio_segment.segmentmock")

def get_source_invalid_response(value: SourceProfile, failure: Failure | None = None) -> Case[SourceProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_source_invalid_response, '__module__', "dex_connectors.twilio_segment.segmentmock")

def get_source_defect(value: SourceProfile, failure: Failure | None = None) -> Case[SourceProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_source_defect, '__module__', "dex_connectors.twilio_segment.segmentmock")

def get_source_retry(failure: Failure, after_seconds: float = 0) -> Case[SourceProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_source_retry, '__module__', "dex_connectors.twilio_segment.segmentmock")

def get_source_found_default() -> Case[SourceProfile]:
    return Case.branch_result(FOUND, SourceProfile(id='source123', slug='my-source', name='My Source'))

setattr(get_source_found_default, '__module__', "dex_connectors.twilio_segment.segmentmock")


class Mock:

    def __init__(self, connection_name: str = "segment") -> None:
        self.get_source_script: Query[GetSourceInput, SourceProfile] = Query(GET_SOURCE_DEFINITION)
        self.get_source_script.default(get_source_found_default())
        self._connection = new_connection_with_operations(
            get_source=self.get_source_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.twilio_segment.segmentmock")

__all__ = [
    "Mock",
    "get_source_found",
    "get_source_not_found",
    "get_source_provider_rejected",
    "get_source_invalid_response",
    "get_source_defect",
    "get_source_retry",
    "get_source_found_default",
]
