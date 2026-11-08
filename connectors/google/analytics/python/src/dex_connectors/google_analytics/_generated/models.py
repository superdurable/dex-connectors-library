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


# RunReportInput selects one GA4 report page with up to ten dimensions and metrics.
@dataclass(frozen=True, slots=True)
class RunReportInput:
    # PropertyID is the numeric GA4 Property ID from Analytics Admin.
    property_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'propertyId'})
    # StartDate is the inclusive date in YYYY-MM-DD form.
    start_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'startDate'})
    # EndDate is the inclusive date in YYYY-MM-DD form.
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate'})
    # Dimensions are GA4 Data API dimension names in requested order.
    dimensions: tuple[str, ...] = ()
    # Metrics are GA4 Data API metric names in requested order.
    metrics: tuple[str, ...] = ()
    # Limit is the page size from 1 to 10000; zero uses 100.
    limit: int = 0
    # Offset is the zero-based row offset.
    offset: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'dimensions', _replace_nil_slices(self.dimensions, 1))
        object.__setattr__(self, 'metrics', _replace_nil_slices(self.metrics, 1))


RunReportInput.__annotations__['dimensions'] = tuple[str, ...] | None
RunReportInput.__dataclass_fields__['dimensions'].type = tuple[str, ...] | None
RunReportInput.__annotations__['metrics'] = tuple[str, ...] | None
RunReportInput.__dataclass_fields__['metrics'].type = tuple[str, ...] | None
setattr(RunReportInput, '__module__', 'dex_connectors.google_analytics.models')


# ReportRow contains ordered dimension and metric strings from one GA4 row.
@dataclass(frozen=True, slots=True)
class ReportRow:
    # Dimensions match the input dimension order.
    dimensions: tuple[str, ...] = ()
    # Metrics match the input metric order.
    metrics: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'dimensions', _replace_nil_slices(self.dimensions, 1))
        object.__setattr__(self, 'metrics', _replace_nil_slices(self.metrics, 1))


ReportRow.__annotations__['dimensions'] = tuple[str, ...] | None
ReportRow.__dataclass_fields__['dimensions'].type = tuple[str, ...] | None
ReportRow.__annotations__['metrics'] = tuple[str, ...] | None
ReportRow.__dataclass_fields__['metrics'].type = tuple[str, ...] | None
setattr(ReportRow, '__module__', 'dex_connectors.google_analytics.models')


# ReportPage is a bounded GA4 report page and the total matching row count.
@dataclass(frozen=True, slots=True)
class ReportPage:
    # Rows are at most the requested limit.
    rows: tuple[ReportRow, ...] = ()
    # RowCount is the total number of matching rows across all pages.
    row_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'rowCount'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'rows', _replace_nil_slices(self.rows, 1))


ReportPage.__annotations__['rows'] = tuple[ReportRow, ...] | None
ReportPage.__dataclass_fields__['rows'].type = tuple[ReportRow, ...] | None
setattr(ReportPage, '__module__', 'dex_connectors.google_analytics.models')


# GetPropertyInput selects one numeric GA4 property ID.
@dataclass(frozen=True, slots=True)
class GetPropertyInput:
    # PropertyID is the numeric GA4 Property ID from Analytics Admin.
    property_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'propertyId'})


setattr(GetPropertyInput, '__module__', 'dex_connectors.google_analytics.models')


# Property contains non-secret GA4 property metadata.
@dataclass(frozen=True, slots=True)
class Property:
    # Name is the GA4 resource name, such as properties/123456789.
    name: str = ''
    # DisplayName is the human-readable name in Analytics Admin.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # TimeZone is the IANA reporting time zone.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})
    # CurrencyCode is the property's ISO 4217 reporting currency when configured.
    currency_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'currencyCode'})


setattr(Property, '__module__', 'dex_connectors.google_analytics.models')


# ListAudiencesInput selects one page of audiences for a numeric property ID.
@dataclass(frozen=True, slots=True)
class ListAudiencesInput:
    # PropertyID is the numeric GA4 Property ID from Analytics Admin.
    property_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'propertyId'})
    # PageSize is from 1 to 200; zero uses 50.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # PageToken is Google's opaque cursor from a prior page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken'})


setattr(ListAudiencesInput, '__module__', 'dex_connectors.google_analytics.models')


# Audience contains non-secret identity and display metadata.
@dataclass(frozen=True, slots=True)
class Audience:
    # Name is the GA4 audience resource name.
    name: str = ''
    # DisplayName is the name visible in Analytics Admin.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # Description is the audience description, which may be blank.
    description: str = ''


setattr(Audience, '__module__', 'dex_connectors.google_analytics.models')


# AudiencesPage contains one bounded audience page and optional next token.
@dataclass(frozen=True, slots=True)
class AudiencesPage:
    # Audiences are the audience identities on this page.
    audiences: tuple[Audience, ...] = ()
    # NextPageToken is Google's opaque cursor; blank means the page is final.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'audiences', _replace_nil_slices(self.audiences, 1))


AudiencesPage.__annotations__['audiences'] = tuple[Audience, ...] | None
AudiencesPage.__dataclass_fields__['audiences'].type = tuple[Audience, ...] | None
setattr(AudiencesPage, '__module__', 'dex_connectors.google_analytics.models')
