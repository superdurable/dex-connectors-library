# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetObjectTextInput, HeadObjectInput, ListObjectsInput, ListObjectsOutput, ObjectMetadata, ObjectSummary, ObjectText, PutObjectInput, StoredObject
from .definitions import CONNECTOR_ID, ALREADY_EXISTS, DEFECT, FOUND, GET_OBJECT_TEXT_DEFINITION, HEAD_OBJECT_DEFINITION, INVALID_RESPONSE, LIST_OBJECTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, PUT_OBJECT_DEFINITION, READ, STORED, TOO_LARGE, UNSUPPORTED_CONTENT

def list_objects_found(value: ListObjectsOutput, failure: Failure | None = None) -> Case[ListObjectsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_objects_found, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_not_found(value: ListObjectsOutput, failure: Failure | None = None) -> Case[ListObjectsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_objects_not_found, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_provider_rejected(value: ListObjectsOutput, failure: Failure | None = None) -> Case[ListObjectsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_objects_provider_rejected, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_invalid_response(value: ListObjectsOutput, failure: Failure | None = None) -> Case[ListObjectsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_objects_invalid_response, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_defect(value: ListObjectsOutput, failure: Failure | None = None) -> Case[ListObjectsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_objects_defect, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_retry(failure: Failure, after_seconds: float = 0) -> Case[ListObjectsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_objects_retry, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_archived_reports() -> Case[ListObjectsOutput]:
    return Case.paged(FOUND, (ListObjectsOutput(bucket='acme-reports', prefix='reports/', objects=(ObjectSummary(key='reports/ops-weekly.md', size_bytes=72, e_tag='"eccaf41434e0ed82b7154a7fd586cc58"', last_modified=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00'))),), is_truncated=True, next_continuation_token='page-2'), ListObjectsOutput(bucket='acme-reports', prefix='reports/', objects=(ObjectSummary(key='reports/q1.md', size_bytes=31, last_modified=datetime.fromisoformat("2026-09-01T09:00:00Z".replace('Z', '+00:00'))),), is_truncated=True, next_continuation_token='page-3'), ListObjectsOutput(bucket='acme-reports', prefix='reports/', objects=(ObjectSummary(key='reports/team.md', size_bytes=48, last_modified=datetime.fromisoformat("2026-08-01T09:00:00Z".replace('Z', '+00:00'))),), is_truncated=False)))

setattr(list_objects_archived_reports, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_no_reports() -> Case[ListObjectsOutput]:
    return Case.branch_result(NOT_FOUND, ListObjectsOutput(bucket='acme-reports', prefix='reports/missing/', is_truncated=False), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listObjects", "No objects matched the prefix."))

setattr(list_objects_no_reports, '__module__', "dex_connectors.amazon_s3.s3mock")

def list_objects_list_denied() -> Case[ListObjectsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListObjectsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listObjects", "S3 denied the bucket listing."))

setattr(list_objects_list_denied, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_found(value: ObjectMetadata, failure: Failure | None = None) -> Case[ObjectMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(head_object_found, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_not_found(value: ObjectMetadata, failure: Failure | None = None) -> Case[ObjectMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(head_object_not_found, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_provider_rejected(value: ObjectMetadata, failure: Failure | None = None) -> Case[ObjectMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(head_object_provider_rejected, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_invalid_response(value: ObjectMetadata, failure: Failure | None = None) -> Case[ObjectMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(head_object_invalid_response, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_defect(value: ObjectMetadata, failure: Failure | None = None) -> Case[ObjectMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(head_object_defect, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_retry(failure: Failure, after_seconds: float = 0) -> Case[ObjectMetadata]:
    return Case.retry(failure, after_seconds)

setattr(head_object_retry, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_archived_report_metadata() -> Case[ObjectMetadata]:
    return Case.branch_result(FOUND, ObjectMetadata(bucket='acme-reports', key='reports/ops-weekly.md', size_bytes=72, e_tag='"eccaf41434e0ed82b7154a7fd586cc58"', content_type='text/markdown; charset=utf-8', last_modified=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00')), metadata={'report-id': 'ops-weekly', 'generator': 'dex-s3-report-archive', 'dex-idempotency-key': '8a62f575-7f36-5370-af29-4aadf17e894c'}))

setattr(head_object_archived_report_metadata, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_missing_report_metadata() -> Case[ObjectMetadata]:
    return Case.branch_result(NOT_FOUND, ObjectMetadata(bucket='acme-reports', key='reports/missing.md'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "headObject", "The object was not found."))

setattr(head_object_missing_report_metadata, '__module__', "dex_connectors.amazon_s3.s3mock")

def head_object_metadata_denied() -> Case[ObjectMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, ObjectMetadata(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "headObject", "S3 denied the metadata read."))

setattr(head_object_metadata_denied, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_read(value: ObjectText, failure: Failure | None = None) -> Case[ObjectText]:
    return Case.branch_result(READ, value, failure)

setattr(get_object_text_read, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_not_found(value: ObjectText, failure: Failure | None = None) -> Case[ObjectText]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_object_text_not_found, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_unsupported_content(value: ObjectText, failure: Failure | None = None) -> Case[ObjectText]:
    return Case.branch_result(UNSUPPORTED_CONTENT, value, failure)

setattr(get_object_text_unsupported_content, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_too_large(value: ObjectText, failure: Failure | None = None) -> Case[ObjectText]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(get_object_text_too_large, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_provider_rejected(value: ObjectText, failure: Failure | None = None) -> Case[ObjectText]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_object_text_provider_rejected, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_invalid_response(value: ObjectText, failure: Failure | None = None) -> Case[ObjectText]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_object_text_invalid_response, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_defect(value: ObjectText, failure: Failure | None = None) -> Case[ObjectText]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_object_text_defect, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_retry(failure: Failure, after_seconds: float = 0) -> Case[ObjectText]:
    return Case.retry(failure, after_seconds)

setattr(get_object_text_retry, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_archived_report_text() -> Case[ObjectText]:
    return Case.branch_result(READ, ObjectText(bucket='acme-reports', key='reports/ops-weekly.md', content_type='text/markdown; charset=utf-8', e_tag='"eccaf41434e0ed82b7154a7fd586cc58"', last_modified=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00')), size_bytes=72, text='# Ops weekly\n\nReport ID: ops-weekly\n\n- Refunds above 500 need approval.\n'))

setattr(get_object_text_archived_report_text, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_missing_report_text() -> Case[ObjectText]:
    return Case.branch_result(NOT_FOUND, ObjectText(bucket='acme-reports', key='reports/missing.md'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getObjectText", "The object was not found."))

setattr(get_object_text_missing_report_text, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_binary_report() -> Case[ObjectText]:
    return Case.branch_result(UNSUPPORTED_CONTENT, ObjectText(bucket='acme-reports', key='reports/diagram.pdf', content_type='application/pdf', size_bytes=2048), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "getObjectText", "The object is not UTF-8 text."))

setattr(get_object_text_binary_report, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_oversized_report() -> Case[ObjectText]:
    return Case.branch_result(TOO_LARGE, ObjectText(bucket='acme-reports', key='reports/archive-large.md', content_type='text/markdown', size_bytes=5242880), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "getObjectText", "The object exceeds maxTextBytes."))

setattr(get_object_text_oversized_report, '__module__', "dex_connectors.amazon_s3.s3mock")

def get_object_text_text_read_denied() -> Case[ObjectText]:
    return Case.branch_result(PROVIDER_REJECTED, ObjectText(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getObjectText", "S3 denied the object read."))

setattr(get_object_text_text_read_denied, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_stored(value: StoredObject, failure: Failure | None = None) -> Case[StoredObject]:
    return Case.branch_result(STORED, value, failure)

setattr(put_object_stored, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_already_exists(value: StoredObject, failure: Failure | None = None) -> Case[StoredObject]:
    return Case.branch_result(ALREADY_EXISTS, value, failure)

setattr(put_object_already_exists, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_provider_rejected(value: StoredObject, failure: Failure | None = None) -> Case[StoredObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(put_object_provider_rejected, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_defect(value: StoredObject, failure: Failure | None = None) -> Case[StoredObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(put_object_defect, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_retry(failure: Failure, after_seconds: float = 0) -> Case[StoredObject]:
    return Case.retry(failure, after_seconds)

setattr(put_object_retry, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_stored_ops_report() -> Case[StoredObject]:
    return Case.branch_result(STORED, StoredObject(bucket='acme-reports', key='reports/ops-weekly.md', e_tag='"eccaf41434e0ed82b7154a7fd586cc58"', size_bytes=72, content_type='text/markdown; charset=utf-8'))

setattr(put_object_stored_ops_report, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_report_already_exists() -> Case[StoredObject]:
    return Case.branch_result(ALREADY_EXISTS, StoredObject(bucket='acme-reports', key='reports/ops-weekly.md', e_tag='"existing-report"', size_bytes=43, content_type='text/markdown; charset=utf-8', last_modified=datetime.fromisoformat("2026-10-24T09:00:00Z".replace('Z', '+00:00'))), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "putObject", "Another object already occupies the key."))

setattr(put_object_report_already_exists, '__module__', "dex_connectors.amazon_s3.s3mock")

def put_object_store_denied() -> Case[StoredObject]:
    return Case.branch_result(PROVIDER_REJECTED, StoredObject(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "putObject", "S3 denied the object write."))

setattr(put_object_store_denied, '__module__', "dex_connectors.amazon_s3.s3mock")


class Mock:

    def __init__(self, connection_name: str = "amazon-s3") -> None:
        self.list_objects_script: Query[ListObjectsInput, ListObjectsOutput] = Query(LIST_OBJECTS_DEFINITION)
        self.list_objects_script.default(list_objects_archived_reports())
        self.head_object_script: Query[HeadObjectInput, ObjectMetadata] = Query(HEAD_OBJECT_DEFINITION)
        self.head_object_script.default(head_object_archived_report_metadata())
        self.get_object_text_script: Query[GetObjectTextInput, ObjectText] = Query(GET_OBJECT_TEXT_DEFINITION)
        self.get_object_text_script.default(get_object_text_archived_report_text())
        self.put_object_script: Mutation[PutObjectInput, StoredObject] = Mutation(PUT_OBJECT_DEFINITION)
        self.put_object_script.default(put_object_stored_ops_report())
        self._connection = new_connection_with_operations(
            list_objects=self.list_objects_script,
            head_object=self.head_object_script,
            get_object_text=self.get_object_text_script,
            put_object=self.put_object_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amazon_s3.s3mock")

__all__ = [
    "Mock",
    "list_objects_found",
    "list_objects_not_found",
    "list_objects_provider_rejected",
    "list_objects_invalid_response",
    "list_objects_defect",
    "list_objects_retry",
    "list_objects_archived_reports",
    "list_objects_no_reports",
    "list_objects_list_denied",
    "head_object_found",
    "head_object_not_found",
    "head_object_provider_rejected",
    "head_object_invalid_response",
    "head_object_defect",
    "head_object_retry",
    "head_object_archived_report_metadata",
    "head_object_missing_report_metadata",
    "head_object_metadata_denied",
    "get_object_text_read",
    "get_object_text_not_found",
    "get_object_text_unsupported_content",
    "get_object_text_too_large",
    "get_object_text_provider_rejected",
    "get_object_text_invalid_response",
    "get_object_text_defect",
    "get_object_text_retry",
    "get_object_text_archived_report_text",
    "get_object_text_missing_report_text",
    "get_object_text_binary_report",
    "get_object_text_oversized_report",
    "get_object_text_text_read_denied",
    "put_object_stored",
    "put_object_already_exists",
    "put_object_provider_rejected",
    "put_object_defect",
    "put_object_retry",
    "put_object_stored_ops_report",
    "put_object_report_already_exists",
    "put_object_store_denied",
]
