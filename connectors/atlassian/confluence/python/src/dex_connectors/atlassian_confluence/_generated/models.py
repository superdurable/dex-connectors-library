# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# PageSearchFilter is a typed page search that the connector turns into escaped CQL, so no caller text
# can change the query's structure. The query always starts with type = page, and every set field adds
# one clause joined with AND.
@dataclass(frozen=True, slots=True)
class PageSearchFilter:
    # SpaceKeys limits the search to these space keys, such as OPS or ~5b10ac8d82e05b22cc7d4ef5.
    space_keys: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'spaceKeys', 'dex_json_omit_default': True})
    # TitlePhrase matches pages whose title contains this phrase as Confluence's text search tokenizes
    # it. Text search ignores case and punctuation, so compare the returned titles for an exact match.
    title_phrase: str = _dataclass_field(default='', metadata={'dex_json_name': 'titlePhrase', 'dex_json_omit_default': True})
    # Labels matches pages that carry at least one of these labels, such as policy or in-force.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ModifiedSince keeps only pages last modified at or after this instant. CQL compares dates in the
    # authorizing user's Confluence time zone, so the connector widens the CQL bound by half a day and
    # removes earlier pages from each result page itself.
    modified_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'modifiedSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # AdditionalCQL is one caller-built clause ANDed inside parentheses, such as
    # ancestor = 123456 OR parent = 123456. It must balance its parentheses and quotes and cannot
    # contain ORDER BY. Quote every value that came from outside the application with QuoteCQLString.
    additional_cql: str = _dataclass_field(default='', metadata={'dex_json_name': 'additionalCql', 'dex_json_omit_default': True})
    # Order selects the result order; blank uses PageSearchOrderLastModifiedDescending.
    order: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.modified_since, str):
            object.__setattr__(self, 'modified_since', parse_go_datetime(self.modified_since, 'modified_since'))
        elif self.modified_since is not None:
            validate_go_datetime(self.modified_since, 'modified_since')


setattr(PageSearchFilter, '__module__', 'dex_connectors.atlassian_confluence.models')


# SearchPagesInput selects one page of search results.
@dataclass(frozen=True, slots=True)
class SearchPagesInput:
    # Filter is the typed search the connector escapes into CQL.
    filter: PageSearchFilter = _dataclass_field(default_factory=PageSearchFilter)
    # PageSize is the number of pages to request, 1 to 100; zero requests 25. Confluence may return fewer.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Cursor continues a previous search with the same filter; blank reads the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SearchPagesInput, '__module__', 'dex_connectors.atlassian_confluence.models')


# PageSummary identifies one page found by search. Read its body with getPage.
@dataclass(frozen=True, slots=True)
class PageSummary:
    # ID is the numeric page ID.
    id: str = ''
    # Title is the page title.
    title: str = ''
    # Status is Confluence's page status, such as current.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SpaceID is the numeric ID of the space that holds the page.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId', 'dex_json_omit_default': True})
    # SpaceKey is the key of the space that holds the page, such as OPS.
    space_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceKey', 'dex_json_omit_default': True})
    # VersionNumber is the page's current version number.
    version_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'versionNumber', 'dex_json_omit_default': True})
    # LastModifiedAt is when the current version was saved, or zero when Confluence omitted it.
    last_modified_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastModifiedAt', 'dex_json_format': 'rfc3339'})
    # WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.last_modified_at, str):
            object.__setattr__(self, 'last_modified_at', parse_go_datetime(self.last_modified_at, 'last_modified_at'))
        elif self.last_modified_at is not None:
            validate_go_datetime(self.last_modified_at, 'last_modified_at')


setattr(PageSummary, '__module__', 'dex_connectors.atlassian_confluence.models')


# SearchPagesOutput is one page of matching pages.
@dataclass(frozen=True, slots=True)
class SearchPagesOutput:
    # CQL is the exact query sent to Confluence.
    cql: str = ''
    # Pages lists the matching pages in the requested order, without their bodies.
    pages: tuple[PageSummary, ...] = ()
    # NextCursor continues the search; it is empty on the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'pages', _replace_nil_slices(self.pages, 1))


SearchPagesOutput.__annotations__['pages'] = tuple[PageSummary, ...] | None
SearchPagesOutput.__dataclass_fields__['pages'].type = tuple[PageSummary, ...] | None
setattr(SearchPagesOutput, '__module__', 'dex_connectors.atlassian_confluence.models')


# GetPageInput identifies one page and how to return its body.
@dataclass(frozen=True, slots=True)
class GetPageInput:
    # PageID is the numeric page ID, such as 123456.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # BodyRepresentation selects the Confluence body to convert; blank reads storage format.
    body_representation: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'bodyRepresentation', 'dex_json_omit_default': True})
    # BodyFormat selects the returned text; blank returns Markdown.
    body_format: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'bodyFormat', 'dex_json_omit_default': True})
    # MaxBodyCharacters cuts Body after this many characters, 1 to 131072; zero keeps 32768.
    max_body_characters: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxBodyCharacters', 'dex_json_omit_default': True})


setattr(GetPageInput, '__module__', 'dex_connectors.atlassian_confluence.models')


# PageVersion is one published version of a page.
@dataclass(frozen=True, slots=True)
class PageVersion:
    # Number is the version number; an update must name Number plus one.
    number: int = 0
    # CreatedAt is when the version was saved.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # AuthorAccountID is the Atlassian account that saved the version.
    author_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorAccountId', 'dex_json_omit_default': True})
    # IsMinorEdit reports a version saved without notifications.
    is_minor_edit: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isMinorEdit', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(PageVersion, '__module__', 'dex_connectors.atlassian_confluence.models')


# Page is the connector's view of one Confluence page.
@dataclass(frozen=True, slots=True)
class Page:
    # ID is the numeric page ID, stable across title changes and moves.
    id: str = ''
    # Title is the page title, unique within its space.
    title: str = ''
    # Status is Confluence's page status, such as current or archived.
    status: str = ''
    # SpaceID is the numeric ID of the space that holds the page.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId'})
    # ParentPageID is the parent page's ID, or empty for a page at the space root.
    parent_page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentPageId', 'dex_json_omit_default': True})
    # AuthorAccountID is the account that created the page.
    author_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorAccountId', 'dex_json_omit_default': True})
    # CreatedAt is when the page was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # Version is the current published version.
    version: PageVersion = _dataclass_field(default_factory=PageVersion)
    # Body is the page body converted to BodyFormat, cut at the requested character limit.
    body: str = ''
    # BodyFormat is the format of Body.
    body_format: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'bodyFormat'})
    # IsBodyTruncated reports that Body stopped at the character limit.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(Page, '__module__', 'dex_connectors.atlassian_confluence.models')


# CreatePageInput describes one published page. Set exactly one of SpaceID and SpaceKey.
@dataclass(frozen=True, slots=True)
class CreatePageInput:
    # SpaceID is the numeric space ID, as the space picker stores it.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId', 'dex_json_omit_default': True})
    # SpaceKey is the space key, such as OPS; the connector looks up its ID before creating.
    space_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceKey', 'dex_json_omit_default': True})
    # ParentPageID places the page below this page in the same space; blank uses the space homepage.
    parent_page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentPageId', 'dex_json_omit_default': True})
    # Title is the one-line page title, at most 255 characters. Confluence allows one page per title
    # in a space.
    title: str = ''
    # Body is the page content of at most 262144 characters in BodyFormat.
    body: str = ''
    # BodyFormat is the format of Body; blank reads Markdown.
    body_format: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'bodyFormat', 'dex_json_omit_default': True})


setattr(CreatePageInput, '__module__', 'dex_connectors.atlassian_confluence.models')


# CreatePageOutput identifies the page. On created it is the new page. On titleConflict it is the
# existing page with that title, so an application can update it with VersionNumber plus one. On
# notFound and providerRejected only the requested title and space are echoed.
@dataclass(frozen=True, slots=True)
class CreatePageOutput:
    # PageID is the page's numeric ID.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId', 'dex_json_omit_default': True})
    # Title echoes the requested title.
    title: str = ''
    # SpaceID is the numeric ID of the space, resolved from SpaceKey when needed.
    space_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceId', 'dex_json_omit_default': True})
    # SpaceKey echoes the requested space key.
    space_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'spaceKey', 'dex_json_omit_default': True})
    # ParentPageID is the page's parent page ID.
    parent_page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentPageId', 'dex_json_omit_default': True})
    # VersionNumber is the page's current version number: 1 for a new page.
    version_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'versionNumber', 'dex_json_omit_default': True})
    # WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})
    # IsConfirmedByTitleLookup reports that Confluence did not confirm the create, and the connector
    # found the page this Step execution sent by its title and content.
    is_confirmed_by_title_lookup: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isConfirmedByTitleLookup', 'dex_json_omit_default': True})


setattr(CreatePageOutput, '__module__', 'dex_connectors.atlassian_confluence.models')


# UpdatePageInput replaces one page's title and body as the next version.
@dataclass(frozen=True, slots=True)
class UpdatePageInput:
    # PageID is the numeric page ID, such as 123456.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # Title is the page title after the update, one line of at most 255 characters; repeat the current
    # title to keep it.
    title: str = ''
    # Body is the complete new page content of at most 262144 characters in BodyFormat.
    body: str = ''
    # BodyFormat is the format of Body; blank reads Markdown.
    body_format: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'bodyFormat', 'dex_json_omit_default': True})
    # NextVersionNumber is the version this update creates: the version number getPage returned plus one.
    # Confluence refuses it when another edit saved that version first.
    next_version_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextVersionNumber'})
    # VersionMessage is an optional one-line change summary of at most 200 characters, shown in page
    # history. The connector appends a marker such as [dex:3f9a2c1b7e04d5a6] to recognize its own update.
    version_message: str = _dataclass_field(default='', metadata={'dex_json_name': 'versionMessage', 'dex_json_omit_default': True})


setattr(UpdatePageInput, '__module__', 'dex_connectors.atlassian_confluence.models')


# UpdatePageOutput describes the page after the update. On versionConflict VersionNumber is the page's
# current version, so an application can read the page again and decide whether to update it.
@dataclass(frozen=True, slots=True)
class UpdatePageOutput:
    # PageID echoes the requested page.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # Title echoes the requested title.
    title: str = ''
    # VersionNumber is the page's version: NextVersionNumber on updated, the current version otherwise.
    version_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'versionNumber', 'dex_json_omit_default': True})
    # WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})
    # IsConfirmedByReadBack reports that Confluence did not confirm the update, and the connector found
    # this Step execution's version by reading the page back.
    is_confirmed_by_read_back: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isConfirmedByReadBack', 'dex_json_omit_default': True})


setattr(UpdatePageOutput, '__module__', 'dex_connectors.atlassian_confluence.models')


# AddCommentInput describes one footer comment on one page.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # PageID is the numeric page ID, such as 123456.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # Body is the comment of at most 262144 characters in BodyFormat.
    body: str = ''
    # BodyFormat is the format of Body; blank reads Markdown.
    body_format: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'bodyFormat', 'dex_json_omit_default': True})


setattr(AddCommentInput, '__module__', 'dex_connectors.atlassian_confluence.models')


# AddCommentOutput identifies the comment. On notFound, providerRejected, and uncertain, CommentID is
# empty and PageID echoes the request.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # PageID echoes the requested page.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # CommentID is the new footer comment's numeric ID.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # CreatedAt is when Confluence stored the comment.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # IsConfirmedByReadBack reports that an earlier attempt sent the comment and the connector found it
    # among the page's newest footer comments instead of sending it again.
    is_confirmed_by_read_back: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isConfirmedByReadBack', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(AddCommentOutput, '__module__', 'dex_connectors.atlassian_confluence.models')
