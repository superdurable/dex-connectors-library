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


# ListFolderFilesInput selects one drive folder and an optional page cursor.
@dataclass(frozen=True, slots=True)
class ListFolderFilesInput:
    # DriveID is the WPS drive identifier visible in its drive API or URL.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId'})
    # ParentID is the WPS folder identifier; 0 selects the drive root.
    parent_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentId'})
    # PageToken is an opaque cursor from a prior response; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken'})


setattr(ListFolderFilesInput, '__module__', 'dex_connectors.kingsoftoffice.models')


# FileMetadata holds only file identifiers and names, without content, people, or sharing links.
@dataclass(frozen=True, slots=True)
class FileMetadata:
    # ID is the WPS file identifier.
    id: str = ''
    # Name is the file or folder name.
    name: str = ''
    # Type is the WPS file type, such as file or folder.
    type: str = ''
    # SizeBytes is the provider's file size in bytes.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})


setattr(FileMetadata, '__module__', 'dex_connectors.kingsoftoffice.models')


# ListFolderFilesOutput is one bounded page of file metadata.
@dataclass(frozen=True, slots=True)
class ListFolderFilesOutput:
    # Items contains at most the configured page size.
    items: tuple[FileMetadata, ...] = ()
    # NextPageToken is empty when no next page exists.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


ListFolderFilesOutput.__annotations__['items'] = tuple[FileMetadata, ...] | None
ListFolderFilesOutput.__dataclass_fields__['items'].type = tuple[FileMetadata, ...] | None
setattr(ListFolderFilesOutput, '__module__', 'dex_connectors.kingsoftoffice.models')
