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


# QueryWaybillTraceInput identifies one STO waybill authorized for the subscription application.
@dataclass(frozen=True, slots=True)
class QueryWaybillTraceInput:
    # WaybillNo is the STO waybill number.
    waybill_no: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillNo'})


setattr(QueryWaybillTraceInput, '__module__', 'dex_connectors.sto.models')


# WaybillEvent is one tracking scan with personal details omitted.
@dataclass(frozen=True, slots=True)
class WaybillEvent:
    # ScanType is the provider's scan type, such as 收件 or 签收.
    scan_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'scanType'})
    # OpTime is the scan time in yyyy-MM-dd HH:mm:ss format.
    op_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'opTime'})
    # OpOrgName is the scanning branch name, when available.
    op_org_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'opOrgName', 'dex_json_omit_default': True})


setattr(WaybillEvent, '__module__', 'dex_connectors.sto.models')


# WaybillTrace contains a bounded, privacy-minimized tracking history.
@dataclass(frozen=True, slots=True)
class WaybillTrace:
    # WaybillNo is the queried waybill number.
    waybill_no: str = _dataclass_field(default='', metadata={'dex_json_name': 'waybillNo'})
    # Events follow the order returned by STO.
    events: tuple[WaybillEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


WaybillTrace.__annotations__['events'] = tuple[WaybillEvent, ...] | None
WaybillTrace.__dataclass_fields__['events'].type = tuple[WaybillEvent, ...] | None
setattr(WaybillTrace, '__module__', 'dex_connectors.sto.models')
