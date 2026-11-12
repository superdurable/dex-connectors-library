# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListFolderInput selects one bounded page of a Dropbox folder.
@dataclass(frozen=True, slots=True)
class ListFolderInput:
    # Path is an empty string for the root or a Dropbox folder path or ID.
    path: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Limit is the approximate number of entries per page, from 1 to 2000.
    limit: int = 0


setattr(ListFolderInput, '__module__', 'dex_connectors.dropbox.models')


# EntryMetadata contains a stable Dropbox entry identity without file contents.
@dataclass(frozen=True, slots=True)
class EntryMetadata:
    # ID is the Dropbox file or folder ID.
    id: str = ''
    # Name is the entry's current file or folder name.
    name: str = ''
    # PathDisplay is the provider's human-readable path.
    path_display: str = _dataclass_field(default='', metadata={'dex_json_name': 'pathDisplay'})
    # Kind is file or folder.
    kind: str = ''


setattr(EntryMetadata, '__module__', 'dex_connectors.dropbox.models')


# FolderPage contains a bounded folder listing and its continuation cursor.
@dataclass(frozen=True, slots=True)
class FolderPage:
    # Entries contains the page's file and folder identities.
    entries: tuple[EntryMetadata, ...] = ()
    # Cursor is passed to a future continuation request when HasMore is true.
    cursor: str = ''
    # HasMore reports whether Dropbox has more entries for this cursor.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'entries', _replace_nil_slices(self.entries, 1))


FolderPage.__annotations__['entries'] = tuple[EntryMetadata, ...] | None
FolderPage.__dataclass_fields__['entries'].type = tuple[EntryMetadata, ...] | None
setattr(FolderPage, '__module__', 'dex_connectors.dropbox.models')


# GetMetadataInput selects one Dropbox file or folder.
@dataclass(frozen=True, slots=True)
class GetMetadataInput:
    # Path is an absolute Dropbox path or a stable id: or rev: identifier.
    path: str = ''


setattr(GetMetadataInput, '__module__', 'dex_connectors.dropbox.models')


# ContinueFolderInput selects the page or changes after a Dropbox cursor.
@dataclass(frozen=True, slots=True)
class ContinueFolderInput:
    # Cursor is returned by a previous folder listing or continuation.
    cursor: str = ''


setattr(ContinueFolderInput, '__module__', 'dex_connectors.dropbox.models')


# DownloadFileInput selects one Dropbox file by path, ID, or revision.
@dataclass(frozen=True, slots=True)
class DownloadFileInput:
    # Path is an absolute Dropbox path or a stable id: or rev: identifier.
    path: str = ''


setattr(DownloadFileInput, '__module__', 'dex_connectors.dropbox.models')


# DownloadedFile contains bounded Base64 content and provider file identity.
@dataclass(frozen=True, slots=True)
class DownloadedFile:
    # File is Dropbox's metadata for the downloaded file.
    file: EntryMetadata = _dataclass_field(default_factory=EntryMetadata)
    # ContentBase64 is the downloaded byte sequence encoded as Base64.
    content_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentBase64'})
    # ContentBytes is the number of decoded bytes in ContentBase64.
    content_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'contentBytes'})


setattr(DownloadedFile, '__module__', 'dex_connectors.dropbox.models')


# UploadFileInput replaces one Dropbox path with Base64-decoded content.
@dataclass(frozen=True, slots=True)
class UploadFileInput:
    # Path is the absolute destination path, including the file name.
    path: str = ''
    # ContentBase64 contains the complete replacement file content.
    content_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentBase64'})


setattr(UploadFileInput, '__module__', 'dex_connectors.dropbox.models')


# UploadedFile identifies the stored replacement without returning its content.
@dataclass(frozen=True, slots=True)
class UploadedFile:
    # File is the stored Dropbox file identity.
    file: EntryMetadata = _dataclass_field(default_factory=EntryMetadata)


setattr(UploadedFile, '__module__', 'dex_connectors.dropbox.models')
