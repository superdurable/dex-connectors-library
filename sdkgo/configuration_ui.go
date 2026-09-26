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
	Units []ConnectorUIUnit `json:"units" yaml:"units"`
}

// ConnectorUIUnit is one reusable unit from a Connector release's Studio unit
// catalog. Bindings connect the unit's named ports to configuration JSON paths.
type ConnectorUIUnit struct {
	ID          string               `json:"id" yaml:"id"`
	UnitID      string               `json:"unitId" yaml:"unitId"`
	Label       string               `json:"label" yaml:"label"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool                 `json:"required" yaml:"required"`
	Bindings    []ConnectorUIBinding `json:"bindings" yaml:"bindings"`
}

// ConnectorUIBinding maps one Connector UI unit port to an RFC 6901 JSON
// Pointer in the application-owned configuration object.
type ConnectorUIBinding struct {
	Port        string `json:"port" yaml:"port"`
	JSONPointer string `json:"jsonPointer" yaml:"jsonPointer"`
}

// ConnectorConfigurationRef identifies one operation configuration owned by a
// stable Connector Step use. FlowType and StepType prevent two uses of the same
// operation and connection from sharing configuration implicitly.
type ConnectorConfigurationRef struct {
	ConnectorID    string `json:"connectorId" yaml:"connectorId"`
	ConnectionName string `json:"connectionName" yaml:"connectionName"`
	OperationID    string `json:"operationId" yaml:"operationId"`
	FlowType       string `json:"flowType" yaml:"flowType"`
	StepType       string `json:"stepType" yaml:"stepType"`
}

// ConnectorLoadedConfiguration is one strictly decoded, startup-time
// configuration snapshot together with the identity from which it was loaded.
// Applications explicitly use Value when mapping Flow state to operation input.
type ConnectorLoadedConfiguration[T any] struct {
	Reference ConnectorConfigurationRef
	Value     T
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
			if !validConnectorJSONPointer(binding.JSONPointer) {
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

func validConnectorJSONPointer(pointer string) bool {
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
