# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Gorgias ticket event types the ticketChanged Trigger reads, and its binding configuration rules."""

from enum import StrEnum
from typing import Final

from .models import TicketChangedTriggerConfiguration


class TicketEventType(StrEnum):
    """A Gorgias event type whose object is a ticket, spelled as Gorgias's event log spells it.

    Message events, such as ticket-message-created, are not among them: their object is a message, and
    Gorgias documents no ticket ID for it.
    """

    CREATED = "ticket-created"
    """A ticket was created."""
    UPDATED = "ticket-updated"
    """A ticket's fields changed."""
    CLOSED = "ticket-closed"
    """A ticket was closed."""
    REOPENED = "ticket-reopened"
    """A closed ticket was reopened."""
    ASSIGNED = "ticket-assigned"
    """A ticket was assigned to a user."""
    UNASSIGNED = "ticket-unassigned"
    """A ticket's user assignment was removed."""
    TEAM_ASSIGNED = "ticket-team-assigned"
    """A ticket was assigned to a team."""
    TEAM_UNASSIGNED = "ticket-team-unassigned"
    """A ticket's team assignment was removed."""
    TAGS_ADDED = "ticket-tags-added"
    """Tags were added to a ticket."""
    TAGS_REMOVED = "ticket-tags-removed"
    """Tags were removed from a ticket."""
    SUBJECT_UPDATED = "ticket-subject-updated"
    """A ticket's subject changed."""
    CUSTOMER_UPDATED = "ticket-customer-updated"
    """A ticket's customer changed."""
    SNOOZED = "ticket-snoozed"
    """A ticket was snoozed."""
    SELF_UNSNOOZED = "ticket-self-unsnoozed"
    """A snoozed ticket woke up on its own."""
    MARKED_SPAM = "ticket-marked-spam"
    """A ticket was marked as spam."""
    UNMARKED_SPAM = "ticket-unmarked-spam"
    """A ticket was no longer marked as spam."""
    TRASHED = "ticket-trashed"
    """A ticket was moved to the trash."""
    UNTRASHED = "ticket-untrashed"
    """A ticket was restored from the trash."""
    DELETED = "ticket-deleted"
    """A ticket was deleted."""
    MERGED = "ticket-merged"
    """A ticket was merged into another."""
    SPLIT = "ticket-split"
    """A ticket was split."""
    SLA_POLICY_ASSIGNED = "ticket-sla-policy-assigned"
    """An SLA policy was assigned to a ticket."""


TICKET_EVENT_TYPES: Final[tuple[str, ...]] = tuple(event_type.value for event_type in TicketEventType)
"""Every event type ticketChanged reads, in the order each poll requests them."""


def validate_ticket_changed_trigger_configuration(configuration: TicketChangedTriggerConfiguration) -> None:
    """Reject an unknown or repeated event type; an empty configuration is valid.

    Raises ValueError naming the entry.
    """
    seen: set[str] = set()
    for event_type in configuration.event_types:
        if event_type not in TICKET_EVENT_TYPES:
            raise ValueError(
                f"ticketChanged eventTypes entry {_go_quote(event_type)} is not a ticket event type that "
                "ticketChanged reads"
            )
        if event_type in seen:
            raise ValueError(f"ticketChanged eventTypes lists {_go_quote(event_type)} twice")
        seen.add(event_type)


def delivers_event_type(configuration: TicketChangedTriggerConfiguration, event_type: str) -> bool:
    """Report whether the binding delivers event_type: every read type when event_types is empty."""
    if not configuration.event_types:
        return event_type in TICKET_EVENT_TYPES
    return event_type in configuration.event_types


def _go_quote(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'
