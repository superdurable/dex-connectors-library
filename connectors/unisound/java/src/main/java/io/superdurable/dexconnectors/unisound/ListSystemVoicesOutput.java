// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.unisound;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Query result in provider order. */
public final class ListSystemVoicesOutput {
    /** Voices returned by the provider. */
    public final List<SystemVoice> voices;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListSystemVoicesOutput(@JsonProperty("voices") List<SystemVoice> voices) {
        this.voices = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(voices));
    }

    /** Metadata for one voice. */
    public static final class SystemVoice {
        /** System voice identifier. */
        public final String voiceID;

        /** Voice display name. */
        public final String name;

        /** Language codes in provider order. */
        public final List<String> languages;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public SystemVoice(
                @JsonProperty("voiceID") String voiceID,
                @JsonProperty("name") String name,
                @JsonProperty("languages") List<String> languages) {
            this.voiceID = voiceID;
            this.name = name;
            this.languages =
                    java.util.Collections.unmodifiableList(new java.util.ArrayList<>(languages));
        }
    }
}
