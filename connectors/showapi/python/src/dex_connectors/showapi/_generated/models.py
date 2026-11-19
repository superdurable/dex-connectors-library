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


# LookupAreaCodesInput names the place to resolve to weather area codes.
@dataclass(frozen=True, slots=True)
class LookupAreaCodesInput:
    # Area is a nonempty Chinese place name, limited to 100 Unicode characters.
    area: str = ''


setattr(LookupAreaCodesInput, '__module__', 'dex_connectors.showapi.models')


# AreaCodeMatch is a weather-region match without customer data.
@dataclass(frozen=True, slots=True)
class AreaCodeMatch:
    # Province is the province returned by ShowAPI.
    province: str = ''
    # Area is the city or district returned by ShowAPI.
    area: str = ''
    # AreaCode is the weather region code for subsequent forecast calls.
    area_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'areaCode'})


setattr(AreaCodeMatch, '__module__', 'dex_connectors.showapi.models')


# AreaCodeMatches contains the bounded matches for one place name.
@dataclass(frozen=True, slots=True)
class AreaCodeMatches:
    # Matches contains up to 100 provider matches.
    matches: tuple[AreaCodeMatch, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'matches', _replace_nil_slices(self.matches, 1))


AreaCodeMatches.__annotations__['matches'] = tuple[AreaCodeMatch, ...] | None
AreaCodeMatches.__dataclass_fields__['matches'].type = tuple[AreaCodeMatch, ...] | None
setattr(AreaCodeMatches, '__module__', 'dex_connectors.showapi.models')
