# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Close lead pages and leads."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListLeadsInput:
    """Offset page of leads; limit zero means 100, at most 100."""

    skip: int = field(default=0, metadata={"dex_json_omit_default": True})
    limit: int = field(default=0, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class GetLeadInput:
    """Close lead ID, which starts with lead_."""

    lead_id: str = field(default="", metadata={"dex_json_name": "leadId"})


@dataclass(frozen=True, slots=True)
class Lead:
    """Close lead identity and display name."""

    id: str = ""
    name: str = ""


@dataclass(frozen=True, slots=True)
class LeadPage:
    """One page of leads; has_more reports whether Close has another page."""

    leads: tuple[Lead, ...] = ()
    has_more: bool = field(default=False, metadata={"dex_json_name": "hasMore"})
