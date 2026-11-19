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


# ListContractsInput selects one zero-based contract window; zero limit uses 20.
@dataclass(frozen=True, slots=True)
class ListContractsInput:
    # Offset is the zero-based first contract to request.
    offset: int = 0
    # Limit is the number of contracts requested, from 1 to 100.
    limit: int = 0


setattr(ListContractsInput, '__module__', 'dex_connectors.qiyuesuo.models')


# ContractSummary contains only a contract ID and its workflow status.
@dataclass(frozen=True, slots=True)
class ContractSummary:
    # ID is the decimal contract ID, encoded as a string to preserve all digits.
    id: str = ''
    # Status is the documented contract workflow state.
    status: str = ''


setattr(ContractSummary, '__module__', 'dex_connectors.qiyuesuo.models')


# ListContractsOutput contains one contract window, the total count, and the next offset.
@dataclass(frozen=True, slots=True)
class ListContractsOutput:
    # Contracts are the returned contract statuses in provider order.
    contracts: tuple[ContractSummary, ...] = ()
    # TotalCount is the total matching contract count.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # NextOffset is the next zero-based offset, or zero if the page is terminal.
    next_offset: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextOffset'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contracts', _replace_nil_slices(self.contracts, 1))


ListContractsOutput.__annotations__['contracts'] = tuple[ContractSummary, ...] | None
ListContractsOutput.__dataclass_fields__['contracts'].type = tuple[ContractSummary, ...] | None
setattr(ListContractsOutput, '__module__', 'dex_connectors.qiyuesuo.models')
