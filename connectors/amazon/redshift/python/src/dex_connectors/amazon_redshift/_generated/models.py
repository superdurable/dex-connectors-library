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


# GetStatementResultInput identifies a JSON result page by statement UUID and optional token.
@dataclass(frozen=True, slots=True)
class GetStatementResultInput:
    statement_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'statementId'})
    next_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextToken'})


setattr(GetStatementResultInput, '__module__', 'dex_connectors.amazon_redshift.models')


# StatementResultPage holds one page of Redshift Field rows and a token for the next page.
@dataclass(frozen=True, slots=True)
class StatementResultPage:
    records: tuple[Any, ...] = ()
    next_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextToken'})
    total_num_rows: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalNumRows'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


StatementResultPage.__annotations__['records'] = tuple[Any, ...] | None
StatementResultPage.__dataclass_fields__['records'].type = tuple[Any, ...] | None
setattr(StatementResultPage, '__module__', 'dex_connectors.amazon_redshift.models')
