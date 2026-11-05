# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

from .timestamps import parse_go_datetime, validate_go_datetime

# ListObjectsInput selects one page of a bucket listing. Keys are listed in UTF-8 binary order.
@dataclass(frozen=True, slots=True)
class ListObjectsInput:
    # Bucket is the bucket name; blank uses the connection's defaultBucket.
    bucket: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Prefix limits the page to keys that begin with it, such as reports/2026/; blank lists every key.
    prefix: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Delimiter groups keys that contain it after Prefix into CommonPrefixes, usually "/" to list one
    # folder level; blank lists every key under Prefix.
    delimiter: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # StartAfter lists only keys after this key; blank starts at the first key.
    start_after: str = _dataclass_field(default='', metadata={'dex_json_name': 'startAfter', 'dex_json_omit_default': True})
    # ContinuationToken is the NextContinuationToken of the previous page; blank reads the first page.
    continuation_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'continuationToken', 'dex_json_omit_default': True})
    # MaxKeys bounds the objects plus common prefixes on the page, from 1 to 1000; zero uses the
    # connection's listPageSize.
    max_keys: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxKeys', 'dex_json_omit_default': True})


setattr(ListObjectsInput, '__module__', 'dex_connectors.amazon_s3.models')


# ObjectSummary is one object on a listObjects page.
@dataclass(frozen=True, slots=True)
class ObjectSummary:
    # Key is the object key.
    key: str = ''
    # SizeBytes is the object size.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # ETag is the entity tag exactly as S3 reports it, including its double quotes.
    e_tag: str = _dataclass_field(default='', metadata={'dex_json_name': 'eTag', 'dex_json_omit_default': True})
    # LastModified is when the object was last written.
    last_modified: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastModified', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # StorageClass is the S3 storage class, such as STANDARD.
    storage_class: str = _dataclass_field(default='', metadata={'dex_json_name': 'storageClass', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.last_modified, str):
            object.__setattr__(self, 'last_modified', parse_go_datetime(self.last_modified, 'last_modified'))
        elif self.last_modified is not None:
            validate_go_datetime(self.last_modified, 'last_modified')


setattr(ObjectSummary, '__module__', 'dex_connectors.amazon_s3.models')


# ListObjectsOutput is one page of a bucket listing.
@dataclass(frozen=True, slots=True)
class ListObjectsOutput:
    # Bucket is the listed bucket.
    bucket: str = ''
    # Prefix is the requested key prefix.
    prefix: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Delimiter is the requested delimiter.
    delimiter: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Objects lists the page's objects in key order.
    objects: tuple[ObjectSummary, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CommonPrefixes lists the folder-like key prefixes the delimiter rolled up, each ending in Delimiter.
    common_prefixes: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'commonPrefixes', 'dex_json_omit_default': True})
    # IsTruncated reports that more keys remain after this page.
    is_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isTruncated'})
    # NextContinuationToken continues the listing when IsTruncated is true; pass it as ContinuationToken.
    next_continuation_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextContinuationToken', 'dex_json_omit_default': True})


setattr(ListObjectsOutput, '__module__', 'dex_connectors.amazon_s3.models')


# HeadObjectInput identifies the object whose metadata is read.
@dataclass(frozen=True, slots=True)
class HeadObjectInput:
    # Bucket is the bucket name; blank uses the connection's defaultBucket.
    bucket: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Key is the object key, such as reports/2026-09.md.
    key: str = ''


setattr(HeadObjectInput, '__module__', 'dex_connectors.amazon_s3.models')


# ObjectMetadata is the metadata S3 reports for one object, without its content.
@dataclass(frozen=True, slots=True)
class ObjectMetadata:
    # Bucket is the object's bucket.
    bucket: str = ''
    # Key is the object key.
    key: str = ''
    # SizeBytes is the object size.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # ETag is the entity tag exactly as S3 reports it, including its double quotes. For a single-request
    # upload without SSE-KMS or SSE-C encryption it is the hex MD5 of the content.
    e_tag: str = _dataclass_field(default='', metadata={'dex_json_name': 'eTag', 'dex_json_omit_default': True})
    # ContentType is the stored Content-Type, such as text/markdown; charset=utf-8.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # ContentEncoding is the stored Content-Encoding, such as gzip; empty means none.
    content_encoding: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentEncoding', 'dex_json_omit_default': True})
    # ContentLanguage is the stored Content-Language.
    content_language: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentLanguage', 'dex_json_omit_default': True})
    # ContentDisposition is the stored Content-Disposition.
    content_disposition: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentDisposition', 'dex_json_omit_default': True})
    # CacheControl is the stored Cache-Control.
    cache_control: str = _dataclass_field(default='', metadata={'dex_json_name': 'cacheControl', 'dex_json_omit_default': True})
    # LastModified is when the object was last written.
    last_modified: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastModified', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # VersionID is the object version in a versioning-enabled bucket; empty otherwise.
    version_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'versionId', 'dex_json_omit_default': True})
    # StorageClass is the storage class; Amazon S3 reports none for STANDARD.
    storage_class: str = _dataclass_field(default='', metadata={'dex_json_name': 'storageClass', 'dex_json_omit_default': True})
    # ServerSideEncryption is the server-side encryption algorithm, such as AES256 or aws:kms.
    server_side_encryption: str = _dataclass_field(default='', metadata={'dex_json_name': 'serverSideEncryption', 'dex_json_omit_default': True})
    # Metadata is the user-defined metadata, keyed by lowercase name without the x-amz-meta- prefix. S3
    # returns a non-ASCII value RFC 2047-encoded, and the value is kept as S3 returns it.
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # MissingMetadataCount is the number of metadata entries S3 could not return as HTTP headers.
    missing_metadata_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'missingMetadataCount', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.last_modified, str):
            object.__setattr__(self, 'last_modified', parse_go_datetime(self.last_modified, 'last_modified'))
        elif self.last_modified is not None:
            validate_go_datetime(self.last_modified, 'last_modified')


setattr(ObjectMetadata, '__module__', 'dex_connectors.amazon_s3.models')


# GetObjectTextInput identifies the object whose text is read.
@dataclass(frozen=True, slots=True)
class GetObjectTextInput:
    # Bucket is the bucket name; blank uses the connection's defaultBucket.
    bucket: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Key is the object key, such as reports/2026-09.md.
    key: str = ''


setattr(GetObjectTextInput, '__module__', 'dex_connectors.amazon_s3.models')


# ObjectText is the bounded text of one object. The unsupportedContent and tooLarge branches return the
# object's identity, content type, and size without Text.
@dataclass(frozen=True, slots=True)
class ObjectText:
    # Bucket is the object's bucket.
    bucket: str = ''
    # Key is the object key.
    key: str = ''
    # ContentType is the stored Content-Type.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # ETag is the entity tag exactly as S3 reports it, including its double quotes.
    e_tag: str = _dataclass_field(default='', metadata={'dex_json_name': 'eTag', 'dex_json_omit_default': True})
    # VersionID is the object version in a versioning-enabled bucket; empty otherwise.
    version_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'versionId', 'dex_json_omit_default': True})
    # LastModified is when the object was last written.
    last_modified: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastModified', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SizeBytes is the object size, which equals the UTF-8 byte length of Text when Text is returned.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # Text is the complete object content, unchanged, including any byte order mark.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.last_modified, str):
            object.__setattr__(self, 'last_modified', parse_go_datetime(self.last_modified, 'last_modified'))
        elif self.last_modified is not None:
            validate_go_datetime(self.last_modified, 'last_modified')


setattr(ObjectText, '__module__', 'dex_connectors.amazon_s3.models')


# PutObjectInput describes one object to store. Set at most one of TextContent and ByteContent; leaving
# both empty stores an empty object.
@dataclass(frozen=True, slots=True)
class PutObjectInput:
    # Bucket is the bucket name; blank uses the connection's defaultBucket.
    bucket: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Key is the object key, such as reports/2026-09.md.
    key: str = ''
    # ContentType is the media type S3 stores and returns, such as text/markdown; charset=utf-8.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})
    # TextContent is UTF-8 text content.
    text_content: str = _dataclass_field(default='', metadata={'dex_json_name': 'textContent', 'dex_json_omit_default': True})
    # ByteContent is binary content, encoded as base64 in JSON.
    byte_content: str = _dataclass_field(default='', metadata={'dex_json_name': 'byteContent', 'dex_json_omit_default': True})
    # Metadata is user-defined metadata, keyed by lowercase name without the x-amz-meta- prefix, such as
    # report-id. Values are printable ASCII. The dex-idempotency-key name is reserved.
    metadata: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # IsCreateOnly stores the object only when no current object exists at the key, by sending
    # If-None-Match: *; an object that this Step execution did not write selects alreadyExists.
    is_create_only: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCreateOnly', 'dex_json_omit_default': True})


setattr(PutObjectInput, '__module__', 'dex_connectors.amazon_s3.models')


# StoredObject identifies the object at the key after putObject. On alreadyExists it describes the
# existing object, which this Step execution did not write.
@dataclass(frozen=True, slots=True)
class StoredObject:
    # Bucket is the object's bucket.
    bucket: str = ''
    # Key is the object key.
    key: str = ''
    # ETag is the entity tag exactly as S3 reports it, including its double quotes.
    e_tag: str = _dataclass_field(default='', metadata={'dex_json_name': 'eTag', 'dex_json_omit_default': True})
    # VersionID is the version S3 created in a versioning-enabled bucket; empty otherwise.
    version_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'versionId', 'dex_json_omit_default': True})
    # SizeBytes is the object size.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # ContentType is the object's content type.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # LastModified is when the object was written, set when the connector read the object back.
    last_modified: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'lastModified', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # IsFromEarlierAttempt reports that a create-only write found the object an earlier attempt of the same
    # Step execution stored, so this attempt wrote nothing.
    is_from_earlier_attempt: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFromEarlierAttempt', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.last_modified, str):
            object.__setattr__(self, 'last_modified', parse_go_datetime(self.last_modified, 'last_modified'))
        elif self.last_modified is not None:
            validate_go_datetime(self.last_modified, 'last_modified')


setattr(StoredObject, '__module__', 'dex_connectors.amazon_s3.models')
