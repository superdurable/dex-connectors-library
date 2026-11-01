# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from .._generated.mock import Mock as Mock
from .._generated.mock import get_photo_found as get_photo_found
from .._generated.mock import get_photo_found_default_photo as get_photo_default_answer
from .._generated.mock import get_photo_not_found as get_photo_not_found
from .._generated.mock import search_photos_searched as search_photos_searched
from .._generated.mock import search_photos_searched_default_photos as search_photos_default_answer

__all__ = [
    'Mock',
    'search_photos_searched',
    'search_photos_default_answer',
    'get_photo_found',
    'get_photo_not_found',
    'get_photo_default_answer',
]
