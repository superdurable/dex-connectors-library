# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import SegmentTextInput, SegmentTextOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, SEGMENTED, SEGMENT_TEXT_DEFINITION

def segment_text_segmented(value: SegmentTextOutput, failure: Failure | None = None) -> Case[SegmentTextOutput]:
    return Case.branch_result(SEGMENTED, value, failure)

setattr(segment_text_segmented, '__module__', "dex_connectors.gugudata.gugudatamock")

def segment_text_provider_rejected(value: SegmentTextOutput, failure: Failure | None = None) -> Case[SegmentTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(segment_text_provider_rejected, '__module__', "dex_connectors.gugudata.gugudatamock")

def segment_text_invalid_response(value: SegmentTextOutput, failure: Failure | None = None) -> Case[SegmentTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(segment_text_invalid_response, '__module__', "dex_connectors.gugudata.gugudatamock")

def segment_text_defect(value: SegmentTextOutput, failure: Failure | None = None) -> Case[SegmentTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(segment_text_defect, '__module__', "dex_connectors.gugudata.gugudatamock")

def segment_text_retry(failure: Failure, after_seconds: float = 0) -> Case[SegmentTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(segment_text_retry, '__module__', "dex_connectors.gugudata.gugudatamock")

def segment_text_chinese_text() -> Case[SegmentTextOutput]:
    return Case.branch_result(SEGMENTED, SegmentTextOutput(segmented_text='计算机,科学,研究,人工智能'))

setattr(segment_text_chinese_text, '__module__', "dex_connectors.gugudata.gugudatamock")


class Mock:

    def __init__(self, connection_name: str = "gugudata") -> None:
        self.segment_text_script: Query[SegmentTextInput, SegmentTextOutput] = Query(SEGMENT_TEXT_DEFINITION)
        self.segment_text_script.default(segment_text_chinese_text())
        self._connection = new_connection_with_operations(
            segment_text=self.segment_text_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.gugudata.gugudatamock")

__all__ = [
    "Mock",
    "segment_text_segmented",
    "segment_text_provider_rejected",
    "segment_text_invalid_response",
    "segment_text_defect",
    "segment_text_retry",
    "segment_text_chinese_text",
]
