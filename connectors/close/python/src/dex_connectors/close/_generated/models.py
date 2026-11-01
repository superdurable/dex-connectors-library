# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListLeadsInput:
    """Typed ListLeadsInput value from the Go connector contract."""

    skip: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListLeadsInput, '__module__', 'dex_connectors.close.models')


@dataclass(frozen=True, slots=True)
class Lead:
    """Typed Lead value from the Go connector contract."""

    id: str = ''
    name: str = ''


setattr(Lead, '__module__', 'dex_connectors.close.models')


@dataclass(frozen=True, slots=True)
class LeadPage:
    """Typed LeadPage value from the Go connector contract."""

    leads: tuple[Lead, ...] | None = None
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})


setattr(LeadPage, '__module__', 'dex_connectors.close.models')


@dataclass(frozen=True, slots=True)
class GetLeadInput:
    """Typed GetLeadInput value from the Go connector contract."""

    lead_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'leadId'})


setattr(GetLeadInput, '__module__', 'dex_connectors.close.models')
