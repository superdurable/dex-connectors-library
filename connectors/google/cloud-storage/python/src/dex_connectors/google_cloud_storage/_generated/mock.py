# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DownloadObjectInput, DownloadedObject, ListObjectsInput, Object, ObjectsPage, UploadObjectInput
from .definitions import CONNECTOR_ID, DEFECT, DOWNLOADED, DOWNLOAD_OBJECT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_OBJECTS_DEFINITION, NOT_FOUND, OBJECT_EXISTS, PROVIDER_REJECTED, UPLOADED, UPLOAD_OBJECT_DEFINITION

def list_objects_listed(value: ObjectsPage, failure: Failure | None = None) -> Case[ObjectsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_objects_listed, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def list_objects_provider_rejected(value: ObjectsPage, failure: Failure | None = None) -> Case[ObjectsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_objects_provider_rejected, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def list_objects_invalid_response(value: ObjectsPage, failure: Failure | None = None) -> Case[ObjectsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_objects_invalid_response, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def list_objects_defect(value: ObjectsPage, failure: Failure | None = None) -> Case[ObjectsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_objects_defect, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def list_objects_retry(failure: Failure, after_seconds: float = 0) -> Case[ObjectsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_objects_retry, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def list_objects_listed_default() -> Case[ObjectsPage]:
    return Case.branch_result(LISTED, ObjectsPage(objects=(Object(name='example.txt', generation='123'),), next_page_token=''))

setattr(list_objects_listed_default, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def download_object_downloaded(value: DownloadedObject, failure: Failure | None = None) -> Case[DownloadedObject]:
    return Case.branch_result(DOWNLOADED, value, failure)

setattr(download_object_downloaded, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def download_object_not_found(value: DownloadedObject, failure: Failure | None = None) -> Case[DownloadedObject]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(download_object_not_found, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def download_object_provider_rejected(value: DownloadedObject, failure: Failure | None = None) -> Case[DownloadedObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(download_object_provider_rejected, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def download_object_invalid_response(value: DownloadedObject, failure: Failure | None = None) -> Case[DownloadedObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(download_object_invalid_response, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def download_object_defect(value: DownloadedObject, failure: Failure | None = None) -> Case[DownloadedObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(download_object_defect, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def download_object_retry(failure: Failure, after_seconds: float = 0) -> Case[DownloadedObject]:
    return Case.retry(failure, after_seconds)

setattr(download_object_retry, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def download_object_downloaded_default() -> Case[DownloadedObject]:
    return Case.branch_result(DOWNLOADED, DownloadedObject(content_base64='aGVsbG8=', content_type='text/plain'))

setattr(download_object_downloaded_default, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def upload_object_uploaded(value: Object, failure: Failure | None = None) -> Case[Object]:
    return Case.branch_result(UPLOADED, value, failure)

setattr(upload_object_uploaded, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def upload_object_object_exists(value: Object, failure: Failure | None = None) -> Case[Object]:
    return Case.branch_result(OBJECT_EXISTS, value, failure)

setattr(upload_object_object_exists, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def upload_object_provider_rejected(value: Object, failure: Failure | None = None) -> Case[Object]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upload_object_provider_rejected, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def upload_object_defect(value: Object, failure: Failure | None = None) -> Case[Object]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upload_object_defect, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def upload_object_retry(failure: Failure, after_seconds: float = 0) -> Case[Object]:
    return Case.retry(failure, after_seconds)

setattr(upload_object_retry, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

def upload_object_uploaded_default() -> Case[Object]:
    return Case.branch_result(UPLOADED, Object(name='example.txt', generation='123'))

setattr(upload_object_uploaded_default, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")


class Mock:

    def __init__(self, connection_name: str = "google-cloud-storage") -> None:
        self.list_objects_script: Query[ListObjectsInput, ObjectsPage] = Query(LIST_OBJECTS_DEFINITION)
        self.list_objects_script.default(list_objects_listed_default())
        self.download_object_script: Query[DownloadObjectInput, DownloadedObject] = Query(DOWNLOAD_OBJECT_DEFINITION)
        self.download_object_script.default(download_object_downloaded_default())
        self.upload_object_script: Mutation[UploadObjectInput, Object] = Mutation(UPLOAD_OBJECT_DEFINITION)
        self.upload_object_script.default(upload_object_uploaded_default())
        self._connection = new_connection_with_operations(
            list_objects=self.list_objects_script,
            download_object=self.download_object_script,
            upload_object=self.upload_object_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_cloud_storage.cloudstoragemock")

__all__ = [
    "Mock",
    "list_objects_listed",
    "list_objects_provider_rejected",
    "list_objects_invalid_response",
    "list_objects_defect",
    "list_objects_retry",
    "list_objects_listed_default",
    "download_object_downloaded",
    "download_object_not_found",
    "download_object_provider_rejected",
    "download_object_invalid_response",
    "download_object_defect",
    "download_object_retry",
    "download_object_downloaded_default",
    "upload_object_uploaded",
    "upload_object_object_exists",
    "upload_object_provider_rejected",
    "upload_object_defect",
    "upload_object_retry",
    "upload_object_uploaded_default",
]
