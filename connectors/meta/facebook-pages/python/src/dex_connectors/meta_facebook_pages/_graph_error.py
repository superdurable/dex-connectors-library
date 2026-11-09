# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Meta Graph API error envelope decisions shared by Queries and the publishing Mutation."""

import json
from dataclasses import dataclass

_THROTTLE_CODES = frozenset({4, 17, 32, 341, 613})


@dataclass(frozen=True, slots=True)
class GraphError:
    """The Graph API error fields used for retry decisions."""

    code: int
    is_transient: bool

    @property
    def is_throttled(self) -> bool:
        """Report Meta's documented rate-limit codes, which Graph returns with HTTP 4xx."""
        return self.code in _THROTTLE_CODES or 80000 <= self.code <= 80014


def decode_graph_error(body: bytes) -> GraphError | None:
    """Return None when the body is not a Graph API error envelope."""
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(document, dict):
        return None
    error = document.get("error")
    if not isinstance(error, dict):
        return None
    code = error.get("code")
    is_transient = error.get("is_transient")
    code = 0 if code is None else code
    is_transient = False if is_transient is None else is_transient
    if type(code) is not int or type(is_transient) is not bool:
        return None
    return GraphError(code, is_transient)
