// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const maximumObjectBytes = 4 << 20

var kmsKeyARN = regexp.MustCompile(`^arn:[a-z0-9-]+:kms:[a-z0-9-]+:[0-9]{12}:key/[a-zA-Z0-9-]+$`)

// S3StoreConfig fixes the storage boundary before any application or browser request is accepted.
type S3StoreConfig struct {
	// Client is an authenticated S3 client; its owner controls endpoint, transport, and IAM credentials.
	Client *s3.Client
	// Bucket is an existing versioned, private bucket authorized for this workload.
	Bucket string
	// Prefix is an optional trusted environment prefix beneath Bucket.
	Prefix string
	// KMSKeyID is the exact KMS key ARN required for every write and read; aliases are rejected.
	KMSKeyID string
	// AllowUnencrypted permits an isolated local MinIO fixture without KMS, never hosted credentials.
	AllowUnencrypted bool
}

// S3ObjectStore implements durable conditional writes against a versioned S3 or MinIO bucket.
type S3ObjectStore struct {
	client                 *s3.Client
	bucket, prefix, kmsKey string
	allowUnencrypted       bool
}

// NewS3ObjectStore validates configuration and bucket versioning without creating or modifying resources.
func NewS3ObjectStore(ctx context.Context, config *S3StoreConfig) (*S3ObjectStore, error) {
	if config == nil || config.Client == nil || config.Bucket == "" || strings.ContainsAny(config.Bucket, "/\\\x00") || (!kmsKeyARN.MatchString(config.KMSKeyID) && !(config.AllowUnencrypted && config.KMSKeyID == "")) {
		return nil, errors.New("versioned project storage and encryption configuration are required")
	}
	prefix := strings.TrimSuffix(config.Prefix, "/")
	if prefix != "" {
		if err := validateObjectKey(prefix); err != nil {
			return nil, err
		}
		prefix += "/"
	}
	versioning, err := config.Client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(config.Bucket)})
	if err != nil || versioning == nil || versioning.Status != types.BucketVersioningStatusEnabled {
		return nil, errors.New("project object bucket versioning must be enabled")
	}
	return &S3ObjectStore{client: config.Client, bucket: config.Bucket, prefix: prefix, kmsKey: config.KMSKeyID, allowUnencrypted: config.AllowUnencrypted}, nil
}

// ReadObject returns bounded private bytes and the exact version and ETag observed by S3.
func (store *S3ObjectStore) ReadObject(ctx context.Context, key, version string) (Object, error) {
	if err := validateObjectKey(key); err != nil {
		return Object{}, err
	}
	input := &s3.GetObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(store.prefix + key)}
	if version != "" {
		input.VersionId = aws.String(version)
	}
	output, err := store.client.GetObject(ctx, input)
	if err != nil {
		return Object{}, classifyStorageError(err, false)
	}
	defer func() { _ = output.Body.Close() }() // Read-only response cleanup cannot alter the operation outcome.
	if output.VersionId == nil || *output.VersionId == "" || *output.VersionId == "null" || output.ETag == nil || *output.ETag == "" || (version != "" && *output.VersionId != version) {
		return Object{}, errors.New("project object lacks exact version identity")
	}
	if store.kmsKey != "" && (output.ServerSideEncryption != types.ServerSideEncryptionAwsKms || aws.ToString(output.SSEKMSKeyId) != store.kmsKey) {
		return Object{}, errors.New("project object encryption boundary differs")
	}
	contents, err := io.ReadAll(io.LimitReader(output.Body, maximumObjectBytes+1))
	if err != nil || len(contents) == 0 || len(contents) > maximumObjectBytes {
		return Object{}, errors.New("project object body is unavailable or oversized")
	}
	return Object{Key: key, Version: *output.VersionId, ETag: *output.ETag, Contents: contents}, nil
}

// CreateObject writes once using If-None-Match; ambiguous outcomes require a read of the same key.
func (store *S3ObjectStore) CreateObject(ctx context.Context, key string, contents []byte) (Object, error) {
	return store.writeObject(ctx, key, "", contents)
}

// CompareAndSwapObject never overwrites a newer writer's ETag.
func (store *S3ObjectStore) CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (Object, error) {
	if expectedETag == "" {
		return Object{}, errors.New("object compare-and-swap requires an ETag")
	}
	return store.writeObject(ctx, key, expectedETag, contents)
}

func (store *S3ObjectStore) writeObject(ctx context.Context, key, etag string, contents []byte) (Object, error) {
	if err := validateObjectKey(key); err != nil {
		return Object{}, err
	}
	if len(contents) == 0 || len(contents) > maximumObjectBytes {
		return Object{}, errors.New("project object body is empty or oversized")
	}
	input := &s3.PutObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(store.prefix + key), Body: bytes.NewReader(contents), ContentType: aws.String("application/json")}
	if etag == "" {
		input.IfNoneMatch = aws.String("*")
	} else {
		input.IfMatch = aws.String(etag)
	}
	if store.kmsKey != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = aws.String(store.kmsKey)
	}
	output, err := store.client.PutObject(ctx, input, func(options *s3.Options) { options.RetryMaxAttempts = 1 })
	if err != nil {
		return Object{}, classifyStorageError(err, true)
	}
	if output.VersionId == nil || *output.VersionId == "" || *output.VersionId == "null" || output.ETag == nil || *output.ETag == "" {
		return Object{}, ErrOutcomeUnknown
	}
	return Object{Key: key, Version: *output.VersionId, ETag: *output.ETag, Contents: contents}, nil
}

func classifyStorageError(err error, isWrite bool) error {
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NoSuchKey", "NoSuchVersion", "NotFound":
			return ErrObjectNotFound
		case "PreconditionFailed", "ConditionalRequestConflict", "OperationAborted":
			return ErrConflict
		}
	}
	if isWrite {
		return ErrOutcomeUnknown
	}
	return errors.New("project object read failed")
}
