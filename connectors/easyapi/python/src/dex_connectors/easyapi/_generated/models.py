# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# QueryShopInput identifies one authorized invoicing company.
@dataclass(frozen=True, slots=True)
class QueryShopInput:
    # TaxNumber is the company's taxpayer identification number, 15, 17, 18, 19, or 20 characters.
    tax_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'taxNumber'})


setattr(QueryShopInput, '__module__', 'dex_connectors.easyapi.models')


# ShopResult contains only non-sensitive invoicing service configuration.
@dataclass(frozen=True, slots=True)
class ShopResult:
    # ShopID is EasyAPI's company ID.
    shop_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'shopId'})
    # Name is the company's registered name.
    name: str = ''
    # APIState is 1 normal, 0 expired, or -1 not activated.
    api_state: int = _dataclass_field(default=0, metadata={'dex_json_name': 'apiState'})
    # EndTime is the service expiry time in provider local time, or blank if absent.
    end_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'endTime'})


setattr(ShopResult, '__module__', 'dex_connectors.easyapi.models')
