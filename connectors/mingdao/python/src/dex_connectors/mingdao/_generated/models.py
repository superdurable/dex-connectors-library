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


# ListWorksheetsInput has no provider-specific query parameters.
@dataclass(frozen=True, slots=True)
class ListWorksheetsInput:
    pass


setattr(ListWorksheetsInput, '__module__', 'dex_connectors.mingdao.models')


# Worksheet identifies one application worksheet without exposing records.
@dataclass(frozen=True, slots=True)
class Worksheet:
    # ID is the stable worksheet ID.
    id: str = ''
    # Name is the worksheet display name.
    name: str = ''


setattr(Worksheet, '__module__', 'dex_connectors.mingdao.models')


# ListWorksheetsOutput contains the application's worksheet metadata.
@dataclass(frozen=True, slots=True)
class ListWorksheetsOutput:
    # Worksheets are the worksheets visible to the application's authorization key.
    worksheets: tuple[Worksheet, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'worksheets', _replace_nil_slices(self.worksheets, 1))


ListWorksheetsOutput.__annotations__['worksheets'] = tuple[Worksheet, ...] | None
ListWorksheetsOutput.__dataclass_fields__['worksheets'].type = tuple[Worksheet, ...] | None
setattr(ListWorksheetsOutput, '__module__', 'dex_connectors.mingdao.models')
