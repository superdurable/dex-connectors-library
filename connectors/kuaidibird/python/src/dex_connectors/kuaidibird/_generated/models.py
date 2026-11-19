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


# QueryTrackingInput identifies one carrier waybill for the base instant-query API.
@dataclass(frozen=True, slots=True)
class QueryTrackingInput:
    # CarrierCode is the uppercase code from KuaidiBird's official carrier table.
    carrier_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'carrierCode'})
    # TrackingNumber is the carrier waybill number, at most 30 characters.
    tracking_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'trackingNumber'})


setattr(QueryTrackingInput, '__module__', 'dex_connectors.kuaidibird.models')


# TrackingEvent is one provider-reported event in the provider's returned order.
@dataclass(frozen=True, slots=True)
class TrackingEvent:
    # Time is the event timestamp string returned by KuaidiBird.
    time: str = ''
    # Content is the carrier's event description; carriers may include contact details in this text.
    content: str = ''


setattr(TrackingEvent, '__module__', 'dex_connectors.kuaidibird.models')


# QueryTrackingOutput contains the current state and event history for one waybill.
@dataclass(frozen=True, slots=True)
class QueryTrackingOutput:
    # CarrierCode is the provider-confirmed carrier code.
    carrier_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'carrierCode'})
    # TrackingNumber is the provider-confirmed waybill number.
    tracking_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'trackingNumber'})
    # State is the provider's open state code, preserving unknown future codes.
    state: str = ''
    # Events preserves the provider's event order.
    events: tuple[TrackingEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


QueryTrackingOutput.__annotations__['events'] = tuple[TrackingEvent, ...] | None
QueryTrackingOutput.__dataclass_fields__['events'].type = tuple[TrackingEvent, ...] | None
setattr(QueryTrackingOutput, '__module__', 'dex_connectors.kuaidibird.models')
