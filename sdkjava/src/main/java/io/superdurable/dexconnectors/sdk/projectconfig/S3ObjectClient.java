// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.annotation.JsonSerialize;
import io.superdurable.dexconnectors.sdk.PrivateValueRefusingSerializer;
import java.io.IOException;

/**
 * The versioned S3 calls {@link S3ObjectStore} needs, implemented by the application.
 *
 * <p>The connector SDK does not depend on an AWS SDK; adapt the application's S3 client, with
 * retries disabled, to this interface. Errors carry the HTTP status so the store can classify
 * not-found, conflict, and unknown write outcomes.
 */
public interface S3ObjectClient {
    /**
     * Returns the bucket versioning status, such as {@code Enabled}.
     *
     * @param bucket the bucket
     * @return the versioning status
     * @throws IOException if the request fails
     */
    String getBucketVersioning(String bucket) throws IOException;

    /**
     * Reads one object version, or the current version when {@code versionId} is empty.
     *
     * @param bucket the bucket
     * @param key the full object key
     * @param versionId the exact version, or empty
     * @param maximumBytes the largest body to read; a longer body may be truncated past this bound
     * @return the response
     * @throws S3RequestException with the HTTP status for a service error
     * @throws IOException if the transport fails
     */
    GetResponse getObject(String bucket, String key, String versionId, int maximumBytes) throws IOException;

    /**
     * Writes one object once with a precondition.
     *
     * @param request the write
     * @return the response
     * @throws S3RequestException with the HTTP status for a service error
     * @throws IOException if the transport fails
     */
    PutResponse putObject(PutRequest request) throws IOException;

    /**
     * One object read; it holds private object contents, so Jackson refuses to serialize it.
     */
    @JsonSerialize(using = PrivateValueRefusingSerializer.class)
    final class GetResponse {
        private final String versionId;
        private final String etag;
        private final String serverSideEncryption;
        private final String kmsKeyId;
        private final byte[] body;

        /**
         * Creates a read response.
         *
         * @param versionId the version ID
         * @param etag the ETag
         * @param serverSideEncryption the encryption algorithm, such as {@code aws:kms}
         * @param kmsKeyId the KMS key ARN, or empty
         * @param body the bytes read
         */
        public GetResponse(
                final String versionId, final String etag, final String serverSideEncryption, final String kmsKeyId,
                final byte[] body) {
            this.versionId = versionId == null ? "" : versionId;
            this.etag = etag == null ? "" : etag;
            this.serverSideEncryption = serverSideEncryption == null ? "" : serverSideEncryption;
            this.kmsKeyId = kmsKeyId == null ? "" : kmsKeyId;
            this.body = body == null ? null : body.clone();
        }

        String getVersionId() {
            return versionId;
        }

        String getEtag() {
            return etag;
        }

        String getServerSideEncryption() {
            return serverSideEncryption;
        }

        String getKmsKeyId() {
            return kmsKeyId;
        }

        byte[] getBody() {
            return body;
        }
    }

    /**
     * One conditional object write; it holds private object contents, so Jackson refuses to
     * serialize it. Build the S3 call explicitly from its getters.
     */
    @JsonSerialize(using = PrivateValueRefusingSerializer.class)
    final class PutRequest {
        private final String bucket;
        private final String key;
        private final byte[] body;
        private final String ifMatch;
        private final String ifNoneMatch;
        private final String kmsKeyId;

        PutRequest(final String bucket, final String key, final byte[] body, final String ifMatch,
                final String ifNoneMatch, final String kmsKeyId) {
            this.bucket = bucket;
            this.key = key;
            this.body = body.clone();
            this.ifMatch = ifMatch;
            this.ifNoneMatch = ifNoneMatch;
            this.kmsKeyId = kmsKeyId;
        }

        /**
         * Returns the bucket.
         *
         * @return the bucket
         */
        public String getBucket() {
            return bucket;
        }

        /**
         * Returns the full object key.
         *
         * @return the key
         */
        public String getKey() {
            return key;
        }

        /**
         * Returns the {@code application/json} body.
         *
         * @return a copy of the body
         */
        public byte[] getBody() {
            return body.clone();
        }

        /**
         * Returns the {@code If-Match} ETag for a compare-and-swap.
         *
         * @return the ETag, or empty for a create
         */
        public String getIfMatch() {
            return ifMatch;
        }

        /**
         * Returns the {@code If-None-Match} value for a create.
         *
         * @return {@code *}, or empty for a compare-and-swap
         */
        public String getIfNoneMatch() {
            return ifNoneMatch;
        }

        /**
         * Returns the KMS key for {@code aws:kms} server-side encryption.
         *
         * @return the key ARN, or empty for an explicitly unencrypted local store
         */
        public String getKmsKeyId() {
            return kmsKeyId;
        }
    }

    /**
     * One object write result.
     */
    final class PutResponse {
        private final String versionId;
        private final String etag;

        /**
         * Creates a write response.
         *
         * @param versionId the new version ID
         * @param etag the new ETag
         */
        public PutResponse(final String versionId, final String etag) {
            this.versionId = versionId == null ? "" : versionId;
            this.etag = etag == null ? "" : etag;
        }

        String getVersionId() {
            return versionId;
        }

        String getEtag() {
            return etag;
        }
    }

    /**
     * An S3 service error with its HTTP status.
     */
    final class S3RequestException extends IOException {
        private static final long serialVersionUID = 1L;

        private final int statusCode;

        /**
         * Creates the exception.
         *
         * @param statusCode the HTTP status code
         */
        public S3RequestException(final int statusCode) {
            super("S3 request failed with HTTP " + statusCode);
            this.statusCode = statusCode;
        }

        /**
         * Returns the HTTP status code.
         *
         * @return the status
         */
        public int getStatusCode() {
            return statusCode;
        }
    }
}
