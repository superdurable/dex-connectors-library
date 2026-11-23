// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.regex.Pattern;

/**
 * Ordered connector-owned Dex Web configuration units shown for a Step or Trigger binding.
 */
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class ConnectorConfigurationUI {
    /** A configuration UI without units. */
    public static final ConnectorConfigurationUI EMPTY =
            new ConnectorConfigurationUI(Collections.<ConnectorUIUnit>emptyList());

    static final Pattern IDENTIFIER = Pattern.compile("^[a-z][A-Za-z0-9]*$");

    private final List<ConnectorUIUnit> units;

    /**
     * Creates a configuration UI; call {@link #validate()} before use.
     *
     * @param units the units in application composition order
     */
    @JsonCreator
    public ConnectorConfigurationUI(@JsonProperty(value = "units", required = true) final List<ConnectorUIUnit> units) {
        this.units = Collections.unmodifiableList(new ArrayList<>(units));
    }

    /**
     * Checks every unit and rejects duplicate unit use IDs.
     *
     * @throws IllegalArgumentException if a unit is invalid or IDs repeat
     */
    public void validate() {
        final Set<String> ids = new HashSet<>();
        for (ConnectorUIUnit unit : units) {
            unit.validate();
            if (!ids.add(unit.getId())) {
                throw new IllegalArgumentException("Connector UI unit IDs must be unique");
            }
        }
    }

    /**
     * Returns the units in application composition order.
     *
     * @return the unmodifiable units
     */
    @JsonProperty("units")
    public List<ConnectorUIUnit> getUnits() {
        return units;
    }
}
