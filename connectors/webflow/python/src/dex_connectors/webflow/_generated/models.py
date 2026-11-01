# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

# ListCollectionItemsInput selects one staged CMS page.
@dataclass(frozen=True, slots=True)
class ListCollectionItemsInput:
    # CollectionID is the 24-character CMS collection ID.
    collection_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'collectionId'})
    # Limit caps items at 100; zero uses the provider default.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Offset is the zero-based page offset.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListCollectionItemsInput, '__module__', 'dex_connectors.webflow.models')


# ListCollectionItemsOutput preserves Webflow's item page.
@dataclass(frozen=True, slots=True)
class ListCollectionItemsOutput:
    # Data contains items and pagination.
    data: Any = None


setattr(ListCollectionItemsOutput, '__module__', 'dex_connectors.webflow.models')


# GetCollectionItemInput selects one staged CMS item.
@dataclass(frozen=True, slots=True)
class GetCollectionItemInput:
    # CollectionID is the 24-character CMS collection ID.
    collection_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'collectionId'})
    # ItemID is the 24-character CMS item ID.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})


setattr(GetCollectionItemInput, '__module__', 'dex_connectors.webflow.models')


# GetCollectionItemOutput preserves one Webflow item.
@dataclass(frozen=True, slots=True)
class GetCollectionItemOutput:
    # Data is the item's JSON object.
    data: Any = None


setattr(GetCollectionItemOutput, '__module__', 'dex_connectors.webflow.models')
