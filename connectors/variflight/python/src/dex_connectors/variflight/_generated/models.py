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


# GetFlightStatusInput selects a flight number and local departure date.
@dataclass(frozen=True, slots=True)
class GetFlightStatusInput:
    # FlightNumber is the airline code and number, such as GX8915.
    flight_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'flightNumber'})
    # DepartureDate is the local flight departure date in YYYY-MM-DD format.
    departure_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'departureDate'})


setattr(GetFlightStatusInput, '__module__', 'dex_connectors.variflight.models')


# FlightStatus contains a small subset of the provider's flight data.
@dataclass(frozen=True, slots=True)
class FlightStatus:
    # FlightNumber is the provider's normalized flight number.
    flight_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'flightNumber'})
    # State is the provider's English flight status.
    state: str = ''
    # StateCode is VariFlight's numeric flight status code.
    state_code: int = _dataclass_field(default=0, metadata={'dex_json_name': 'stateCode'})
    # ScheduledDeparture is the planned local departure timestamp.
    scheduled_departure: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduledDeparture'})
    # ActualDeparture is the actual local departure timestamp when available.
    actual_departure: str = _dataclass_field(default='', metadata={'dex_json_name': 'actualDeparture'})


setattr(FlightStatus, '__module__', 'dex_connectors.variflight.models')


# GetFlightStatusOutput contains the provider record count and at most twenty flight statuses.
@dataclass(frozen=True, slots=True)
class GetFlightStatusOutput:
    # Total is the number of records returned by the provider.
    total: int = 0
    # Flights contains at most the first twenty records.
    flights: tuple[FlightStatus, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'flights', _replace_nil_slices(self.flights, 1))


GetFlightStatusOutput.__annotations__['flights'] = tuple[FlightStatus, ...] | None
GetFlightStatusOutput.__dataclass_fields__['flights'].type = tuple[FlightStatus, ...] | None
setattr(GetFlightStatusOutput, '__module__', 'dex_connectors.variflight.models')
