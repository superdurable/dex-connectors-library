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


# QueryWaybillTraceInput identifies a ZTO waybill under the application's bound electronic-waybill account.
@dataclass(frozen=True, slots=True)
class QueryWaybillTraceInput:
    # BillCode is the ZTO waybill number.
    bill_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'billCode'})


setattr(QueryWaybillTraceInput, '__module__', 'dex_connectors.zto.models')


# WaybillEvent is one tracking scan with provider descriptions and personal details omitted.
@dataclass(frozen=True, slots=True)
class WaybillEvent:
    # ScanType is the ZTO scan type, such as 收件 or 签收.
    scan_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'scanType'})
    # ScanDate is the Unix timestamp in milliseconds.
    scan_date: int = _dataclass_field(default=0, metadata={'dex_json_name': 'scanDate'})
    # ScanSiteName is the scanning site's name, when available.
    scan_site_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'scanSiteName', 'dex_json_omit_default': True})


setattr(WaybillEvent, '__module__', 'dex_connectors.zto.models')


# WaybillTrace contains a bounded, privacy-minimized tracking history.
@dataclass(frozen=True, slots=True)
class WaybillTrace:
    # BillCode is the queried waybill number.
    bill_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'billCode'})
    # Events follow the order returned by ZTO.
    events: tuple[WaybillEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


WaybillTrace.__annotations__['events'] = tuple[WaybillEvent, ...] | None
WaybillTrace.__dataclass_fields__['events'].type = tuple[WaybillEvent, ...] | None
setattr(WaybillTrace, '__module__', 'dex_connectors.zto.models')
