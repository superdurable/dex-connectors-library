# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed SendGrid statistics and transactional template pages."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class GetStatsInput:
    """Inclusive YYYY-MM-DD date range; a blank end date means today."""

    start_date: str = field(default="", metadata={"dex_json_name": "startDate"})
    end_date: str = field(default="", metadata={"dex_json_name": "endDate", "dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class StatsDay:
    """One YYYY-MM-DD reporting date with its email request and delivered counts."""

    date: str = ""
    requests: int = 0
    delivered: int = 0


@dataclass(frozen=True, slots=True)
class StatsOutput:
    """The returned daily global email statistics."""

    days: tuple[StatsDay, ...] = ()


@dataclass(frozen=True, slots=True)
class ListTemplatesInput:
    """Generation (dynamic or legacy; blank is dynamic), page size 1-200 (zero is 100), and page token."""

    generation: str = field(default="", metadata={"dex_json_omit_default": True})
    page_size: int = field(default=0, metadata={"dex_json_name": "pageSize", "dex_json_omit_default": True})
    page_token: str = field(
        default="", metadata={"dex_json_name": "pageToken", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class Template:
    """A transactional template's provider ID, name, and generation."""

    id: str = ""
    name: str = ""
    generation: str = ""


@dataclass(frozen=True, slots=True)
class TemplatesOutput:
    """One template page, the provider-reported count, and the next page token when supplied."""

    templates: tuple[Template, ...] = ()
    count: int = 0
    next_page_token: str = field(
        default="", metadata={"dex_json_name": "nextPageToken", "dex_json_omit_default": True}
    )
