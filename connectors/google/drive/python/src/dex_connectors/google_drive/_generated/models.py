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


# SearchFilesInput filters one page of non-trashed Drive files. Every set
# filter must match; an input without filters lists recent files.
@dataclass(frozen=True, slots=True)
class SearchFilesInput:
    # Name is the file name to match; blank matches any name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # NameMatch selects exact or prefix-term matching; blank means exact.
    name_match: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'nameMatch', 'dex_json_omit_default': True})
    # MimeType restricts results to one Drive MIME type, such as
    # application/vnd.google-apps.spreadsheet; blank matches any type.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType', 'dex_json_omit_default': True})
    # ParentFolderID restricts results to direct children of one folder ID, or
    # root for the My Drive root; blank searches every visible folder.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # PageSize is the page size from 1 to 100; zero uses the connection's searchPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken continues a previous search with its NextPageToken; blank starts at the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(SearchFilesInput, '__module__', 'dex_connectors.google_drive.models')


# FileSummary is the partial file record that SearchFiles returns for
# name-to-ID resolution.
@dataclass(frozen=True, slots=True)
class FileSummary:
    # ID is the stable Drive file ID. A Google Sheets file ID is also its spreadsheet ID.
    id: str = ''
    # Name is the file title shown in Google Drive.
    name: str = ''
    # MimeType is the Drive MIME type, such as application/vnd.google-apps.spreadsheet.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType'})
    # Parents lists the parent folder ID; Drive allows at most one parent.
    parents: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ModifiedTime is the last modification time reported by Drive.
    modified_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'modifiedTime', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WebViewLink opens the file in a browser for a user who can access it.
    web_view_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'webViewLink', 'dex_json_omit_default': True})


setattr(FileSummary, '__module__', 'dex_connectors.google_drive.models')


# SearchFilesOutput is one page of matching files, newest modification first.
@dataclass(frozen=True, slots=True)
class SearchFilesOutput:
    # Files lists the page's matches. Drive may return a short or empty page
    # while NextPageToken is set.
    files: tuple[FileSummary, ...] = ()
    # NextPageToken continues the search; blank means Drive has no further pages.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # IsIncompleteSearch reports that Drive did not search every document, so
    # an absent match is not conclusive.
    is_incomplete_search: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isIncompleteSearch', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'files', _replace_nil_slices(self.files, 1))


SearchFilesOutput.__annotations__['files'] = tuple[FileSummary, ...] | None
SearchFilesOutput.__dataclass_fields__['files'].type = tuple[FileSummary, ...] | None
setattr(SearchFilesOutput, '__module__', 'dex_connectors.google_drive.models')


# GetFileInput identifies one Drive file.
@dataclass(frozen=True, slots=True)
class GetFileInput:
    # FileID is the Drive file ID, such as a SearchFiles result ID.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})


setattr(GetFileInput, '__module__', 'dex_connectors.google_drive.models')


# FileUser is a Drive user identity attached to file metadata.
@dataclass(frozen=True, slots=True)
class FileUser:
    # DisplayName is the user's display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # EmailAddress is the user's email address when Drive discloses it.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress', 'dex_json_omit_default': True})


setattr(FileUser, '__module__', 'dex_connectors.google_drive.models')


# ShortcutTarget identifies the file a Drive shortcut points to.
@dataclass(frozen=True, slots=True)
class ShortcutTarget:
    # TargetFileID is the Drive ID of the shortcut's target file.
    target_file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'targetFileId'})
    # TargetMimeType is the Drive MIME type of the target file.
    target_mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'targetMimeType', 'dex_json_omit_default': True})


setattr(ShortcutTarget, '__module__', 'dex_connectors.google_drive.models')


# File is the full metadata record that GetFile and UploadFile return.
@dataclass(frozen=True, slots=True)
class File:
    # ID is the stable Drive file ID.
    id: str = ''
    # Name is the file title shown in Google Drive.
    name: str = ''
    # MimeType is the Drive MIME type.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType'})
    # Description is the optional file description.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Parents lists the parent folder ID; Drive allows at most one parent.
    parents: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # DriveID identifies the shared drive that owns the file; empty means My Drive.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # CreatedTime is the creation time reported by Drive.
    created_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdTime', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ModifiedTime is the last modification time reported by Drive.
    modified_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'modifiedTime', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SizeBytes is the stored content size; Google Workspace files and folders report zero.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes', 'dex_json_omit_default': True})
    # MD5Checksum is the content checksum Drive reports for uploaded binary content.
    md5_checksum: str = _dataclass_field(default='', metadata={'dex_json_name': 'md5Checksum', 'dex_json_omit_default': True})
    # Version is Drive's monotonically increasing file version.
    version: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # WebViewLink opens the file in a browser for a user who can access it.
    web_view_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'webViewLink', 'dex_json_omit_default': True})
    # IsTrashed reports whether the file is in the trash.
    is_trashed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isTrashed'})
    # Owners lists the owners Drive reports; shared-drive files have none.
    owners: tuple[FileUser, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # LastModifyingUser is the user who last modified the file, when Drive reports one.
    last_modifying_user: FileUser | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastModifyingUser', 'dex_json_omit_default': True})
    # Shortcut identifies the target of a Drive shortcut file; nil for other files.
    shortcut: ShortcutTarget | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(File, '__module__', 'dex_connectors.google_drive.models')


# ReadFileTextInput identifies the Drive file whose text is read.
@dataclass(frozen=True, slots=True)
class ReadFileTextInput:
    # FileID is the Drive file ID, such as a SearchFiles result ID.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})


setattr(ReadFileTextInput, '__module__', 'dex_connectors.google_drive.models')


# ReadFileTextOutput is the bounded UTF-8 text of one file. The unsupportedContent
# and tooLarge branches return the file identity without Text.
@dataclass(frozen=True, slots=True)
class ReadFileTextOutput:
    # FileID is the Drive file ID that was read.
    file_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileId'})
    # Name is the file title shown in Google Drive.
    name: str = ''
    # MimeType is the file's Drive MIME type.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType'})
    # TextMimeType is text/plain for exported Docs and Slides, text/csv for the
    # first sheet of an exported Sheets file, or the downloaded file's own type.
    text_mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'textMimeType', 'dex_json_omit_default': True})
    # Text is the complete file text without a leading UTF-8 byte order mark.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ByteCount is the UTF-8 byte length of Text.
    byte_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'byteCount'})


setattr(ReadFileTextOutput, '__module__', 'dex_connectors.google_drive.models')


# UploadFileInput describes one new Drive file. Set at most one of TextContent
# and ByteContent; leaving both empty creates an empty file.
@dataclass(frozen=True, slots=True)
class UploadFileInput:
    # Name is the new file's title; it must not be blank.
    name: str = ''
    # ParentFolderID is the destination folder ID; blank creates the file in the My Drive root.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # MimeType is the content's lowercase MIME type, such as text/plain or
    # text/csv. Google Workspace types are rejected, so Drive never converts the upload.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType'})
    # TextContent is UTF-8 text content.
    text_content: str = _dataclass_field(default='', metadata={'dex_json_name': 'textContent', 'dex_json_omit_default': True})
    # ByteContent is binary content, encoded as base64 in JSON.
    byte_content: str = _dataclass_field(default='', metadata={'dex_json_name': 'byteContent', 'dex_json_omit_default': True})


setattr(UploadFileInput, '__module__', 'dex_connectors.google_drive.models')


# UploadFileOutput identifies the created file.
@dataclass(frozen=True, slots=True)
class UploadFileOutput:
    # File is the created file's metadata.
    file: File = _dataclass_field(default_factory=File)
    # IsFromEarlierAttempt reports that an earlier attempt of the same Step
    # execution had already created File, so this attempt uploaded nothing.
    is_from_earlier_attempt: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFromEarlierAttempt', 'dex_json_omit_default': True})


setattr(UploadFileOutput, '__module__', 'dex_connectors.google_drive.models')
