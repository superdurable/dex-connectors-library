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


# ListOrganizationsInput selects a one-based page; zero defaults to the first page.
@dataclass(frozen=True, slots=True)
class ListOrganizationsInput:
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListOrganizationsInput, '__module__', 'dex_connectors.eventbrite.models')


# OrganizationMetadata identifies an organization without member details.
@dataclass(frozen=True, slots=True)
class OrganizationMetadata:
    id: str = ''
    name: str = ''


setattr(OrganizationMetadata, '__module__', 'dex_connectors.eventbrite.models')


# OrganizationsPage is one page of organizations visible to the connected account.
@dataclass(frozen=True, slots=True)
class OrganizationsPage:
    organizations: tuple[OrganizationMetadata, ...] = ()
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'organizations', _replace_nil_slices(self.organizations, 1))


OrganizationsPage.__annotations__['organizations'] = tuple[OrganizationMetadata, ...] | None
OrganizationsPage.__dataclass_fields__['organizations'].type = tuple[OrganizationMetadata, ...] | None
setattr(OrganizationsPage, '__module__', 'dex_connectors.eventbrite.models')


# ListEventsInput selects one organization's one-based event page.
@dataclass(frozen=True, slots=True)
class ListEventsInput:
    organization_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationId'})
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListEventsInput, '__module__', 'dex_connectors.eventbrite.models')


# EventMetadata contains an event's identity, name, and status.
@dataclass(frozen=True, slots=True)
class EventMetadata:
    id: str = ''
    name: str = ''
    status: str = ''


setattr(EventMetadata, '__module__', 'dex_connectors.eventbrite.models')


# EventsPage is one page of an organization's events.
@dataclass(frozen=True, slots=True)
class EventsPage:
    events: tuple[EventMetadata, ...] = ()
    page_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageNumber'})
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


EventsPage.__annotations__['events'] = tuple[EventMetadata, ...] | None
EventsPage.__dataclass_fields__['events'].type = tuple[EventMetadata, ...] | None
setattr(EventsPage, '__module__', 'dex_connectors.eventbrite.models')


# GetAttendeeInput selects an attendee within an event.
@dataclass(frozen=True, slots=True)
class GetAttendeeInput:
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId'})
    attendee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'attendeeId'})


setattr(GetAttendeeInput, '__module__', 'dex_connectors.eventbrite.models')


# AttendeeMetadata excludes the attendee profile, answers, barcode, and email.
@dataclass(frozen=True, slots=True)
class AttendeeMetadata:
    id: str = ''
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId'})
    status: str = ''
    created_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAt'})


setattr(AttendeeMetadata, '__module__', 'dex_connectors.eventbrite.models')
