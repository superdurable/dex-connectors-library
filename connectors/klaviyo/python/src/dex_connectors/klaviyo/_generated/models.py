# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class GetProfileInput:
    """Typed GetProfileInput value from the Go connector contract."""

    id: str = ''


setattr(GetProfileInput, '__module__', 'dex_connectors.klaviyo.models')


@dataclass(frozen=True, slots=True)
class Profile:
    """Typed Profile value from the Go connector contract."""

    id: str = ''
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Profile, '__module__', 'dex_connectors.klaviyo.models')


@dataclass(frozen=True, slots=True)
class CreateEventInput:
    """Typed CreateEventInput value from the Go connector contract."""

    email: str = ''
    metric_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'metricName'})
    properties: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CreateEventInput, '__module__', 'dex_connectors.klaviyo.models')


@dataclass(frozen=True, slots=True)
class EventAcceptance:
    """Typed EventAcceptance value from the Go connector contract."""

    accepted: bool = False


setattr(EventAcceptance, '__module__', 'dex_connectors.klaviyo.models')
