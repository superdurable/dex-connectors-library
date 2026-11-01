# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class GetFileInput:
    """Typed GetFileInput value from the Go connector contract."""

    file_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileKey'})


setattr(GetFileInput, '__module__', 'dex_connectors.figma.models')


@dataclass(frozen=True, slots=True)
class File:
    """Typed File value from the Go connector contract."""

    name: str = ''
    last_modified: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastModified'})
    document: Any = None


setattr(File, '__module__', 'dex_connectors.figma.models')


@dataclass(frozen=True, slots=True)
class GetFileCommentsInput:
    """Typed GetFileCommentsInput value from the Go connector contract."""

    file_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileKey'})


setattr(GetFileCommentsInput, '__module__', 'dex_connectors.figma.models')


@dataclass(frozen=True, slots=True)
class FileComment:
    """Typed FileComment value from the Go connector contract."""

    id: str = ''
    message: str = ''


setattr(FileComment, '__module__', 'dex_connectors.figma.models')


@dataclass(frozen=True, slots=True)
class FileComments:
    """Typed FileComments value from the Go connector contract."""

    comments: tuple[FileComment, ...] | None = None


setattr(FileComments, '__module__', 'dex_connectors.figma.models')
