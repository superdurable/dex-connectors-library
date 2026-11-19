# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetShipmentStatusCountsInput selects the account's aggregate parcel statuses.
@dataclass(frozen=True, slots=True)
class GetShipmentStatusCountsInput:
    pass


setattr(GetShipmentStatusCountsInput, '__module__', 'dex_connectors.fiftyonetracking.models')


# ShipmentStatusCounts contains aggregate registered-parcel counts without personal information.
@dataclass(frozen=True, slots=True)
class ShipmentStatusCounts:
    # Pending counts parcels awaiting the first lookup.
    pending: int = 0
    # NotFound counts parcels without a carrier result.
    not_found: int = _dataclass_field(default=0, metadata={'dex_json_name': 'notFound'})
    # InTransit counts parcels in transit.
    in_transit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'inTransit'})
    # Pickup counts parcels ready for pickup.
    pickup: int = 0
    # Delivered counts delivered parcels.
    delivered: int = 0
    # Expired counts parcels that exceeded the tracking window.
    expired: int = 0
    # Undelivered counts failed delivery attempts.
    undelivered: int = 0
    # Exception counts parcels with exceptional delivery status.
    exception: int = 0


setattr(ShipmentStatusCounts, '__module__', 'dex_connectors.fiftyonetracking.models')
