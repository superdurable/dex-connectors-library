// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

/**
 * Provider or local fact behind a {@link Failure}; a kind alone does not choose a Step route.
 *
 * <p>The enum constant names are the wire values shared with the Go, Python, and TypeScript
 * connector SDKs.
 */
public enum FailureKind {
    /** The request or application input failed validation. */
    VALIDATION,
    /** The provider rejected the credential identity. */
    AUTHENTICATION,
    /** The credential is valid but lacks permission. */
    AUTHORIZATION,
    /** The requested provider object does not exist. */
    NOT_FOUND,
    /** The provider reported a conflicting state. */
    CONFLICT,
    /** The provider throttled the request. */
    RATE_LIMIT,
    /** A provider quota is exhausted. */
    QUOTA_EXHAUSTED,
    /** The provider is temporarily unavailable. */
    AVAILABILITY,
    /** The provider refused the request for a business reason. */
    PROVIDER_REJECTION,
    /** The network transport failed. */
    TRANSPORT,
    /** The provider response exceeded the configured size limit. */
    RESPONSE_TOO_LARGE,
    /** The provider response violated the expected protocol. */
    PROTOCOL,
    /** Connector or application code violated an invariant. */
    LOCAL_DEFECT
}
