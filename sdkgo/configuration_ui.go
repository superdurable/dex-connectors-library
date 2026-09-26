// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"fmt"
	"regexp"
	"strings"
)

var connectorUIIdentifierPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// ConnectorConfigurationUI declares the ordered Connector-owned UI units that
// Dex Web composes for one Connector Step or Trigger binding configuration.
// It contains presentation metadata only; configuration values are loaded
// separately at application startup.
type ConnectorConfigurationUI struct {
	// Units lists reusable UI units in application composition order.
	Units []ConnectorUIUnit `json:"units" yaml:"units"`
}

// ConnectorUIUnit is one reusable unit from a Connector release's Studio unit
// catalog. Bindings connect the unit's named ports to configuration JSON paths.
type ConnectorUIUnit struct {
	// ID is the stable application-local identity of this unit use.
	ID string `json:"id" yaml:"id"`
	// UnitID identifies a unit in the connector's released UI catalog.
	UnitID string `json:"unitId" yaml:"unitId"`
	// Label is the application-facing unit label.
	Label string `json:"label" yaml:"label"`
	// Description optionally explains the unit's purpose.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Required reports whether the application must configure this unit.
	Required bool `json:"required" yaml:"required"`
	// Bindings map unit ports to application-owned configuration paths.
	Bindings []ConnectorUIBinding `json:"bindings" yaml:"bindings"`
}

// ConnectorUIBinding maps one Connector UI unit port to an RFC 6901 JSON
// Pointer in the application-owned configuration object.
type ConnectorUIBinding struct {
	// Port names an input or output declared by the released unit.
	Port string `json:"port" yaml:"port"`
	// JSONPointer is an RFC 6901 path into application configuration.
	JSONPointer string `json:"jsonPointer" yaml:"jsonPointer"`
}

// ConnectorConfigurationRef identifies one operation configuration owned by a
// stable Connector Step use. FlowType and StepType prevent two uses of the same
// operation and connection from sharing configuration implicitly.
type ConnectorConfigurationRef struct {
	// ConnectorID identifies the connector manifest.
	ConnectorID string `json:"connectorId" yaml:"connectorId"`
	// ConnectionName names the configured provider connection.
	ConnectionName string `json:"connectionName" yaml:"connectionName"`
	// OperationID identifies the operation within its connector manifest.
	OperationID string `json:"operationId" yaml:"operationId"`
	// FlowType is the stable Dex Flow type that owns the Step.
	FlowType string `json:"flowType" yaml:"flowType"`
	// StepType is the stable Dex Step type.
	StepType string `json:"stepType" yaml:"stepType"`
}

// ConnectorLoadedConfiguration is one strictly decoded, startup-time
// configuration snapshot together with the identity from which it was loaded.
// Applications explicitly use Value when mapping Flow state to operation input.
type ConnectorLoadedConfiguration[T any] struct {
	// Reference identifies the Connector Step use that owns Value.
	Reference ConnectorConfigurationRef
	// Value is the strictly decoded startup-time configuration snapshot.
	Value T
}

// Validate checks the static UI declaration independently of a Connector's
// release-specific unit catalog. Dex CLI and Dex Web additionally validate unit
// and port existence against the exact Connector release.
func (configuration ConnectorConfigurationUI) Validate() error {
	unitIDs := make(map[string]bool, len(configuration.Units))
	for index, unit := range configuration.Units {
		if !connectorUIIdentifierPattern.MatchString(unit.ID) {
			return fmt.Errorf("Connector UI unit %d ID must be lower camel case", index)
		}
		if unitIDs[unit.ID] {
			return fmt.Errorf("Connector UI unit ID %q is duplicated", unit.ID)
		}
		unitIDs[unit.ID] = true
		if !connectorUIIdentifierPattern.MatchString(unit.UnitID) {
			return fmt.Errorf("Connector UI unit %q unit ID must be lower camel case", unit.ID)
		}
		if strings.TrimSpace(unit.Label) == "" {
			return fmt.Errorf("Connector UI unit %q label is required", unit.ID)
		}
		if len(unit.Bindings) == 0 {
			return fmt.Errorf("Connector UI unit %q requires at least one binding", unit.ID)
		}
		ports := make(map[string]bool, len(unit.Bindings))
		for bindingIndex, binding := range unit.Bindings {
			if !connectorUIIdentifierPattern.MatchString(binding.Port) {
				return fmt.Errorf("Connector UI unit %q binding %d port must be lower camel case", unit.ID, bindingIndex)
			}
			if ports[binding.Port] {
				return fmt.Errorf("Connector UI unit %q port %q is duplicated", unit.ID, binding.Port)
			}
			ports[binding.Port] = true
			if !isValidConnectorJSONPointer(binding.JSONPointer) {
				return fmt.Errorf("Connector UI unit %q port %q has an invalid JSON Pointer", unit.ID, binding.Port)
			}
		}
	}
	return nil
}

// Validate checks the stable identity used to load one operation configuration.
func (reference ConnectorConfigurationRef) Validate() error {
	if !connectorIDPattern.MatchString(reference.ConnectorID) {
		return fmt.Errorf("Connector configuration connector ID must be DNS-like and 2-63 characters")
	}
	if strings.TrimSpace(reference.ConnectionName) == "" {
		return fmt.Errorf("Connector configuration connection name is required")
	}
	if !operationIDPattern.MatchString(reference.OperationID) {
		return fmt.Errorf("Connector configuration operation ID must be lower camel case")
	}
	if strings.TrimSpace(reference.FlowType) == "" || strings.TrimSpace(reference.StepType) == "" {
		return fmt.Errorf("Connector configuration Flow and Step types are required")
	}
	return nil
}

func cloneConnectorConfigurationUI(configuration ConnectorConfigurationUI) ConnectorConfigurationUI {
	cloned := ConnectorConfigurationUI{Units: make([]ConnectorUIUnit, len(configuration.Units))}
	for index, unit := range configuration.Units {
		cloned.Units[index] = unit
		cloned.Units[index].Bindings = append([]ConnectorUIBinding(nil), unit.Bindings...)
	}
	return cloned
}

func isValidConnectorJSONPointer(pointer string) bool {
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return false
	}
	for index := 0; index < len(pointer); index++ {
		if pointer[index] != '~' {
			continue
		}
		if index+1 >= len(pointer) || (pointer[index+1] != '0' && pointer[index+1] != '1') {
			return false
		}
		index++
	}
	return true
}
