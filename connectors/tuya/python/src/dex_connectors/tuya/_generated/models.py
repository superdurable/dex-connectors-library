# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetDeviceDetailsInput identifies one device linked to the project.
@dataclass(frozen=True, slots=True)
class GetDeviceDetailsInput:
    # DeviceID is the non-secret device identifier from the Tuya project Devices tab.
    device_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'deviceId'})


setattr(GetDeviceDetailsInput, '__module__', 'dex_connectors.tuya.models')


# GetDeviceDetailsOutput contains only safe metadata, excluding local_key and status values.
@dataclass(frozen=True, slots=True)
class GetDeviceDetailsOutput:
    # DeviceID is the provider's stable device identifier.
    device_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'deviceId'})
    # Name is the device's display name.
    name: str = ''
    # Online reports current connectivity.
    online: bool = False
    # Category is the provider product-category code.
    category: str = ''
    # ProductID is the provider product identifier.
    product_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'productId'})
    # CreatedAtUnix is the first-pair timestamp in Unix seconds.
    created_at_unix: int = _dataclass_field(default=0, metadata={'dex_json_name': 'createdAtUnix'})
    # UpdatedAtUnix is the last-update timestamp in Unix seconds.
    updated_at_unix: int = _dataclass_field(default=0, metadata={'dex_json_name': 'updatedAtUnix'})


setattr(GetDeviceDetailsOutput, '__module__', 'dex_connectors.tuya.models')
