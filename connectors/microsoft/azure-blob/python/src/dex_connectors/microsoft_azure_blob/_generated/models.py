# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetBlobInput names one blob within a lowercase Azure Storage container.
@dataclass(frozen=True, slots=True)
class GetBlobInput:
    container: str = ''
    blob_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'blobName'})


setattr(GetBlobInput, '__module__', 'dex_connectors.microsoft_azure_blob.models')


# BlobDownload holds bounded blob bytes as base64 and non-secret response metadata.
@dataclass(frozen=True, slots=True)
class BlobDownload:
    data_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'dataBase64'})
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})
    etag: str = ''


setattr(BlobDownload, '__module__', 'dex_connectors.microsoft_azure_blob.models')
