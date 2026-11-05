# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

from .timestamps import parse_go_datetime, validate_go_datetime

# GetDocumentTextInput identifies the document whose first tab is read.
@dataclass(frozen=True, slots=True)
class GetDocumentTextInput:
    # DocumentID is the Google Docs document ID, the part of a
    # docs.google.com/document/d/{documentId}/edit link after /d/.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # Format is markdown or plainText; blank renders Markdown.
    format: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(GetDocumentTextInput, '__module__', 'dex_connectors.google_docs.models')


# GetDocumentTextOutput is the bounded text of a document's first tab, read
# with every pending suggestion rejected. The tooLarge branch returns the
# identity fields without Text.
@dataclass(frozen=True, slots=True)
class GetDocumentTextOutput:
    # DocumentID is the document that was read.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # Title is the document title shown in Google Docs and Drive.
    title: str = ''
    # RevisionID identifies the revision that Text renders. Pass it as
    # RequiredRevisionID to replaceDocumentText or appendText to write only
    # over this revision. Google returns it only to a connection with edit
    # access and guarantees it for 24 hours; it is empty for a viewer.
    revision_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'revisionId', 'dex_json_omit_default': True})
    # Format is the rendering of Text.
    format: str = _dataclass_field(kw_only=True)
    # Text is the rendered body without headers, footers, footnote text,
    # tables of contents, images, or equations.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ByteCount is the UTF-8 byte length of Text.
    byte_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'byteCount'})
    # HasOtherTabs reports that the document has tabs after the first, which
    # this operation does not read.
    has_other_tabs: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasOtherTabs', 'dex_json_omit_default': True})


setattr(GetDocumentTextOutput, '__module__', 'dex_connectors.google_docs.models')


# CreateDocumentInput describes one new Google Doc.
@dataclass(frozen=True, slots=True)
class CreateDocumentInput:
    # Title is the new document's title; it must not be blank or contain line breaks.
    title: str = ''
    # ParentFolderID is the destination Drive folder ID; blank creates the
    # document in the My Drive root of the connection's account.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # InitialText is the document's first content; blank creates an empty document.
    initial_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'initialText', 'dex_json_omit_default': True})
    # InitialTextFormat is plainText or markdown; blank imports plain text.
    # Markdown asks Drive's Markdown import to create headings, lists, and tables.
    initial_text_format: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'initialTextFormat', 'dex_json_omit_default': True})


setattr(CreateDocumentInput, '__module__', 'dex_connectors.google_docs.models')


# CreatedDocument is the Drive metadata of a document createDocument created.
@dataclass(frozen=True, slots=True)
class CreatedDocument:
    # DocumentID is the Google Docs document ID, which is also its Drive file ID.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # Title is the document title.
    title: str = ''
    # Parents lists the parent folder ID; Drive allows at most one parent.
    parents: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # WebViewLink opens the document in a browser for a user who can access it.
    web_view_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'webViewLink', 'dex_json_omit_default': True})
    # CreatedTime is the creation time reported by Drive.
    created_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdTime', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # IsTrashed reports that someone moved the document to the trash after an earlier attempt created it.
    is_trashed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isTrashed', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_time, str):
            object.__setattr__(self, 'created_time', parse_go_datetime(self.created_time, 'created_time'))
        elif self.created_time is not None:
            validate_go_datetime(self.created_time, 'created_time')


setattr(CreatedDocument, '__module__', 'dex_connectors.google_docs.models')


# CreateDocumentOutput identifies the created document.
@dataclass(frozen=True, slots=True)
class CreateDocumentOutput:
    # Document is the created document's Drive metadata.
    document: CreatedDocument = _dataclass_field(default_factory=CreatedDocument)
    # IsFromEarlierAttempt reports that an earlier attempt of the same Step
    # execution had already created Document, so this attempt created nothing.
    is_from_earlier_attempt: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFromEarlierAttempt', 'dex_json_omit_default': True})


setattr(CreateDocumentOutput, '__module__', 'dex_connectors.google_docs.models')


# PlaceholderReplacement is one named text placeholder and its replacement.
@dataclass(frozen=True, slots=True)
class PlaceholderReplacement:
    # Placeholder is the exact, case-sensitive text to replace, such as
    # {{effectiveDate}}. It is 1 to 256 bytes on one line.
    placeholder: str = ''
    # Text replaces every occurrence; blank removes the placeholder. It cannot
    # contain any requested placeholder.
    text: str = ''


setattr(PlaceholderReplacement, '__module__', 'dex_connectors.google_docs.models')


# ReplaceDocumentTextInput describes one guarded replacement in a document's first tab.
@dataclass(frozen=True, slots=True)
class ReplaceDocumentTextInput:
    # DocumentID is the Google Docs document ID.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # RequiredRevisionID is the revision the replacement applies to, normally
    # the RevisionID of a getDocumentText Step. Google applies the change only
    # while the document is still at this revision.
    required_revision_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'requiredRevisionId'})
    # Target is wholeBody or placeholders.
    target: str = _dataclass_field(kw_only=True)
    # Text is the new body text for wholeBody; blank empties the body. It is
    # inserted literally, so Markdown markup is not interpreted.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Placeholders lists the placeholders to replace for placeholders, such as
    # {{effectiveDate}}; every one must occur in the body at RequiredRevisionID.
    placeholders: tuple[PlaceholderReplacement, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(ReplaceDocumentTextInput, '__module__', 'dex_connectors.google_docs.models')


# PlaceholderReplacementResult is Google's count of replaced occurrences of one placeholder.
@dataclass(frozen=True, slots=True)
class PlaceholderReplacementResult:
    # Placeholder is the replaced placeholder.
    placeholder: str = ''
    # OccurrencesChanged is the number of occurrences Google replaced.
    occurrences_changed: int = _dataclass_field(default=0, metadata={'dex_json_name': 'occurrencesChanged'})


setattr(PlaceholderReplacementResult, '__module__', 'dex_connectors.google_docs.models')


# ReplaceDocumentTextOutput reports the document after the replacement.
@dataclass(frozen=True, slots=True)
class ReplaceDocumentTextOutput:
    # DocumentID is the document that was changed.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # RevisionID is the document's revision after the replacement; on
    # revisionChanged it is the current revision that blocked the write. It is
    # empty only when Google omits it from a successful update response.
    revision_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'revisionId', 'dex_json_omit_default': True})
    # Replacements reports Google's occurrence count per placeholder when this attempt wrote.
    replacements: tuple[PlaceholderReplacementResult, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # MissingPlaceholders lists the placeholders absent on placeholderNotFound.
    missing_placeholders: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'missingPlaceholders', 'dex_json_omit_default': True})
    # WasAlreadyApplied reports that this attempt wrote nothing because the
    # document already held the requested text, normally written by an earlier
    # attempt of the same Step execution whose response was lost or slow.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(ReplaceDocumentTextOutput, '__module__', 'dex_connectors.google_docs.models')


# AppendTextInput describes text appended to the end of a document's first tab.
@dataclass(frozen=True, slots=True)
class AppendTextInput:
    # DocumentID is the Google Docs document ID.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # RequiredRevisionID is the revision the text is appended to, normally the
    # RevisionID of a getDocumentText or replaceDocumentText Step. Google
    # appends only while the document is still at this revision.
    required_revision_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'requiredRevisionId'})
    # Text is appended as one or more plain normal-style paragraphs, one per
    # line, after the body's last paragraph. It must not be blank.
    text: str = ''


setattr(AppendTextInput, '__module__', 'dex_connectors.google_docs.models')


# AppendTextOutput reports the document after the append.
@dataclass(frozen=True, slots=True)
class AppendTextOutput:
    # DocumentID is the document that was changed.
    document_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'documentId'})
    # RevisionID is the document's revision after the append; on
    # revisionChanged it is the current revision that blocked the write. It is
    # empty only when Google omits it from a successful update response.
    revision_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'revisionId', 'dex_json_omit_default': True})
    # WasAlreadyApplied reports that this attempt wrote nothing because the
    # changed document already ends with the text, normally appended by an
    # earlier attempt of the same Step execution whose response was lost or slow.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(AppendTextOutput, '__module__', 'dex_connectors.google_docs.models')
