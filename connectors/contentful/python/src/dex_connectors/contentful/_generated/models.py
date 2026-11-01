# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

# GetEntryInput identifies one published entry.
@dataclass(frozen=True, slots=True)
class GetEntryInput:
    # SpaceID is the Contentful space ID.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId'})
    # EnvironmentID is the environment ID, usually master.
    environment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'environmentId'})
    # EntryID is the published entry ID.
    entry_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'entryId'})


setattr(GetEntryInput, '__module__', 'dex_connectors.contentful.models')


# GetEntryOutput preserves the entry JSON.
@dataclass(frozen=True, slots=True)
class GetEntryOutput:
    # Data is the published entry object.
    data: Any = None


setattr(GetEntryOutput, '__module__', 'dex_connectors.contentful.models')


# ListEntriesInput selects one page of published entries.
@dataclass(frozen=True, slots=True)
class ListEntriesInput:
    # SpaceID is the Contentful space ID.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId'})
    # EnvironmentID is the environment ID.
    environment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'environmentId'})
    # Limit caps entries, from 1 through 1000; zero uses the provider default.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Skip is the zero-based offset.
    skip: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListEntriesInput, '__module__', 'dex_connectors.contentful.models')


# ListEntriesOutput preserves the entry page JSON.
@dataclass(frozen=True, slots=True)
class ListEntriesOutput:
    # Data contains items, total, limit, and skip.
    data: Any = None


setattr(ListEntriesOutput, '__module__', 'dex_connectors.contentful.models')
