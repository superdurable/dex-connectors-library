# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Mailgun event and domain pages."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class GetEventsInput:
    """Sending domain and events per page; zero limit uses Mailgun's default, at most 300."""

    domain: str = ""
    limit: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class Event:
    """Event ID, type such as delivered, and Unix timestamp in seconds."""

    id: str = ""
    event: str = ""
    timestamp: float = 0.0


@dataclass(frozen=True, slots=True)
class EventsOutput:
    """Events on the first page."""

    events: tuple[Event, ...] = ()


@dataclass(frozen=True, slots=True)
class ListDomainsInput:
    """Domains per page (zero uses Mailgun's default, at most 1000) and the number to skip."""

    limit: int = field(default=0, metadata={"dex_json_omit_default": True})
    skip: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class Domain:
    """Domain name and verification state such as active."""

    name: str = ""
    state: str = ""


@dataclass(frozen=True, slots=True)
class DomainsOutput:
    """Domains on the page and Mailgun's total domain count."""

    domains: tuple[Domain, ...] = ()
    total_count: int = field(default=0, metadata={"dex_json_name": "totalCount"})
