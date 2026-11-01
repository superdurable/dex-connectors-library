# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetEventsInput selects one sending domain and a bounded events page.
@dataclass(frozen=True, slots=True)
class GetEventsInput:
    # Domain is a Mailgun sending domain such as example.test.
    domain: str = ''
    # Limit is at most 300 events; zero uses Mailgun's default.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(GetEventsInput, '__module__', 'dex_connectors.mailgun.models')


# Event identifies one Mailgun delivery event.
@dataclass(frozen=True, slots=True)
class Event:
    # ID is the event's provider identifier.
    id: str = ''
    # Event is the event kind, such as delivered.
    event: str = ''
    # Timestamp is epoch seconds, including any fractional seconds.
    timestamp: float = 0.0


setattr(Event, '__module__', 'dex_connectors.mailgun.models')


# EventsOutput contains one domain events page.
@dataclass(frozen=True, slots=True)
class EventsOutput:
    # Events is the returned event page.
    events: tuple[Event, ...] | None = None


setattr(EventsOutput, '__module__', 'dex_connectors.mailgun.models')


# ListDomainsInput selects one account domains page.
@dataclass(frozen=True, slots=True)
class ListDomainsInput:
    # Limit is at most 1000 domains; zero uses Mailgun's default.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Skip is the zero-based offset into the account domain list.
    skip: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListDomainsInput, '__module__', 'dex_connectors.mailgun.models')


# Domain identifies one Mailgun sending domain.
@dataclass(frozen=True, slots=True)
class Domain:
    # Name is the DNS domain name.
    name: str = ''
    # State is the provider's verification state.
    state: str = ''


setattr(Domain, '__module__', 'dex_connectors.mailgun.models')


# DomainsOutput holds one domains page and the account count.
@dataclass(frozen=True, slots=True)
class DomainsOutput:
    # Domains is the page of sending domains.
    domains: tuple[Domain, ...] | None = None
    # TotalCount is the provider-reported account domain count.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})


setattr(DomainsOutput, '__module__', 'dex_connectors.mailgun.models')
