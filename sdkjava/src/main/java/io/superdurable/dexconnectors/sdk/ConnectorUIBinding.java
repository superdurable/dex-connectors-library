// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/**
 * Maps one released UI unit port to an application JSON Pointer.
 */
@JsonPropertyOrder({"port", "jsonPointer"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class ConnectorUIBinding {
    private final String port;
    private final String jsonPointer;

    /**
     * Creates a binding; call {@link #validate()} before use.
     *
     * @param port the lower-camel unit port
     * @param jsonPointer the nonempty RFC 6901 JSON Pointer into the application value
     */
    @JsonCreator
    public ConnectorUIBinding(
            @JsonProperty(value = "port", required = true) final String port,
            @JsonProperty(value = "jsonPointer", required = true) final String jsonPointer) {
        this.port = port == null ? "" : port;
        this.jsonPointer = jsonPointer == null ? "" : jsonPointer;
    }

    /**
     * Requires a lower-camel port and a valid nonempty RFC 6901 path.
     *
     * @throws IllegalArgumentException if the binding is invalid
     */
    public void validate() {
        if (!ConnectorConfigurationUI.IDENTIFIER.matcher(port).matches() || !jsonPointer.startsWith("/")) {
            throw new IllegalArgumentException("invalid Connector UI binding");
        }
        for (int index = 0; index < jsonPointer.length(); index++) {
            if (jsonPointer.charAt(index) == '~') {
                if (index + 1 >= jsonPointer.length()
                        || (jsonPointer.charAt(index + 1) != '0' && jsonPointer.charAt(index + 1) != '1')) {
                    throw new IllegalArgumentException("invalid Connector UI JSON Pointer escape");
                }
                index++;
            }
        }
    }

    /**
     * Returns the unit port.
     *
     * @return the port
     */
    @JsonProperty("port")
    public String getPort() {
        return port;
    }

    /**
     * Returns the application JSON Pointer.
     *
     * @return the pointer
     */
    @JsonProperty("jsonPointer")
    public String getJsonPointer() {
        return jsonPointer;
    }
}
