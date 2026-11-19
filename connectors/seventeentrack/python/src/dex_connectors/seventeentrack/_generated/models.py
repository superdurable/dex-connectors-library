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


# ListTrackedShipmentsInput selects a 40-record page of already registered shipments.
@dataclass(frozen=True, slots=True)
class ListTrackedShipmentsInput:
    # PageNumber is one-based; zero selects the first page.
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})


setattr(ListTrackedShipmentsInput, '__module__', 'dex_connectors.seventeentrack.models')


# TrackedShipment contains only a registered number and tracking statuses.
@dataclass(frozen=True, slots=True)
class TrackedShipment:
    # Number is the registered tracking number.
    number: str = ''
    # Carrier is the 17TRACK carrier code.
    carrier: int = 0
    # PackageStatus is the latest package status.
    package_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'packageStatus'})
    # TrackingStatus says whether tracking is active or stopped.
    tracking_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'trackingStatus'})


setattr(TrackedShipment, '__module__', 'dex_connectors.seventeentrack.models')


# TrackedShipmentPage contains a bounded tracking-list page and pagination metadata.
@dataclass(frozen=True, slots=True)
class TrackedShipmentPage:
    # PageNumber is the provider's one-based page number.
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})
    # Total is the provider's count of matching shipments.
    total: int = 0
    # Shipments contains at most 40 registered shipments.
    shipments: tuple[TrackedShipment, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'shipments', _replace_nil_slices(self.shipments, 1))


TrackedShipmentPage.__annotations__['shipments'] = tuple[TrackedShipment, ...] | None
TrackedShipmentPage.__dataclass_fields__['shipments'].type = tuple[TrackedShipment, ...] | None
setattr(TrackedShipmentPage, '__module__', 'dex_connectors.seventeentrack.models')
