# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Webflow request inputs and provider JSON outputs."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class ListCollectionItemsInput:
    """Collection ID, an optional limit of at most 100, and an optional offset."""

    collection_id: str = field(default="", metadata={"dex_json_name": "collectionId"})
    limit: int = field(default=0, metadata={"dex_json_omit_default": True})
    offset: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class ListCollectionItemsOutput:
    """The provider's collection item page JSON, kept untouched."""

    data: Any = None


@dataclass(frozen=True, slots=True)
class GetCollectionItemInput:
    """Collection and item IDs, each 24 hexadecimal characters."""

    collection_id: str = field(default="", metadata={"dex_json_name": "collectionId"})
    item_id: str = field(default="", metadata={"dex_json_name": "itemId"})


@dataclass(frozen=True, slots=True)
class GetCollectionItemOutput:
    """The provider's collection item JSON, kept untouched."""

    data: Any = None
