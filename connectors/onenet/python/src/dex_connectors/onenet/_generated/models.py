# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# QueryDeviceDetailInput selects one device by its name in the configured product.
@dataclass(frozen=True, slots=True)
class QueryDeviceDetailInput:
    # DeviceName is the device name shown in OneNET Product Development > Device Management.
    device_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'deviceName'})


setattr(QueryDeviceDetailInput, '__module__', 'dex_connectors.onenet.models')


# DeviceDetail contains nonsecret OneNET device metadata.
@dataclass(frozen=True, slots=True)
class DeviceDetail:
    # ProductID identifies the configured product.
    product_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'productId'})
    # DeviceName identifies the device within that product.
    device_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'deviceName'})
    # Status is 0 offline, 1 online, or 2 not activated.
    status: int = 0
    # Description is the optional device description.
    description: str = ''
    # CreateTime is the provider's device creation timestamp.
    create_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'createTime'})
    # LastOnlineTime is the provider's last online timestamp.
    last_online_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastOnlineTime'})


setattr(DeviceDetail, '__module__', 'dex_connectors.onenet.models')
