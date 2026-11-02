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


# GetStatsInput selects a global email statistics date range.
@dataclass(frozen=True, slots=True)
class GetStatsInput:
    # StartDate is the first day in YYYY-MM-DD format.
    start_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'startDate'})
    # EndDate is the final day in YYYY-MM-DD format; blank means today.
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate', 'dex_json_omit_default': True})


setattr(GetStatsInput, '__module__', 'dex_connectors.sendgrid.models')


# StatsDay gives the requested and delivered counts for one date.
@dataclass(frozen=True, slots=True)
class StatsDay:
    # Date is the reporting date in YYYY-MM-DD format.
    date: str = ''
    # Requests is the number of email requests.
    requests: int = 0
    # Delivered is the number confirmed delivered.
    delivered: int = 0


setattr(StatsDay, '__module__', 'dex_connectors.sendgrid.models')


# StatsOutput contains daily global email statistics.
@dataclass(frozen=True, slots=True)
class StatsOutput:
    # Days is the returned date series.
    days: tuple[StatsDay, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'days', _replace_nil_slices(self.days, 1))


StatsOutput.__annotations__['days'] = tuple[StatsDay, ...] | None
StatsOutput.__dataclass_fields__['days'].type = tuple[StatsDay, ...] | None
setattr(StatsOutput, '__module__', 'dex_connectors.sendgrid.models')


# ListTemplatesInput chooses a page of transactional templates.
@dataclass(frozen=True, slots=True)
class ListTemplatesInput:
    # Generation is dynamic or legacy; blank selects dynamic.
    generation: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PageSize is 1 to 200 templates; zero selects 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the token from the preceding page; blank starts at page one.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListTemplatesInput, '__module__', 'dex_connectors.sendgrid.models')


# Template identifies a transactional template.
@dataclass(frozen=True, slots=True)
class Template:
    # ID is the provider template ID.
    id: str = ''
    # Name is the human-readable template name.
    name: str = ''
    # Generation is legacy or dynamic.
    generation: str = ''


setattr(Template, '__module__', 'dex_connectors.sendgrid.models')


# TemplatesOutput holds the returned template page.
@dataclass(frozen=True, slots=True)
class TemplatesOutput:
    # Templates is the returned page of templates.
    templates: tuple[Template, ...] = ()
    # Count is the provider-reported count; its precise meaning is unverified.
    count: int = 0
    # NextPageToken selects the next page when the provider supplies one.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'templates', _replace_nil_slices(self.templates, 1))


TemplatesOutput.__annotations__['templates'] = tuple[Template, ...] | None
TemplatesOutput.__dataclass_fields__['templates'].type = tuple[Template, ...] | None
setattr(TemplatesOutput, '__module__', 'dex_connectors.sendgrid.models')
