# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Contentful request inputs and provider JSON outputs."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class GetEntryInput:
    """Space, environment, and entry IDs."""

    space_id: str = field(default="", metadata={"dex_json_name": "spaceId"})
    environment_id: str = field(default="", metadata={"dex_json_name": "environmentId"})
    entry_id: str = field(default="", metadata={"dex_json_name": "entryId"})


@dataclass(frozen=True, slots=True)
class GetEntryOutput:
    """The provider's entry JSON, kept untouched."""

    data: Any = None


@dataclass(frozen=True, slots=True)
class ListEntriesInput:
    """Space and environment IDs, an optional limit of at most 1000, and an optional skip."""

    space_id: str = field(default="", metadata={"dex_json_name": "spaceId"})
    environment_id: str = field(default="", metadata={"dex_json_name": "environmentId"})
    limit: int = field(default=0, metadata={"dex_json_omit_default": True})
    skip: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class ListEntriesOutput:
    """The provider's entry collection JSON, kept untouched."""

    data: Any = None
