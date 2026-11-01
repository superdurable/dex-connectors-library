# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class GetEntryInput:
    """Typed GetEntryInput value from the Go connector contract."""

    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId'})
    environment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'environmentId'})
    entry_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'entryId'})


setattr(GetEntryInput, '__module__', 'dex_connectors.contentful.models')


@dataclass(frozen=True, slots=True)
class GetEntryOutput:
    """Typed GetEntryOutput value from the Go connector contract."""

    data: Any = None


setattr(GetEntryOutput, '__module__', 'dex_connectors.contentful.models')


@dataclass(frozen=True, slots=True)
class ListEntriesInput:
    """Typed ListEntriesInput value from the Go connector contract."""

    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId'})
    environment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'environmentId'})
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    skip: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListEntriesInput, '__module__', 'dex_connectors.contentful.models')


@dataclass(frozen=True, slots=True)
class ListEntriesOutput:
    """Typed ListEntriesOutput value from the Go connector contract."""

    data: Any = None


setattr(ListEntriesOutput, '__module__', 'dex_connectors.contentful.models')
