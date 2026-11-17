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


# QueryWaybillTraceInput identifies a Cainiao electronic waybill and carrier.
@dataclass(frozen=True, slots=True)
class QueryWaybillTraceInput:
    # CarrierCode is the carrier cp_code, such as YTO.
    carrier_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'carrierCode'})
    # WaybillCode is the Cainiao electronic waybill number.
    waybill_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillCode'})


setattr(QueryWaybillTraceInput, '__module__', 'dex_connectors.cainiao.models')


# WaybillEvent is one logistics event returned by TOP.
@dataclass(frozen=True, slots=True)
class WaybillEvent:
    # Action is the provider's event action code.
    action: str = ''
    # Description is the provider's event description.
    description: str = ''
    # Time is the provider's event timestamp in yyyy-MM-dd HH:mm:ss format.
    time: str = ''


setattr(WaybillEvent, '__module__', 'dex_connectors.cainiao.models')


# WaybillTrace contains bounded event history for one electronic waybill.
@dataclass(frozen=True, slots=True)
class WaybillTrace:
    # CarrierCode is the queried cp_code.
    carrier_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'carrierCode'})
    # WaybillCode is the queried electronic waybill number.
    waybill_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillCode'})
    # Events are logistics events in the order returned by TOP.
    events: tuple[WaybillEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


WaybillTrace.__annotations__['events'] = tuple[WaybillEvent, ...] | None
WaybillTrace.__dataclass_fields__['events'].type = tuple[WaybillEvent, ...] | None
setattr(WaybillTrace, '__module__', 'dex_connectors.cainiao.models')
