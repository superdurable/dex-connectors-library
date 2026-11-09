# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# DownloadFileInput selects one Box file by its numeric ID.
@dataclass(frozen=True, slots=True)
class DownloadFileInput:
    # FileID appears in Box Web's /files/<ID> URL.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})


setattr(DownloadFileInput, '__module__', 'dex_connectors.box.models')


# DownloadedFile contains bounded Base64 content and its Box file ID.
@dataclass(frozen=True, slots=True)
class DownloadedFile:
    # FileID is the Box file ID requested by the Flow.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})
    # ContentBase64 is the downloaded byte sequence encoded as Base64.
    content_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentBase64'})
    # ContentBytes is the number of decoded bytes in ContentBase64.
    content_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'contentBytes'})


setattr(DownloadedFile, '__module__', 'dex_connectors.box.models')


# UploadFileInput replaces an existing Box file's current content.
@dataclass(frozen=True, slots=True)
class UploadFileInput:
    # FileID appears in Box Web's /files/<ID> URL.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})
    # ContentBase64 holds the complete replacement bytes.
    content_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentBase64'})


setattr(UploadFileInput, '__module__', 'dex_connectors.box.models')


# UploadedFile identifies the file with the requested current content.
@dataclass(frozen=True, slots=True)
class UploadedFile:
    # FileID is the existing Box file ID.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})
    # Name is the existing Box file name, preserved during replacement.
    name: str = ''
    # SHA1 is Box's hexadecimal digest of current content.
    sha1: str = ''


setattr(UploadedFile, '__module__', 'dex_connectors.box.models')
