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


# ListStockCompaniesInput selects one to one hundred A-share stock codes.
@dataclass(frozen=True, slots=True)
class ListStockCompaniesInput:
    # StockCodes are six-digit A-share identifiers to look up, without exchange prefixes.
    stock_codes: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'stockCodes'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'stock_codes', _replace_nil_slices(self.stock_codes, 1))


ListStockCompaniesInput.__annotations__['stock_codes'] = tuple[str, ...] | None
ListStockCompaniesInput.__dataclass_fields__['stock_codes'].type = tuple[str, ...] | None
setattr(ListStockCompaniesInput, '__module__', 'dex_connectors.lixinger.models')


# StockCompany contains only basic listing identifiers, without prices or financial metrics.
@dataclass(frozen=True, slots=True)
class StockCompany:
    # StockCode is the six-digit A-share code.
    stock_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'stockCode'})
    # Name is the company's exchange listing name.
    name: str = ''
    # Exchange is the provider's exchange code such as sh, sz, or bj.
    exchange: str = ''
    # ListingStatus is the provider's listing-state code, blank when omitted.
    listing_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'listingStatus'})


setattr(StockCompany, '__module__', 'dex_connectors.lixinger.models')


# ListStockCompaniesOutput contains the returned matching companies.
@dataclass(frozen=True, slots=True)
class ListStockCompaniesOutput:
    # Companies are returned in provider order.
    companies: tuple[StockCompany, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'companies', _replace_nil_slices(self.companies, 1))


ListStockCompaniesOutput.__annotations__['companies'] = tuple[StockCompany, ...] | None
ListStockCompaniesOutput.__dataclass_fields__['companies'].type = tuple[StockCompany, ...] | None
setattr(ListStockCompaniesOutput, '__module__', 'dex_connectors.lixinger.models')
