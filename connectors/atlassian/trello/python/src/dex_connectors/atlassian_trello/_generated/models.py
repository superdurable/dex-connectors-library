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


# ListCardsInput selects one page of cards. Set exactly one of BoardID and ListID. Trello filters by Status;
# the connector applies the due filters to each page, so a page can hold fewer matching cards than
# PageSize, even none, while NextBefore is set.
@dataclass(frozen=True, slots=True)
class ListCardsInput:
    # BoardID lists the cards of this board ID.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId', 'dex_json_omit_default': True})
    # ListID lists the cards of this list ID.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId', 'dex_json_omit_default': True})
    # Status selects open, closed (archived), or all cards; blank lists open cards.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # DueBefore keeps only cards due strictly before this instant.
    due_before: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueBefore', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DueAfter keeps only cards due at or after this instant.
    due_after: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueAfter', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # HasDueDate keeps only cards with a due date when true and only cards without one when false.
    has_due_date: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'hasDueDate', 'dex_json_omit_default': True})
    # IsDueComplete keeps only cards whose due date is marked complete when true, or not complete when false.
    is_due_complete: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isDueComplete', 'dex_json_omit_default': True})
    # PageSize is the number of cards to read from Trello, 1 to 100; zero requests 50.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Before continues a previous list with the same filters from its NextBefore; blank reads the newest
    # cards first.
    before: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListCardsInput, '__module__', 'dex_connectors.atlassian_trello.models')


# Label is one board label on a card.
@dataclass(frozen=True, slots=True)
class Label:
    # ID is the label's Trello ID.
    id: str = ''
    # Name is the label name, which may be empty for a color-only label.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Color is the label color, such as green or sky, or empty for a colorless label.
    color: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Label, '__module__', 'dex_connectors.atlassian_trello.models')


# MemberReference identifies a Trello member by ID, full name, and username.
@dataclass(frozen=True, slots=True)
class MemberReference:
    # ID is the member's Trello ID.
    id: str = ''
    # FullName is the member's display name.
    full_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullName', 'dex_json_omit_default': True})
    # Username is the member's Trello username.
    username: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(MemberReference, '__module__', 'dex_connectors.atlassian_trello.models')


# Card is the connector's view of one Trello card. Trello has no status field: the card's list is its
# workflow state, so moving a card between lists is its status change.
@dataclass(frozen=True, slots=True)
class Card:
    # ID is the card's Trello ID, 24 hexadecimal characters.
    id: str = ''
    # ShortLink is the 8-character code in the card's web address, such as LrrmgFyd.
    short_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'shortLink', 'dex_json_omit_default': True})
    # Name is the card name.
    name: str = ''
    # Description is the plain-text description, returned by getCard and updateCard only, at most 16384
    # characters.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsDescriptionTruncated reports that Description stopped at 16384 characters.
    is_description_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescriptionTruncated', 'dex_json_omit_default': True})
    # BoardID is the ID of the board that holds the card.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # ListID is the ID of the list that holds the card; the list is the card's workflow state.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # ListName is the name of the card's list, returned by getCard only.
    list_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'listName', 'dex_json_omit_default': True})
    # IsClosed reports an archived card.
    is_closed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isClosed'})
    # Due is when the card is due, or nil without a due date.
    due: datetime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # IsDueComplete reports that the card's due date is marked complete.
    is_due_complete: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDueComplete'})
    # Start is the card's start date, or nil.
    start: datetime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # LabelIDs lists up to 50 IDs of the labels on the card.
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})
    # Labels lists up to 50 labels on the card with their names and colors.
    labels: tuple[Label, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # HasMoreLabels reports that the card carries more than 50 labels, so LabelIDs and Labels are incomplete.
    has_more_labels: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreLabels', 'dex_json_omit_default': True})
    # MemberIDs lists up to 50 IDs of the members on the card.
    member_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'memberIds', 'dex_json_omit_default': True})
    # Members lists up to 50 members on the card with their names, returned by getCard only. Email
    # addresses are never read.
    members: tuple[MemberReference, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Position is the card's position in its list; a smaller value is nearer the top.
    position: float = 0.0
    # URL is the card's full Trello web URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ShortURL is the card's short Trello web URL.
    short_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'shortUrl', 'dex_json_omit_default': True})
    # LastActivityAt is when the card last changed.
    last_activity_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastActivityAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Card, '__module__', 'dex_connectors.atlassian_trello.models')


# ListCardsOutput is one page of cards, newest first.
@dataclass(frozen=True, slots=True)
class ListCardsOutput:
    # Cards lists the page's cards that match the due filters, without descriptions.
    cards: tuple[Card, ...] = ()
    # ScannedCardCount is the number of cards Trello returned for the page before the due filters.
    scanned_card_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'scannedCardCount'})
    # NextBefore continues the list with older cards; it is empty when Trello returned a short page.
    next_before: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextBefore', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'cards', _replace_nil_slices(self.cards, 1))


ListCardsOutput.__annotations__['cards'] = tuple[Card, ...] | None
ListCardsOutput.__dataclass_fields__['cards'].type = tuple[Card, ...] | None
setattr(ListCardsOutput, '__module__', 'dex_connectors.atlassian_trello.models')


# GetCardInput identifies one card.
@dataclass(frozen=True, slots=True)
class GetCardInput:
    # CardID is the card's Trello ID, 24 hexadecimal characters.
    card_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'cardId'})


setattr(GetCardInput, '__module__', 'dex_connectors.atlassian_trello.models')


# CreateCardInput describes one card in one list.
@dataclass(frozen=True, slots=True)
class CreateCardInput:
    # ListID is the ID of the list that receives the card; the list also decides the card's board.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # Name is the one-line card name, at most 16384 characters.
    name: str = ''
    # Description is the plain-text description, at most 16384 characters; blank sends none.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Due is when the card is due; nil sends none.
    due: datetime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # LabelIDs adds up to 50 labels of the list's board by ID.
    label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})
    # MemberIDs adds up to 50 board members to the card by ID.
    member_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'memberIds', 'dex_json_omit_default': True})
    # Position is top, bottom, or a positive number; blank keeps Trello's default position.
    position: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(CreateCardInput, '__module__', 'dex_connectors.atlassian_trello.models')


# CreateCardOutput identifies the created card. On providerRejected and uncertain, CardID is empty and the
# requested name and list are echoed so the application can reconcile.
@dataclass(frozen=True, slots=True)
class CreateCardOutput:
    # CardID is the new card's Trello ID.
    card_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'cardId', 'dex_json_omit_default': True})
    # Name echoes the requested name.
    name: str = ''
    # ListID echoes the requested list.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # BoardID is the board that holds the new card.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId', 'dex_json_omit_default': True})
    # ShortLink is the 8-character code in the new card's web address.
    short_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'shortLink', 'dex_json_omit_default': True})
    # URL is the new card's full Trello web URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ShortURL is the new card's short Trello web URL.
    short_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'shortUrl', 'dex_json_omit_default': True})


setattr(CreateCardOutput, '__module__', 'dex_connectors.atlassian_trello.models')


# UpdateCardInput changes one card. Every set field is an absolute value, so sending the same update twice
# leaves the same card. Set at least one change; set at most one of LabelIDs and AddLabelIDs or
# RemoveLabelIDs, and at most one of Due and ShouldClearDue.
@dataclass(frozen=True, slots=True)
class UpdateCardInput:
    # CardID is the card's Trello ID.
    card_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'cardId'})
    # ListID moves the card to this list, which is how a Trello card changes status; blank leaves its list.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId', 'dex_json_omit_default': True})
    # BoardID names the board of ListID when that list is on another board; it requires ListID.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId', 'dex_json_omit_default': True})
    # Position is top, bottom, or a positive number in the card's list; blank leaves Trello's choice.
    position: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsClosed archives the card when true and reopens it when false; nil leaves it.
    is_closed: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isClosed', 'dex_json_omit_default': True})
    # Due sets when the card is due; nil leaves it.
    due: datetime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ShouldClearDue removes the card's due date.
    should_clear_due: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldClearDue', 'dex_json_omit_default': True})
    # IsDueComplete marks the due date complete when true and not complete when false; nil leaves it.
    is_due_complete: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isDueComplete', 'dex_json_omit_default': True})
    # LabelIDs replaces the card's labels with up to 50 label IDs of its board; a pointer to an empty list
    # removes every label, and nil leaves them.
    label_ids: tuple[str, ...] | None = _dataclass_field(default=None, metadata={'dex_json_name': 'labelIds', 'dex_json_omit_default': True})
    # AddLabelIDs adds up to 50 label IDs to the card's current labels, read first.
    add_label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addLabelIds', 'dex_json_omit_default': True})
    # RemoveLabelIDs removes up to 50 label IDs from the card's current labels, read first.
    remove_label_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeLabelIds', 'dex_json_omit_default': True})
    # MemberIDs replaces the card's members with up to 50 member IDs of its board; a pointer to an empty list
    # removes every member, and nil leaves them.
    member_ids: tuple[str, ...] | None = _dataclass_field(default=None, metadata={'dex_json_name': 'memberIds', 'dex_json_omit_default': True})


setattr(UpdateCardInput, '__module__', 'dex_connectors.atlassian_trello.models')


# UpdateCardOutput reports the update. On updated, Card is the card read back after the change.
@dataclass(frozen=True, slots=True)
class UpdateCardOutput:
    # CardID echoes the requested card.
    card_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'cardId'})
    # Card is the card after the update, set only on updated.
    card: Card | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(UpdateCardOutput, '__module__', 'dex_connectors.atlassian_trello.models')


# AddCommentInput describes one comment on one card.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # CardID is the card's Trello ID.
    card_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'cardId'})
    # Text is the plain-text comment, at most 16384 characters; line breaks are kept.
    text: str = ''


setattr(AddCommentInput, '__module__', 'dex_connectors.atlassian_trello.models')


# AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain, CommentID is
# empty and CardID echoes the request.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # CardID echoes the requested card.
    card_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'cardId'})
    # CommentID is the ID of Trello's commentCard action that holds the comment.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # AuthorMemberID is the member that wrote the comment, the token's member.
    author_member_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorMemberId', 'dex_json_omit_default': True})
    # CreatedAt is when Trello stored the comment.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(AddCommentOutput, '__module__', 'dex_connectors.atlassian_trello.models')
