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


# SearchFilesInput selects one page of items. With ParentFolderID set, the
# operation reads that folder's direct children, which is strongly consistent;
# with it blank, it runs Microsoft Graph's drive-wide search, which uses an
# index that can lag recent changes and requires Name.
@dataclass(frozen=True, slots=True)
class SearchFilesInput:
    # DriveID is the drive to search, such as a drivePicker result; blank means
    # the signed-in user's OneDrive and is invalid for app-only connections.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # ParentFolderID limits results to direct children of one folder item ID, or
    # root for the drive root; blank searches the whole drive.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # Name is the item name to match; blank lists every child of ParentFolderID.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # NameMatch selects exact or contains matching; blank means exact.
    name_match: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'nameMatch', 'dex_json_omit_default': True})
    # MimeType restricts results to files of one media type, such as text/plain; blank matches files and folders.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType', 'dex_json_omit_default': True})
    # PageSize is the page size from 1 to 200; zero uses the connection's searchPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken continues a previous search with its NextPageToken; repeat the
    # same filters with it. Blank starts at the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(SearchFilesInput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# FileSummary is the partial item record SearchFiles returns for name-to-ID resolution.
@dataclass(frozen=True, slots=True)
class FileSummary:
    # ID is the stable drive item ID.
    id: str = ''
    # Name is the file or folder name.
    name: str = ''
    # DriveID is the ID of the drive that holds the item.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # ParentFolderID is the parent folder's item ID when Graph reports it.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # IsFolder reports a folder.
    is_folder: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFolder'})
    # IsPackage reports a package, such as a OneNote notebook.
    is_package: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPackage', 'dex_json_omit_default': True})
    # MimeType is the file's media type; empty for folders.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType', 'dex_json_omit_default': True})
    # SizeBytes is the item size reported by Graph.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # LastModifiedDateTime is when the item was last modified.
    last_modified_date_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastModifiedDateTime', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WebURL opens the item in a browser for a user who can access it.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.last_modified_date_time, str):
            object.__setattr__(self, 'last_modified_date_time', parse_go_datetime(self.last_modified_date_time, 'last_modified_date_time'))
        elif self.last_modified_date_time is not None:
            validate_go_datetime(self.last_modified_date_time, 'last_modified_date_time')


setattr(FileSummary, '__module__', 'dex_connectors.microsoft_onedrive.models')


# SearchFilesOutput is one page of matching items in Graph's order.
@dataclass(frozen=True, slots=True)
class SearchFilesOutput:
    # Files lists the page's matches. Filtering happens per page, so a page can
    # be short or empty while NextPageToken is set.
    files: tuple[FileSummary, ...] = ()
    # NextPageToken continues the search; blank means Graph has no further pages.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # IsIndexedSearch reports a drive-wide search, whose index can lag recent
    # changes, so an absent match is not conclusive.
    is_indexed_search: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isIndexedSearch', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'files', _replace_nil_slices(self.files, 1))


SearchFilesOutput.__annotations__['files'] = tuple[FileSummary, ...] | None
SearchFilesOutput.__dataclass_fields__['files'].type = tuple[FileSummary, ...] | None
setattr(SearchFilesOutput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# GetFileInput identifies one drive item.
@dataclass(frozen=True, slots=True)
class GetFileInput:
    # DriveID is the drive that holds the item; blank means the signed-in
    # user's OneDrive and is invalid for app-only connections.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # ItemID is the drive item ID, such as a SearchFiles result ID, or root for the drive root.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})


setattr(GetFileInput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# ItemIdentity is the user or application Graph reports for a create or modify action.
@dataclass(frozen=True, slots=True)
class ItemIdentity:
    # DisplayName is the user's or application's display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # Email is the user's email address when Graph discloses it.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsApplication reports that an application, not a user, performed the action.
    is_application: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isApplication', 'dex_json_omit_default': True})


setattr(ItemIdentity, '__module__', 'dex_connectors.microsoft_onedrive.models')


# DriveItem is the metadata of one OneDrive or SharePoint file or folder. It
# never contains a pre-authenticated download URL.
@dataclass(frozen=True, slots=True)
class DriveItem:
    # ID is the stable drive item ID; renaming or moving the item keeps it.
    id: str = ''
    # Name is the file or folder name, including any extension.
    name: str = ''
    # DriveID is the ID of the drive that holds the item.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # DriveType is personal, business, or documentLibrary.
    drive_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveType', 'dex_json_omit_default': True})
    # SiteID is the SharePoint site ID for an item in a site's document library.
    site_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'siteId', 'dex_json_omit_default': True})
    # ParentFolderID is the item ID of the parent folder; empty for a drive root.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # ParentPath is Graph's percent-encoded path of the parent folder, such as /drives/b!Abc/root:/Reports.
    parent_path: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentPath', 'dex_json_omit_default': True})
    # IsFolder reports a folder, which can hold children.
    is_folder: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFolder'})
    # IsPackage reports a package, such as a OneNote notebook, which is neither a plain file nor a folder.
    is_package: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPackage', 'dex_json_omit_default': True})
    # ChildCount is the number of direct children of a folder.
    child_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'childCount', 'dex_json_omit_default': True})
    # MimeType is the file's media type as Graph reports it; empty for folders.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType', 'dex_json_omit_default': True})
    # SizeBytes is the file size, or the total size of a folder's contents.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # QuickXorHash is Microsoft's base64 content hash of a file, when Graph reports one.
    quick_xor_hash: str = _dataclass_field(default='', metadata={'dex_json_name': 'quickXorHash', 'dex_json_omit_default': True})
    # ETag changes whenever the item's metadata or content changes.
    e_tag: str = _dataclass_field(default='', metadata={'dex_json_name': 'eTag', 'dex_json_omit_default': True})
    # CTag changes whenever the file's content changes; Graph omits it for SharePoint folders.
    c_tag: str = _dataclass_field(default='', metadata={'dex_json_name': 'cTag', 'dex_json_omit_default': True})
    # CreatedDateTime is when the item was created.
    created_date_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdDateTime', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # LastModifiedDateTime is when the item was last modified.
    last_modified_date_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastModifiedDateTime', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WebURL opens the item in a browser for a user who can access it; it is not pre-authenticated.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})
    # CreatedBy identifies the user or application that created the item.
    created_by: ItemIdentity | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdBy', 'dex_json_omit_default': True})
    # LastModifiedBy identifies the user or application that last modified the item.
    last_modified_by: ItemIdentity | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastModifiedBy', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_date_time, str):
            object.__setattr__(self, 'created_date_time', parse_go_datetime(self.created_date_time, 'created_date_time'))
        elif self.created_date_time is not None:
            validate_go_datetime(self.created_date_time, 'created_date_time')
        if isinstance(self.last_modified_date_time, str):
            object.__setattr__(self, 'last_modified_date_time', parse_go_datetime(self.last_modified_date_time, 'last_modified_date_time'))
        elif self.last_modified_date_time is not None:
            validate_go_datetime(self.last_modified_date_time, 'last_modified_date_time')


setattr(DriveItem, '__module__', 'dex_connectors.microsoft_onedrive.models')


# ReadFileTextInput identifies the file whose text is read.
@dataclass(frozen=True, slots=True)
class ReadFileTextInput:
    # DriveID is the drive that holds the file; blank means the signed-in
    # user's OneDrive and is invalid for app-only connections.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # ItemID is the file's drive item ID, such as a SearchFiles result ID.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})


setattr(ReadFileTextInput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# ReadFileTextOutput is the bounded UTF-8 text of one file. The
# unsupportedContent and tooLarge branches return the file identity without Text.
@dataclass(frozen=True, slots=True)
class ReadFileTextOutput:
    # ItemID is the drive item ID that was read.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # Name is the file name.
    name: str = ''
    # MimeType is the media type Graph reports for the file.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType', 'dex_json_omit_default': True})
    # SizeBytes is the file size Graph reports.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # Text is the complete file content, unchanged, including any byte order mark.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ByteCount is the UTF-8 byte length of Text.
    byte_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'byteCount'})


setattr(ReadFileTextOutput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# UploadFileInput describes one file written by parent folder and name. Set at
# most one of TextContent and ByteContent; leaving both empty writes an empty file.
@dataclass(frozen=True, slots=True)
class UploadFileInput:
    # DriveID is the destination drive; blank means the signed-in user's
    # OneDrive and is invalid for app-only connections.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # ParentFolderID is the destination folder's item ID, or root; blank means the drive root.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # Name is the file name, including its extension; it must be a valid OneDrive and SharePoint name.
    name: str = ''
    # ConflictBehavior is fail or replace and is required. Graph's own rename
    # behavior is not offered, because a repeated attempt would create a second file.
    conflict_behavior: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'conflictBehavior'})
    # MimeType is the content's lowercase media type, such as text/plain or text/csv.
    mime_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mimeType'})
    # TextContent is UTF-8 text content.
    text_content: str = _dataclass_field(default='', metadata={'dex_json_name': 'textContent', 'dex_json_omit_default': True})
    # ByteContent is binary content, encoded as base64 in JSON.
    byte_content: str = _dataclass_field(default='', metadata={'dex_json_name': 'byteContent', 'dex_json_omit_default': True})


setattr(UploadFileInput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# UploadFileOutput identifies the written file, or the existing item for alreadyExists.
@dataclass(frozen=True, slots=True)
class UploadFileOutput:
    # Item is the written file's metadata, or the conflicting item's metadata for alreadyExists.
    item: DriveItem = _dataclass_field(default_factory=DriveItem)
    # IsExistingFileIdentical reports that a file with exactly this size and
    # QuickXorHash already existed at the path, such as one an earlier attempt
    # of this Step wrote, so this attempt wrote nothing.
    is_existing_file_identical: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isExistingFileIdentical', 'dex_json_omit_default': True})


setattr(UploadFileOutput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# CreateFolderInput names one folder to ensure in a parent folder.
@dataclass(frozen=True, slots=True)
class CreateFolderInput:
    # DriveID is the drive that holds the parent folder; blank means the
    # signed-in user's OneDrive and is invalid for app-only connections.
    drive_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'driveId', 'dex_json_omit_default': True})
    # ParentFolderID is the parent folder's item ID, or root; blank means the drive root.
    parent_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'parentFolderId', 'dex_json_omit_default': True})
    # Name is the folder name; it must be a valid OneDrive and SharePoint name and cannot end with a period.
    name: str = ''


setattr(CreateFolderInput, '__module__', 'dex_connectors.microsoft_onedrive.models')


# CreateFolderOutput identifies the ensured folder, or the conflicting file for nameConflict.
@dataclass(frozen=True, slots=True)
class CreateFolderOutput:
    # Folder is the folder's metadata, or the conflicting file's metadata for nameConflict.
    folder: DriveItem = _dataclass_field(default_factory=DriveItem)
    # IsExistingFolder reports that a folder with the name already existed, such
    # as one an earlier attempt of this Step created, so nothing was created.
    is_existing_folder: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isExistingFolder', 'dex_json_omit_default': True})


setattr(CreateFolderOutput, '__module__', 'dex_connectors.microsoft_onedrive.models')
