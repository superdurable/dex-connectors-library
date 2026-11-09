# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

# GetItemInput identifies a table row by all of its S, N, or B key attributes.
@dataclass(frozen=True, slots=True)
class GetItemInput:
    table_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableName'})
    key: dict[str, Any] | None = None
    consistent_read: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'consistentRead'})


setattr(GetItemInput, '__module__', 'dex_connectors.amazon_dynamodb.models')


# ItemResult returns the item's AttributeValue map when it exists.
@dataclass(frozen=True, slots=True)
class ItemResult:
    exists: bool = False
    item: dict[str, Any] | None = None


setattr(ItemResult, '__module__', 'dex_connectors.amazon_dynamodb.models')
