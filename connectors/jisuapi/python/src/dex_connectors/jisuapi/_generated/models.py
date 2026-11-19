# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListProvincesInput needs no selection because the provider returns the province catalog.
@dataclass(frozen=True, slots=True)
class ListProvincesInput:
    pass


setattr(ListProvincesInput, '__module__', 'dex_connectors.jisuapi.models')


# Province identifies one administrative province without additional provider data.
@dataclass(frozen=True, slots=True)
class Province:
    # ID is the provider's province identifier.
    id: str = ''
    # Name is the province display name.
    name: str = ''


setattr(Province, '__module__', 'dex_connectors.jisuapi.models')


# ListProvincesOutput contains only province IDs and names.
@dataclass(frozen=True, slots=True)
class ListProvincesOutput:
    # Provinces are the provinces in the provider response.
    provinces: tuple[Province, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'provinces', _replace_nil_slices(self.provinces, 1))


ListProvincesOutput.__annotations__['provinces'] = tuple[Province, ...] | None
ListProvincesOutput.__dataclass_fields__['provinces'].type = tuple[Province, ...] | None
setattr(ListProvincesOutput, '__module__', 'dex_connectors.jisuapi.models')
