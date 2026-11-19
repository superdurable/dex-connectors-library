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


# ListIndustriesInput needs no operation-specific values.
@dataclass(frozen=True, slots=True)
class ListIndustriesInput:
    pass


setattr(ListIndustriesInput, '__module__', 'dex_connectors.shixiseng.models')


# ListIndustriesOutput contains only industry names, without applicant data.
@dataclass(frozen=True, slots=True)
class ListIndustriesOutput:
    # Industries are Shixiseng industry filter labels.
    industries: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'industries', _replace_nil_slices(self.industries, 1))


ListIndustriesOutput.__annotations__['industries'] = tuple[str, ...] | None
ListIndustriesOutput.__dataclass_fields__['industries'].type = tuple[str, ...] | None
setattr(ListIndustriesOutput, '__module__', 'dex_connectors.shixiseng.models')
