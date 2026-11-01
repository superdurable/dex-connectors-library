# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

@dataclass(frozen=True, slots=True)
class ListCollectionItemsInput:
    """Typed ListCollectionItemsInput value from the Go connector contract."""

    collection_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'collectionId'})
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListCollectionItemsInput, '__module__', 'dex_connectors.webflow.models')


@dataclass(frozen=True, slots=True)
class ListCollectionItemsOutput:
    """Typed ListCollectionItemsOutput value from the Go connector contract."""

    data: Any = None


setattr(ListCollectionItemsOutput, '__module__', 'dex_connectors.webflow.models')


@dataclass(frozen=True, slots=True)
class GetCollectionItemInput:
    """Typed GetCollectionItemInput value from the Go connector contract."""

    collection_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'collectionId'})
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})


setattr(GetCollectionItemInput, '__module__', 'dex_connectors.webflow.models')


@dataclass(frozen=True, slots=True)
class GetCollectionItemOutput:
    """Typed GetCollectionItemOutput value from the Go connector contract."""

    data: Any = None


setattr(GetCollectionItemOutput, '__module__', 'dex_connectors.webflow.models')
