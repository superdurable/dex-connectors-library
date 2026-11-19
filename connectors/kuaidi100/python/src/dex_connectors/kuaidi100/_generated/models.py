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


# QueryTrackingInput identifies one carrier waybill. Phone is only needed by carriers requiring recipient verification.
@dataclass(frozen=True, slots=True)
class QueryTrackingInput:
    # CarrierCode is the lowercase code from Kuaidi100's official carrier-code table.
    carrier_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'carrierCode'})
    # TrackingNumber is the 6–32 character carrier waybill number.
    tracking_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'trackingNumber'})
    # Phone is the sender or recipient phone for carriers such as SF Express and ZTO; it is never returned.
    phone: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(QueryTrackingInput, '__module__', 'dex_connectors.kuaidi100.models')


# TrackingEvent is one provider-reported event, in latest-first order.
@dataclass(frozen=True, slots=True)
class TrackingEvent:
    # Time is the provider's event timestamp string.
    time: str = ''
    # Content is the provider's parcel-status narrative.
    content: str = ''


setattr(TrackingEvent, '__module__', 'dex_connectors.kuaidi100.models')


# QueryTrackingOutput contains one waybill's current state and latest-first events.
@dataclass(frozen=True, slots=True)
class QueryTrackingOutput:
    # CarrierCode is the provider-confirmed carrier code.
    carrier_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'carrierCode'})
    # TrackingNumber is the provider-confirmed waybill number.
    tracking_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'trackingNumber'})
    # State is the provider's open state code; unknown future codes are preserved.
    state: str = ''
    # Events contains the carrier events in provider order, newest first.
    events: tuple[TrackingEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


QueryTrackingOutput.__annotations__['events'] = tuple[TrackingEvent, ...] | None
QueryTrackingOutput.__dataclass_fields__['events'].type = tuple[TrackingEvent, ...] | None
setattr(QueryTrackingOutput, '__module__', 'dex_connectors.kuaidi100.models')
