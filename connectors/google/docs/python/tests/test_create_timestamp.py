# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Docs provider timestamp validation."""

import pytest

from dex_connectors.google_docs.create import _created


def test_rejects_invalid_go_created_timestamp() -> None:
    """Reject a provider timestamp Go's JSON decoder cannot parse."""
    with pytest.raises(ValueError):
        _created({
            "id": "document123",
            "mimeType": "application/vnd.google-apps.document",
            "createdTime": "2026-01-01T24:00:00Z",
        })
