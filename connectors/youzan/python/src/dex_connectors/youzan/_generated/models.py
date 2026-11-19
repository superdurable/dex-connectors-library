# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetShopBasicInput has no parameters; the access token identifies the authorized shop.
@dataclass(frozen=True, slots=True)
class GetShopBasicInput:
    pass


setattr(GetShopBasicInput, '__module__', 'dex_connectors.youzan.models')


# ShopBasic contains the authorized shop's stable identity and basic details.
@dataclass(frozen=True, slots=True)
class ShopBasic:
    # ShopID is the Youzan kdt_id.
    shop_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'shopID'})
    # Name is the shop's display name.
    name: str = ''
    # Logo is the shop's logo URL, when available.
    logo: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is the shop's homepage URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Role identifies a single shop, head office, branch, or other organization role.
    role: int = 0
    # LineOfBusiness identifies the Youzan commerce product line.
    line_of_business: int = _dataclass_field(default=0, metadata={'dex_json_name': 'lineOfBusiness'})


setattr(ShopBasic, '__module__', 'dex_connectors.youzan.models')
