# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The change fetcher contract, provider signals, and the request limiter hook."""

import contextvars
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import timedelta
from typing import Final, Generic, Protocol, TypeVar

from ..projectconfig.poll_cursor import KeyedSnapshot
from ..trigger_delivery import TriggerContext

ChangeT = TypeVar("ChangeT")
ChangeT_co = TypeVar("ChangeT_co", covariant=True)

MAXIMUM_CHANGES_PER_PAGE: Final = 500
"""Bound of `ChangePage.changes`; a durable Trigger inbox holds at most 1000 pending events."""

_MAXIMUM_CAUSE_DEPTH = 32


@dataclass(frozen=True, slots=True)
class Cursor:
    """A committed provider position: an opaque connector token and an optional keyed snapshot."""

    token: str = ""
    """The connector's encoded position, at most 64 KiB of UTF-8. Empty is a valid position.

    Connectors encode structured positions, such as JSON, themselves.
    """
    snapshot: KeyedSnapshot | None = None
    """The keyed snapshot when the poller enables one with `KeyedSnapshotOptions`; None otherwise.

    It commits atomically with the token, so it never advances past unrecorded changes.
    """


@dataclass(frozen=True, slots=True)
class ChangePage(Generic[ChangeT_co]):
    """One page of provider changes after a cursor."""

    changes: Sequence[ChangeT_co] = ()
    """Changes in provider order, at most 500. The kit records and delivers them in order."""
    next_cursor: Cursor = Cursor()
    """The cursor that follows the changes; committed only after every target recorded or consumed them."""
    has_more_pages: bool = False
    """Whether `fetch_changes(next_cursor)` can return more changes immediately."""


class ChangeFetcher(Protocol[ChangeT_co]):
    """Reads one provider change feed; it never touches storage or Trigger targets.

    A poller owns its fetcher and calls it from one task at a time. Every event identity derived from
    a change must be a pure function of that change, so a refetch from the same cursor after a crash
    yields the same Trigger event IDs and the Dex targets deduplicate them. Await
    `wait_for_poll_request` before every provider request.
    """

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Return the provider's current position without reporting existing items.

        The kit calls it once when a cursor is first created and again after `fetch_changes` raises an
        error marked with `mark_cursor_invalid`.
        """
        ...

    async def fetch_changes(self, context: TriggerContext, cursor: Cursor) -> ChangePage[ChangeT_co]:
        """Return one page of changes after cursor.

        Raise `mark_cursor_invalid(error)` when the provider rejects the cursor permanently,
        `mark_rate_limited(error, retry_after)` for a quota or 429 response, and any other exception for
        a failure the next poll may fix.
        """
        ...


class CursorInvalidError(Exception):
    """Marks a cursor the provider can never accept again; build it with `mark_cursor_invalid`."""

    def __init__(self, cause: BaseException) -> None:
        """Wrap the provider failure, which becomes `__cause__`."""
        super().__init__(f"poll cursor is invalid: {cause}")
        self.__cause__ = cause


def mark_cursor_invalid(error: BaseException) -> CursorInvalidError:
    """Report that the provider rejected a cursor permanently, such as Google Calendar 410 Gone.

    The poller then reseeds with `seed_cursor`, increments the stored reset count, and logs a WARN record;
    changes between the last commit and the reseed are not delivered. An already marked error is
    returned unchanged. Raise the result from `fetch_changes`.
    """
    return error if isinstance(error, CursorInvalidError) else CursorInvalidError(error)


def _find_in_causes(error: BaseException | None, error_type: type[BaseException]) -> BaseException | None:
    current = error
    for _ in range(_MAXIMUM_CAUSE_DEPTH):
        if current is None:
            return None
        if isinstance(current, error_type):
            return current
        current = current.__cause__
    return None


def is_cursor_invalid(error: BaseException | None) -> bool:
    """Report whether error or an error in its `__cause__` chain was marked with `mark_cursor_invalid`."""
    return _find_in_causes(error, CursorInvalidError) is not None


class RateLimitedError(Exception):
    """A provider quota or 429 response; the runner pauses every poller of its connection.

    Provider quotas are per user or per OAuth client, so one poller's limit applies to all of them.
    """

    def __init__(self, cause: BaseException | None = None, retry_after: timedelta = timedelta(0)) -> None:
        """Wrap the provider failure with its requested wait; a negative wait counts as zero."""
        super().__init__(
            "provider rate limited the poll" if cause is None else f"provider rate limited the poll: {cause}"
        )
        self.retry_after = max(retry_after, timedelta(0))
        """The provider's requested wait, or zero when it gave none.

        With zero the runner backs off exponentially; otherwise it waits max(retry_after, interval).
        """
        if cause is not None:
            self.__cause__ = cause


def mark_rate_limited(error: BaseException, retry_after: timedelta | None = None) -> RateLimitedError:
    """Wrap error as a RateLimitedError with the provider's requested wait.

    For a Retry-After header pass `timedelta(seconds=providerhttp.parse_retry_after(header, now))`; None
    or a negative wait counts as zero. Raise the result from `fetch_changes` or `seed_cursor`.
    """
    return RateLimitedError(error, retry_after if retry_after is not None else timedelta(0))


class RequestLimiter(Protocol):
    """Waits before each provider request a poller makes, such as a token bucket shared in the process."""

    async def wait_for_poll_request(self, context: TriggerContext) -> None:
        """Return once one more provider request may start; an exception counts as a failed poll."""
        ...


_REQUEST_LIMITER: contextvars.ContextVar[RequestLimiter | None] = contextvars.ContextVar(
    "dex_connectors_poll_request_limiter", default=None
)


async def wait_for_poll_request(context: TriggerContext) -> None:
    """Wait on the RequestLimiter of the runner that called the fetcher.

    Fetchers await it before every provider request, including each page of a seed. It returns at once
    when the runner has no limiter, and raises TriggerDeliveryAbortedError once the context is cancelled.
    The runner's limiter reaches fetchers through a context variable of the polling task.
    """
    context.cancellation.raise_if_cancelled()
    limiter = _REQUEST_LIMITER.get()
    if limiter is not None:
        await limiter.wait_for_poll_request(context)
