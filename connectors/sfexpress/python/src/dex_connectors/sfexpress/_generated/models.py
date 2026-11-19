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


# QueryRoutesInput identifies one SF Express waybill owned by the application.
@dataclass(frozen=True, slots=True)
class QueryRoutesInput:
    # WaybillNumber is an SF Express waybill number, not a customer order number.
    waybill_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillNumber'})


setattr(QueryRoutesInput, '__module__', 'dex_connectors.sfexpress.models')


# RouteEvent contains the time and code of a delivery event.
@dataclass(frozen=True, slots=True)
class RouteEvent:
    # OccurredAt is the provider's local timestamp (YYYY-MM-DD HH:mm:ss).
    occurred_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'occurredAt'})
    # Opcode is the provider's event code.
    opcode: str = ''


setattr(RouteEvent, '__module__', 'dex_connectors.sfexpress.models')


# RouteQueryResult contains a waybill and its bounded standard route history.
@dataclass(frozen=True, slots=True)
class RouteQueryResult:
    # WaybillNumber is the SF Express waybill number.
    waybill_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillNumber'})
    # Routes contains at most 100 route events, without locations or contacts.
    routes: tuple[RouteEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'routes', _replace_nil_slices(self.routes, 1))


RouteQueryResult.__annotations__['routes'] = tuple[RouteEvent, ...] | None
RouteQueryResult.__dataclass_fields__['routes'].type = tuple[RouteEvent, ...] | None
setattr(RouteQueryResult, '__module__', 'dex_connectors.sfexpress.models')
