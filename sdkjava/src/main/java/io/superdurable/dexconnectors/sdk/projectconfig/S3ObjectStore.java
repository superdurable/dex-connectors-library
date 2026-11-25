// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;
import java.util.regex.Pattern;

/**
 * Versioned S3 project object store with exact KMS and conditional-write boundaries.
 *
 * <p>Writes use {@code If-None-Match: *} or {@code If-Match} with the exact prior ETag. An
 * ambiguous write error is {@link OutcomeUnknownException}. Reads require an exact version and,
 * with a KMS key, the same {@code aws:kms} key. Construct it with {@link #create}.
 */
public final class S3ObjectStore implements ObjectStore {
    private static final Pattern KMS_KEY_ARN =
            Pattern.compile("^arn:[a-z0-9-]+:kms:[a-z0-9-]+:[0-9]{12}:key/[a-zA-Z0-9-]+$");

    private final S3ObjectClient client;
    private final String bucket;
    private final String prefix;
    private final String kmsKeyArn;

    private S3ObjectStore(
            final S3ObjectClient client, final String bucket, final String prefix, final String kmsKeyArn,
            final boolean allowUnencrypted) {
        final String key = kmsKeyArn == null ? "" : kmsKeyArn;
        if (client == null || bucket == null || bucket.isEmpty() || bucket.indexOf('/') >= 0
                || bucket.indexOf('\\') >= 0 || bucket.indexOf('\u0000') >= 0
                || !(KMS_KEY_ARN.matcher(key).matches() || (allowUnencrypted && key.isEmpty()))) {
            throw new IllegalArgumentException("versioned project storage and encryption configuration are required");
        }
        String normalizedPrefix = "";
        if (prefix != null && !prefix.isEmpty()) {
            String trimmed = prefix;
            while (trimmed.endsWith("/")) {
                trimmed = trimmed.substring(0, trimmed.length() - 1);
            }
            InMemoryObjectStore.validateObjectKey(trimmed);
            normalizedPrefix = trimmed + "/";
        }
        this.client = client;
        this.bucket = bucket;
        this.prefix = normalizedPrefix;
        this.kmsKeyArn = key;
    }

    /**
     * Validates bucket versioning before constructing the store.
     *
     * @param client the application's S3 client adapter
     * @param bucket the bucket
     * @param prefix the object key prefix, or empty
     * @param kmsKeyArn the KMS key ARN, or empty only when {@code allowUnencrypted}
     * @param allowUnencrypted whether a local fixture store may skip KMS
     * @return the store
     * @throws IOException if the versioning request fails
     * @throws IllegalArgumentException if configuration is invalid or versioning is not enabled
     */
    public static S3ObjectStore create(
            final S3ObjectClient client, final String bucket, final String prefix, final String kmsKeyArn,
            final boolean allowUnencrypted) throws IOException {
        final S3ObjectStore store = new S3ObjectStore(client, bucket, prefix, kmsKeyArn, allowUnencrypted);
        if (!"Enabled".equals(client.getBucketVersioning(bucket))) {
            throw new IllegalArgumentException("project object bucket versioning must be enabled");
        }
        return store;
    }

    /** {@inheritDoc} */
    @Override
    public ProjectObject readObject(final String key, final String version) throws IOException {
        InMemoryObjectStore.validateObjectKey(key);
        final S3ObjectClient.GetResponse response;
        try {
            response = client.getObject(bucket, prefix + key, version == null ? "" : version,
                    InMemoryObjectStore.MAXIMUM_OBJECT_BYTES + 1);
        } catch (IOException error) {
            throw classify(error, false);
        }
        final byte[] body = response.getBody();
        if (body == null) {
            throw new IllegalArgumentException("project object body is unavailable");
        }
        final String observedVersion = response.getVersionId();
        if (observedVersion.isEmpty() || "null".equals(observedVersion) || response.getEtag().isEmpty()
                || (version != null && !version.isEmpty() && !observedVersion.equals(version))) {
            throw new IllegalArgumentException("project object lacks exact version identity");
        }
        if (!kmsKeyArn.isEmpty() && (!"aws:kms".equals(response.getServerSideEncryption())
                || !kmsKeyArn.equals(response.getKmsKeyId()))) {
            throw new IllegalArgumentException("project object encryption boundary differs");
        }
        if (body.length == 0 || body.length > InMemoryObjectStore.MAXIMUM_OBJECT_BYTES) {
            throw new IllegalArgumentException("project object body is unavailable or oversized");
        }
        return new ProjectObject(key, observedVersion, response.getEtag(), body);
    }

    /** {@inheritDoc} */
    @Override
    public ProjectObject createObject(final String key, final byte[] contents) throws IOException {
        return writeObject(key, contents, "");
    }

    /** {@inheritDoc} */
    @Override
    public ProjectObject compareAndSwapObject(final String key, final String expectedEtag, final byte[] contents)
            throws IOException {
        if (expectedEtag == null || expectedEtag.isEmpty()) {
            throw new IllegalArgumentException("object compare-and-swap requires an ETag");
        }
        return writeObject(key, contents, expectedEtag);
    }

    private ProjectObject writeObject(final String key, final byte[] contents, final String etag) throws IOException {
        InMemoryObjectStore.validateObjectKey(key);
        if (contents == null || contents.length == 0 || contents.length > InMemoryObjectStore.MAXIMUM_OBJECT_BYTES) {
            throw new IllegalArgumentException("project object body is empty or oversized");
        }
        final S3ObjectClient.PutResponse response;
        try {
            response = client.putObject(new S3ObjectClient.PutRequest(bucket, prefix + key, contents, etag,
                    etag.isEmpty() ? "*" : "", kmsKeyArn));
        } catch (IOException error) {
            throw classify(error, true);
        }
        if (response.getVersionId().isEmpty() || "null".equals(response.getVersionId())
                || response.getEtag().isEmpty()) {
            throw new OutcomeUnknownException("project write has no exact version identity");
        }
        return new ProjectObject(key, response.getVersionId(), response.getEtag(), contents);
    }

    private static IOException classify(final IOException error, final boolean isWrite) {
        final int status = error instanceof S3ObjectClient.S3RequestException
                ? ((S3ObjectClient.S3RequestException) error).getStatusCode() : 0;
        if (status == 404 && !isWrite) {
            return new ObjectNotFoundException("project object not found");
        }
        if (status == 409 || status == 412) {
            return new ObjectConflictException("project object conditional write conflict");
        }
        if (isWrite) {
            return new OutcomeUnknownException("project write outcome is unknown");
        }
        return new IOException("project object read failed");
    }
}
