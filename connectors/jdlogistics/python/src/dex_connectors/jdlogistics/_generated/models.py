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


# QueryWaybillTraceInput identifies one JD Logistics B2C merchant waybill.
@dataclass(frozen=True, slots=True)
class QueryWaybillTraceInput:
    # WaybillNo is the authorized JD Logistics waybill number.
    waybill_no: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillNo'})


setattr(QueryWaybillTraceInput, '__module__', 'dex_connectors.jdlogistics.models')


# WaybillEvent is one scan with personal details omitted.
@dataclass(frozen=True, slots=True)
class WaybillEvent:
    # State is the provider's tracking status code.
    state: str = ''
    # CategoryName is the tracking stage description.
    category_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'categoryName'})
    # OperationTime is the scan time in yyyy-MM-dd HH:mm:ss format.
    operation_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'operationTime'})


setattr(WaybillEvent, '__module__', 'dex_connectors.jdlogistics.models')


# WaybillTrace contains a bounded, privacy-minimized tracking history.
@dataclass(frozen=True, slots=True)
class WaybillTrace:
    # WaybillNo is the queried waybill number.
    waybill_no: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillNo'})
    # Events follow the order returned by JD Logistics.
    events: tuple[WaybillEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


WaybillTrace.__annotations__['events'] = tuple[WaybillEvent, ...] | None
WaybillTrace.__dataclass_fields__['events'].type = tuple[WaybillEvent, ...] | None
setattr(WaybillTrace, '__module__', 'dex_connectors.jdlogistics.models')
