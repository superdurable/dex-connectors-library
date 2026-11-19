# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetProjectAPISummaryInput identifies one Apifox project by positive decimal ID.
@dataclass(frozen=True, slots=True)
class GetProjectAPISummaryInput:
    # ProjectID is the decimal ID shown in Apifox Project Settings.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectID'})


setattr(GetProjectAPISummaryInput, '__module__', 'dex_connectors.apifox.models')


# GetProjectAPISummaryOutput excludes the exported API document and endpoint details.
@dataclass(frozen=True, slots=True)
class GetProjectAPISummaryOutput:
    # Title is the exported OpenAPI info title.
    title: str = ''
    # Version is the exported OpenAPI info version.
    version: str = ''
    # EndpointCount counts HTTP methods across the exported paths.
    endpoint_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'endpointCount'})


setattr(GetProjectAPISummaryOutput, '__module__', 'dex_connectors.apifox.models')
