# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from .._generated.mock import Mock as Mock
from .._generated.mock import save_object_saved as save_object_saved
from .._generated.mock import save_object_saved_default_object as save_object_default_answer
from .._generated.mock import search_searched as search_searched
from .._generated.mock import search_searched_default_index as search_default_answer

__all__ = [
    'Mock',
    'search_searched',
    'search_default_answer',
    'save_object_saved',
    'save_object_default_answer',
]
