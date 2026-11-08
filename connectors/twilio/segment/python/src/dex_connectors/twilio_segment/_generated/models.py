# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetSourceInput selects a Source by its non-secret Segment ID.
@dataclass(frozen=True, slots=True)
class GetSourceInput:
    # SourceID is the opaque ID returned by the Segment Public API.
    source_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceId'})


setattr(GetSourceInput, '__module__', 'dex_connectors.twilio_segment.models')


# SourceProfile excludes write keys and Source settings.
@dataclass(frozen=True, slots=True)
class SourceProfile:
    # ID is the Segment Source ID.
    id: str = ''
    # Slug is the Source's human-readable identifier.
    slug: str = ''
    # Name is the optional display name, which may be blank.
    name: str = ''


setattr(SourceProfile, '__module__', 'dex_connectors.twilio_segment.models')
