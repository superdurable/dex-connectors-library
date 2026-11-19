# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetStoreBasicInput has no parameters; application credentials identify the authorized store.
@dataclass(frozen=True, slots=True)
class GetStoreBasicInput:
    pass


setattr(GetStoreBasicInput, '__module__', 'dex_connectors.weimob.models')


# StoreBasic contains the WOS store's public profile.
@dataclass(frozen=True, slots=True)
class StoreBasic:
    # StoreID is the Weimob BOS ID.
    store_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'storeID'})
    # Name is the store's display name.
    name: str = ''
    # Logo is the store's logo URL, when available.
    logo: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Description is the store's introduction, when available.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(StoreBasic, '__module__', 'dex_connectors.weimob.models')
