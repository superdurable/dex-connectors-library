// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tianxiagongchang;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Account snapshot returned by the provider. */
public final class GetAccountStatusOutput {
    /** Application identified by the key. */
    public final long appId;

    /** Account-wide available provider credits. */
    public final long creditsBalance;

    /** Credits frozen for this application. */
    public final long frozenCredits;

    /** Whether the provider identified the key as a sandbox key. */
    public final boolean isSandbox;

    /** Restore the provider snapshot from the Dex result. */
    @JsonCreator
    public GetAccountStatusOutput(
            @JsonProperty("appId") long appId,
            @JsonProperty("creditsBalance") long creditsBalance,
            @JsonProperty("frozenCredits") long frozenCredits,
            @JsonProperty("isSandbox") boolean isSandbox) {
        this.appId = appId;
        this.creditsBalance = creditsBalance;
        this.frozenCredits = frozenCredits;
        this.isSandbox = isSandbox;
    }
}
