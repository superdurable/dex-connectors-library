# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from .._generated.mock import Mock as Mock
from .._generated.mock import get_stats_found as get_stats_found
from .._generated.mock import get_stats_found_default as get_stats_default_answer
from .._generated.mock import list_templates_listed as list_templates_listed
from .._generated.mock import list_templates_listed_default as list_templates_default_answer

__all__ = [
    'Mock',
    'get_stats_found',
    'get_stats_default_answer',
    'list_templates_listed',
    'list_templates_default_answer',
]
