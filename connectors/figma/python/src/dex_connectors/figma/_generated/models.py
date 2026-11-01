# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetFileInput identifies a Figma file or branch by key.
@dataclass(frozen=True, slots=True)
class GetFileInput:
    # FileKey is the file or branch key from a Figma URL.
    file_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileKey'})


setattr(GetFileInput, '__module__', 'dex_connectors.figma.models')


# File contains Figma file metadata and its bounded document tree.
@dataclass(frozen=True, slots=True)
class File:
    # Name is the resource display name.
    name: str = ''
    # LastModified is the file's provider-reported timestamp.
    last_modified: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastModified'})
    # Document is the bounded Figma document JSON tree.
    document: Any = None


setattr(File, '__module__', 'dex_connectors.figma.models')


# GetFileCommentsInput identifies a Figma file or branch by key.
@dataclass(frozen=True, slots=True)
class GetFileCommentsInput:
    # FileKey is the file or branch key from a Figma URL.
    file_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileKey'})


setattr(GetFileCommentsInput, '__module__', 'dex_connectors.figma.models')


# FileComment is a comment identity and text.
@dataclass(frozen=True, slots=True)
class FileComment:
    # ID is the provider-assigned resource identity.
    id: str = ''
    # Message is the comment text.
    message: str = ''


setattr(FileComment, '__module__', 'dex_connectors.figma.models')


# FileComments contains comments on one file.
@dataclass(frozen=True, slots=True)
class FileComments:
    # Comments contains comments returned for the file.
    comments: tuple[FileComment, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'comments', _replace_nil_slices(self.comments, 1))


FileComments.__annotations__['comments'] = tuple[FileComment, ...] | None
setattr(FileComments, '__module__', 'dex_connectors.figma.models')
