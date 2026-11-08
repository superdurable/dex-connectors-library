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


# ListDatasetsInput selects one bounded page of datasets in a Google Cloud project.
@dataclass(frozen=True, slots=True)
class ListDatasetsInput:
    # ProjectID is the Google Cloud project ID shown in BigQuery Studio.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})
    # MaxResults limits this page to 1 through 100 datasets; zero uses 100.
    max_results: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxResults', 'dex_json_omit_default': True})
    # PageToken is returned by the previous page; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListDatasetsInput, '__module__', 'dex_connectors.google_bigquery.models')


# Dataset identifies a BigQuery dataset without exposing its full metadata.
@dataclass(frozen=True, slots=True)
class Dataset:
    # ID is the dataset ID within ProjectID.
    id: str = ''
    # ProjectID is the project containing this dataset.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId'})
    # FriendlyName is an optional display name.
    friendly_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'friendlyName'})


setattr(Dataset, '__module__', 'dex_connectors.google_bigquery.models')


# DatasetsPage holds one page of dataset identities and its next cursor.
@dataclass(frozen=True, slots=True)
class DatasetsPage:
    # Datasets are visible to the authorized Google identity.
    datasets: tuple[Dataset, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'datasets', _replace_nil_slices(self.datasets, 1))


DatasetsPage.__annotations__['datasets'] = tuple[Dataset, ...] | None
DatasetsPage.__dataclass_fields__['datasets'].type = tuple[Dataset, ...] | None
setattr(DatasetsPage, '__module__', 'dex_connectors.google_bigquery.models')
