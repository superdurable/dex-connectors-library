# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Figma file and comment reads."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class GetFileInput:
    """Figma file key from the file URL, such as figma.com/design/<key>/..."""

    file_key: str = field(default="", metadata={"dex_json_name": "fileKey"})


@dataclass(frozen=True, slots=True)
class File:
    """File name, last-modified timestamp text, and the untouched document node tree."""

    name: str = ""
    last_modified: str = field(default="", metadata={"dex_json_name": "lastModified"})
    document: Any = None


@dataclass(frozen=True, slots=True)
class GetFileCommentsInput:
    """Figma file key whose comments are read."""

    file_key: str = field(default="", metadata={"dex_json_name": "fileKey"})


@dataclass(frozen=True, slots=True)
class FileComment:
    """One comment's ID and message text."""

    id: str = ""
    message: str = ""


@dataclass(frozen=True, slots=True)
class FileComments:
    """All comments Figma returned for the file."""

    comments: tuple[FileComment, ...] = ()
