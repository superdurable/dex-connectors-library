# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListStoresInput requests all stores authorized to the Lingxing account.
@dataclass(frozen=True, slots=True)
class ListStoresInput:
    pass


setattr(ListStoresInput, '__module__', 'dex_connectors.lingxing.models')


# Store contains selected Amazon store metadata without seller account identifiers.
@dataclass(frozen=True, slots=True)
class Store:
    # StoreID is Lingxing's stable seller ID.
    store_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'storeID'})
    # Name is the configured store display name.
    name: str = ''
    # Region is the Amazon region abbreviation.
    region: str = ''
    # Country is the Amazon marketplace country.
    country: str = ''
    # Status is Lingxing's synchronization state: 0 stopped, 1 normal, 2 authorization error, or 3 payment suspension.
    status: int = 0


setattr(Store, '__module__', 'dex_connectors.lingxing.models')


# StoreList contains up to 200 authorized stores; Truncated reports additional omitted stores.
@dataclass(frozen=True, slots=True)
class StoreList:
    # Stores contains at most 200 stores.
    stores: tuple[Store, ...] = ()
    # Truncated is true when the provider returned more than 200 stores.
    truncated: bool = False

    def __post_init__(self) -> None:
        object.__setattr__(self, 'stores', _replace_nil_slices(self.stores, 1))


StoreList.__annotations__['stores'] = tuple[Store, ...] | None
StoreList.__dataclass_fields__['stores'].type = tuple[Store, ...] | None
setattr(StoreList, '__module__', 'dex_connectors.lingxing.models')
