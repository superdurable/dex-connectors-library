# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any


def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchTicketsInput selects tickets with typed filters that the connector turns
# into one Zendesk search query. Different filters narrow each other; several
# statuses or several tags match any of them. At least one filter is required.
@dataclass(frozen=True, slots=True)
class SearchTicketsInput:
    # Statuses matches tickets in any of these Zendesk statuses. Empty matches every status.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={"dex_json_omit_default": True})
    # RequesterEmail matches tickets whose requester has this email address, such as jane@example.com.
    requester_email: str = _dataclass_field(
        default="", metadata={"dex_json_name": "requesterEmail", "dex_json_omit_default": True}
    )
    # Tags matches tickets carrying any of these lowercase tags.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={"dex_json_omit_default": True})
    # UpdatedSince matches tickets updated at or after this RFC 3339 instant with an explicit
    # offset, such as 2026-01-21T00:00:00Z. Blank applies no time filter.
    updated_since: str = _dataclass_field(
        default="", metadata={"dex_json_name": "updatedSince", "dex_json_omit_default": True}
    )
    # Text holds up to 32 plain keywords that must all appear in the ticket's subject,
    # description, or comments. Search operators such as : < > = " * and a leading - or +
    # are rejected, so the text cannot change the other filters. Blank matches any text.
    text: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})
    # PageSize is 1 to MaxSearchPageSize tickets; zero uses DefaultSearchPageSize.
    page_size: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "pageSize", "dex_json_omit_default": True}
    )
    # Cursor is a previous page's NextCursor. Zendesk expires it one hour after that page was returned.
    cursor: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})


setattr(SearchTicketsInput, "__module__", "dex_connectors.zendesk_support.models")


# Ticket is the connector's view of one Zendesk ticket. Zendesk's API URL,
# satisfaction rating, custom fields, collaborators, and via source details are
# omitted. Zero IDs mean Zendesk reported no value, such as an unassigned ticket.
@dataclass(frozen=True, slots=True)
class Ticket:
    # ID is the Zendesk ticket ID.
    id: int = 0
    # AgentURL opens the ticket in Agent Workspace, such as https://acme.zendesk.com/agent/tickets/35436.
    agent_url: str = _dataclass_field(default="", metadata={"dex_json_name": "agentUrl"})
    # Subject is the ticket subject; Zendesk allows tickets created by API or email without one.
    subject: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})
    # Description is the first comment's text. searchTickets leaves it empty to keep pages small.
    description: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})
    # IsDescriptionTruncated reports that Description was cut at MaxTextBytes.
    is_description_truncated: bool = _dataclass_field(
        default=False, metadata={"dex_json_name": "isDescriptionTruncated", "dex_json_omit_default": True}
    )
    # Status is Zendesk's status value.
    status: str = _dataclass_field(kw_only=True)
    # CustomStatusID is the custom ticket status when the account enables custom statuses.
    custom_status_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "customStatusId", "dex_json_omit_default": True}
    )
    # Priority is Zendesk's priority value, or empty when the ticket has none.
    priority: str | None = _dataclass_field(default=None, metadata={"dex_json_omit_default": True})
    # Type is Zendesk's ticket type, or empty when the ticket has none.
    type: str | None = _dataclass_field(default=None, metadata={"dex_json_omit_default": True})
    # RequesterID is the user who asked for support.
    requester_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "requesterId", "dex_json_omit_default": True}
    )
    # SubmitterID is the user who created the ticket and authored its first comment.
    submitter_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "submitterId", "dex_json_omit_default": True}
    )
    # AssigneeID is the assigned agent, or zero when the ticket is unassigned.
    assignee_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "assigneeId", "dex_json_omit_default": True}
    )
    # GroupID is the assigned group, or zero.
    group_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "groupId", "dex_json_omit_default": True}
    )
    # OrganizationID is the requester's organization for this ticket, or zero.
    organization_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "organizationId", "dex_json_omit_default": True}
    )
    # BrandID is the brand the ticket belongs to, or zero.
    brand_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "brandId", "dex_json_omit_default": True}
    )
    # Tags lists the ticket's tags in Zendesk's order.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={"dex_json_omit_default": True})
    # ExternalID is the application correlation ID stored on the ticket; Zendesk does not require it to be unique.
    external_id: str = _dataclass_field(
        default="", metadata={"dex_json_name": "externalId", "dex_json_omit_default": True}
    )
    # Channel is how the ticket was created, such as api, email, or web.
    channel: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})
    # CreatedAt is when the ticket was created.
    created_at: datetime = _dataclass_field(
        default_factory=lambda: datetime.min.replace(tzinfo=UTC),
        metadata={"dex_json_name": "createdAt", "dex_json_format": "rfc3339"},
    )
    # UpdatedAt is when a ticket event last changed the ticket, at Zendesk's one-second precision.
    updated_at: datetime = _dataclass_field(
        default_factory=lambda: datetime.min.replace(tzinfo=UTC),
        metadata={"dex_json_name": "updatedAt", "dex_json_format": "rfc3339"},
    )
    # DueAt is a task ticket's due time.
    due_at: datetime | None = _dataclass_field(
        default=None,
        metadata={"dex_json_name": "dueAt", "dex_json_omit_default": True, "dex_json_format": "rfc3339"},
    )


setattr(Ticket, "__module__", "dex_connectors.zendesk_support.models")


# SearchTicketsOutput is one page of matching tickets. Zendesk's export search orders
# results by creation time and indexes new or changed tickets within a few minutes,
# so a page can omit a change made moments earlier.
@dataclass(frozen=True, slots=True)
class SearchTicketsOutput:
    # Tickets lists the matching tickets without their Description.
    tickets: tuple[Ticket, ...] = ()
    # NextCursor reads the next page; it is empty on the last page.
    next_cursor: str = _dataclass_field(
        default="", metadata={"dex_json_name": "nextCursor", "dex_json_omit_default": True}
    )

    def __post_init__(self) -> None:
        object.__setattr__(self, "tickets", _replace_nil_slices(self.tickets, 1))


SearchTicketsOutput.__annotations__["tickets"] = tuple[Ticket, ...] | None
SearchTicketsOutput.__dataclass_fields__["tickets"].type = tuple[Ticket, ...] | None
setattr(SearchTicketsOutput, "__module__", "dex_connectors.zendesk_support.models")


# GetTicketInput names one ticket and how many of its latest comments to return.
@dataclass(frozen=True, slots=True)
class GetTicketInput:
    # TicketID is the Zendesk ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={"dex_json_name": "ticketId"})
    # LatestCommentLimit is 1 to MaxLatestCommentLimit; zero uses DefaultLatestCommentLimit.
    latest_comment_limit: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "latestCommentLimit", "dex_json_omit_default": True}
    )


setattr(GetTicketInput, "__module__", "dex_connectors.zendesk_support.models")


# TicketUser is the connector's view of a Zendesk user.
@dataclass(frozen=True, slots=True)
class TicketUser:
    # ID is the Zendesk user ID.
    id: int = 0
    # Name is the user's display name.
    name: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})
    # Email is the user's primary email address, or empty when Zendesk has none.
    email: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})
    # Role is Zendesk's role value, such as end-user, agent, or admin.
    role: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})


setattr(TicketUser, "__module__", "dex_connectors.zendesk_support.models")


# TicketComment is one public reply or internal note.
@dataclass(frozen=True, slots=True)
class TicketComment:
    # ID is the Zendesk comment ID.
    id: int = 0
    # AuthorID is the user who wrote the comment.
    author_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "authorId", "dex_json_omit_default": True}
    )
    # IsPublic is true for a public reply the requester can see and false for an internal note.
    is_public: bool = _dataclass_field(default=False, metadata={"dex_json_name": "isPublic"})
    # Body is the comment as plain text.
    body: str = ""
    # IsBodyTruncated reports that Body was cut at MaxTextBytes.
    is_body_truncated: bool = _dataclass_field(
        default=False, metadata={"dex_json_name": "isBodyTruncated", "dex_json_omit_default": True}
    )
    # CreatedAt is when the comment was added.
    created_at: datetime = _dataclass_field(
        default_factory=lambda: datetime.min.replace(tzinfo=UTC),
        metadata={"dex_json_name": "createdAt", "dex_json_format": "rfc3339"},
    )


setattr(TicketComment, "__module__", "dex_connectors.zendesk_support.models")


# TicketDetails is one ticket with its requester and latest comments.
@dataclass(frozen=True, slots=True)
class TicketDetails:
    # Ticket is the ticket, including its description.
    ticket: Ticket
    # Requester is the ticket's requester, or nil when Zendesk did not return the user.
    requester: TicketUser | None = _dataclass_field(default=None, metadata={"dex_json_omit_default": True})
    # LatestComments lists the newest comments first, public replies and internal notes alike.
    latest_comments: tuple[TicketComment, ...] = _dataclass_field(
        default=(), metadata={"dex_json_name": "latestComments"}
    )
    # HasOlderComments reports that the ticket has comments older than LatestComments.
    has_older_comments: bool = _dataclass_field(
        default=False, metadata={"dex_json_name": "hasOlderComments", "dex_json_omit_default": True}
    )

    def __post_init__(self) -> None:
        object.__setattr__(self, "latest_comments", _replace_nil_slices(self.latest_comments, 1))


TicketDetails.__annotations__["latest_comments"] = tuple[TicketComment, ...] | None
TicketDetails.__dataclass_fields__["latest_comments"].type = tuple[TicketComment, ...] | None
setattr(TicketDetails, "__module__", "dex_connectors.zendesk_support.models")


# TicketCommentInput is one comment to add to a ticket.
@dataclass(frozen=True, slots=True)
class TicketCommentInput:
    # Body is the plain-text comment. It is required and is sent as Zendesk's body, so HTML is not rendered.
    body: str = ""
    # IsInternalNote adds the comment as an internal note that only agents can see; false adds a public reply
    # that Zendesk may email to the requester.
    is_internal_note: bool = _dataclass_field(
        default=False, metadata={"dex_json_name": "isInternalNote", "dex_json_omit_default": True}
    )


setattr(TicketCommentInput, "__module__", "dex_connectors.zendesk_support.models")


# TicketRequesterInput identifies the requester by email. Zendesk uses an existing
# user with that address or, when the account allows it, creates one named Name.
@dataclass(frozen=True, slots=True)
class TicketRequesterInput:
    # Email is one bare address such as jane@example.com.
    email: str = ""
    # Name is used only when Zendesk creates a new user; blank lets Zendesk derive one.
    name: str = _dataclass_field(default="", metadata={"dex_json_omit_default": True})


setattr(TicketRequesterInput, "__module__", "dex_connectors.zendesk_support.models")


# CreateTicketInput is one new ticket and its first comment. Zero IDs and empty
# enums leave the field to Zendesk's defaults and the account's triggers.
@dataclass(frozen=True, slots=True)
class CreateTicketInput:
    # Subject is the ticket subject. It is required.
    subject: str = ""
    # Comment is the first comment, which also becomes the ticket description.
    comment: TicketCommentInput = _dataclass_field(default_factory=TicketCommentInput)
    # Requester is the customer asking for help. Nil makes the connection's agent the requester.
    requester: TicketRequesterInput | None = _dataclass_field(
        default=None, metadata={"dex_json_omit_default": True}
    )
    # Priority is a Zendesk priority, or empty for none.
    priority: str | None = _dataclass_field(default=None, metadata={"dex_json_omit_default": True})
    # Type is a Zendesk ticket type, or empty for none.
    type: str | None = _dataclass_field(default=None, metadata={"dex_json_omit_default": True})
    # Tags are lowercase tags set on the new ticket; account triggers may add more.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={"dex_json_omit_default": True})
    # GroupID assigns the ticket to a group, or zero for none.
    group_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "groupId", "dex_json_omit_default": True}
    )
    # AssigneeID assigns the ticket to an agent, or zero for none.
    assignee_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "assigneeId", "dex_json_omit_default": True}
    )
    # BrandID places the ticket in a brand, or zero for the account's default brand.
    brand_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "brandId", "dex_json_omit_default": True}
    )
    # ExternalID stores an application correlation ID; Zendesk does not enforce uniqueness.
    external_id: str = _dataclass_field(
        default="", metadata={"dex_json_name": "externalId", "dex_json_omit_default": True}
    )


setattr(CreateTicketInput, "__module__", "dex_connectors.zendesk_support.models")


# CreateTicketOutput is the created ticket.
@dataclass(frozen=True, slots=True)
class CreateTicketOutput:
    # Ticket is the ticket Zendesk created or, for a replayed request, created earlier for this Step.
    ticket: Ticket
    # WasIdempotentReplay reports that Zendesk returned the earlier response cached under the
    # Step's Idempotency-Key instead of creating a ticket.
    was_idempotent_replay: bool = _dataclass_field(
        default=False, metadata={"dex_json_name": "wasIdempotentReplay", "dex_json_omit_default": True}
    )


setattr(CreateTicketOutput, "__module__", "dex_connectors.zendesk_support.models")


# UpdateTicketInput is one change to an existing ticket. Empty and zero fields
# are left unchanged; this release cannot clear an assignee or group.
@dataclass(frozen=True, slots=True)
class UpdateTicketInput:
    # TicketID is the Zendesk ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={"dex_json_name": "ticketId"})
    # Status is the new Zendesk status, or empty to keep it.
    status: str | None = _dataclass_field(default=None, metadata={"dex_json_omit_default": True})
    # Priority is the new Zendesk priority, or empty to keep it.
    priority: str | None = _dataclass_field(default=None, metadata={"dex_json_omit_default": True})
    # AssigneeID assigns an agent, or zero to keep the assignee.
    assignee_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "assigneeId", "dex_json_omit_default": True}
    )
    # GroupID assigns a group, or zero to keep the group.
    group_id: int = _dataclass_field(
        default=0, metadata={"dex_json_name": "groupId", "dex_json_omit_default": True}
    )
    # AddTags adds lowercase tags and keeps the ticket's other tags.
    add_tags: tuple[str, ...] = _dataclass_field(
        default=(), metadata={"dex_json_name": "addTags", "dex_json_omit_default": True}
    )
    # RemoveTags removes lowercase tags and keeps the ticket's other tags.
    remove_tags: tuple[str, ...] = _dataclass_field(
        default=(), metadata={"dex_json_name": "removeTags", "dex_json_omit_default": True}
    )
    # Comment adds one public reply or internal note, or nil for none.
    comment: TicketCommentInput | None = _dataclass_field(
        default=None, metadata={"dex_json_omit_default": True}
    )
    # ExpectedUpdatedAt is the ticket's UpdatedAt as the application last read it, an RFC 3339
    # instant such as 2026-01-28T08:45:00Z. A later change selects conflict; blank writes regardless.
    expected_updated_at: str = _dataclass_field(
        default="", metadata={"dex_json_name": "expectedUpdatedAt", "dex_json_omit_default": True}
    )


setattr(UpdateTicketInput, "__module__", "dex_connectors.zendesk_support.models")


# UpdateTicketOutput is the ticket after the change.
@dataclass(frozen=True, slots=True)
class UpdateTicketOutput:
    # Ticket is the ticket Zendesk returned after the change, or as read when nothing needed writing.
    ticket: Ticket
    # WasAlreadyApplied reports that an earlier attempt of this Step already wrote the change, or
    # the ticket already held every requested value, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(
        default=False, metadata={"dex_json_name": "wasAlreadyApplied", "dex_json_omit_default": True}
    )


setattr(UpdateTicketOutput, "__module__", "dex_connectors.zendesk_support.models")
