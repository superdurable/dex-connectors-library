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


# ListDevicesInput selects one bounded page from the authorized account's devices.
@dataclass(frozen=True, slots=True)
class ListDevicesInput:
    # PageNum is a one-based page number; zero selects page one.
    page_num: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNum'})
    # PageSize is a count from 1 to 100; zero selects Aqara's documented default of 50.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListDevicesInput, '__module__', 'dex_connectors.aqara.models')


# Device contains non-secret device identity and online state.
@dataclass(frozen=True, slots=True)
class Device:
    # DeviceID is Aqara's stable device ID.
    device_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'deviceId'})
    # DeviceName is the user-assigned display name.
    device_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'deviceName'})
    # Model is Aqara's device model identifier.
    model: str = ''
    # ModelType is 1 or 2 for a gateway and 3 for a child device.
    model_type: int = _dataclass_field(default=0, metadata={'dex_json_name': 'modelType'})
    # Online reports the documented state value 1.
    online: bool = False
    # FirmwareVersion is the installed firmware version if reported.
    firmware_version: str = _dataclass_field(default='', metadata={'dex_json_name': 'firmwareVersion'})
    # PositionID is the provider location ID if reported.
    position_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'positionId'})
    # ParentDeviceID is the gateway device ID for a child device.
    parent_device_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentDeviceId'})
    # TimeZone is the device's reported timezone if available.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})


setattr(Device, '__module__', 'dex_connectors.aqara.models')


# DevicePage contains one page and the provider's total matching count.
@dataclass(frozen=True, slots=True)
class DevicePage:
    # TotalCount is the provider's total device count.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # PageNum is the one-based page number requested.
    page_num: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNum'})
    # PageSize is the page size requested.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # Devices contains no more than PageSize entries.
    devices: tuple[Device, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'devices', _replace_nil_slices(self.devices, 1))


DevicePage.__annotations__['devices'] = tuple[Device, ...] | None
DevicePage.__dataclass_fields__['devices'].type = tuple[Device, ...] | None
setattr(DevicePage, '__module__', 'dex_connectors.aqara.models')
