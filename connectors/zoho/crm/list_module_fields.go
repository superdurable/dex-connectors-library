// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listModuleFieldsOperation = "listModuleFields"
	fieldsMetadataPath        = "/settings/fields"
)

// ListModuleFieldsInput names the module whose fields to list.
type ListModuleFieldsInput struct {
	// Module is the module API name, such as Deals.
	Module string `json:"module"`
}

// ListModuleFieldsOutput is a module's fields as Zoho CRM describes them, across every layout.
type ListModuleFieldsOutput struct {
	// Module is the module API name.
	Module string `json:"module"`
	// Fields are the module's fields in Zoho CRM's order.
	Fields []ModuleField `json:"fields"`
}

// ModuleField is one field of a module. Values are Zoho CRM's own, such as data type picklist and
// the stage names a deal's Stage accepts.
type ModuleField struct {
	// APIName is the name every operation uses, such as Stage or a custom field's External_Deal_ID.
	APIName string `json:"apiName"`
	// DisplayLabel is the label in the Zoho CRM interface, untranslated.
	DisplayLabel string `json:"displayLabel,omitempty"`
	// DataType is Zoho CRM's field type, such as text, email, picklist, lookup, ownerlookup, currency, or datetime.
	DataType string `json:"dataType"`
	// IsCustom reports a field the organization added.
	IsCustom bool `json:"isCustom,omitempty"`
	// IsReadOnly reports a field the connection's user cannot change.
	IsReadOnly bool `json:"isReadOnly,omitempty"`
	// IsSystemMandatory reports a field every new record needs, such as Last_Name for Contacts.
	IsSystemMandatory bool `json:"isSystemMandatory,omitempty"`
	// IsUnique reports a field marked Do not allow duplicate values, which a concurrent upsert relies on.
	IsUnique bool `json:"isUnique,omitempty"`
	// Length is the maximum length of a text value, or zero when Zoho CRM states none.
	Length int `json:"length,omitempty"`
	// LookupModule is the module a lookup field points at, such as Accounts, or empty.
	LookupModule string `json:"lookupModule,omitempty"`
	// PicklistValues are a picklist's options in Zoho CRM's order, used and unused.
	PicklistValues []PicklistValue `json:"picklistValues,omitempty"`
}

// PicklistValue is one option of a picklist or multi-select picklist.
type PicklistValue struct {
	// ActualValue is the value records store and writes send, such as Closed Won.
	ActualValue string `json:"actualValue"`
	// DisplayValue is the label the Zoho CRM interface shows.
	DisplayValue string `json:"displayValue,omitempty"`
	// IsUnused reports an option Zoho CRM keeps but no longer offers.
	IsUnused bool `json:"isUnused,omitempty"`
}

// ListModuleFieldsOperation is the listModuleFields Query.
type ListModuleFieldsOperation struct {
	client *Client
}

type fieldMetadataWire struct {
	APIName         string          `json:"api_name"`
	DisplayLabel    string          `json:"display_label"`
	DataType        string          `json:"data_type"`
	CustomField     bool            `json:"custom_field"`
	ReadOnly        bool            `json:"read_only"`
	SystemMandatory bool            `json:"system_mandatory"`
	Unique          json.RawMessage `json:"unique"`
	Length          int             `json:"length"`
	Lookup          struct {
		Module struct {
			APIName string `json:"api_name"`
		} `json:"module"`
	} `json:"lookup"`
	PicklistValues []struct {
		ActualValue  string `json:"actual_value"`
		DisplayValue string `json:"display_value"`
		Type         string `json:"type"`
	} `json:"pick_list_values"`
}

// Definition returns the immutable connector operation definition.
func (ListModuleFieldsOperation) Definition() sdkgo.QueryDefinition {
	return ListModuleFieldsDefinition
}

// Invoke sends GET /crm/v8/settings/fields?module={module}, which needs ZohoCRM.settings.fields.READ.
func (operation ListModuleFieldsOperation) Invoke(call sdkgo.Call, input ListModuleFieldsInput) sdkgo.QueryAttempt[ListModuleFieldsOutput] {
	branches := queryBranches{providerRejected: ListModuleFieldsBranchProviderRejected, invalidResponse: ListModuleFieldsBranchInvalidResponse, defect: ListModuleFieldsBranchDefect}
	if err := validateModule(input.Module); err != nil {
		return sdkgo.NewQueryBranch(ListModuleFieldsBranchDefect, ListModuleFieldsOutput{}, crmFailurePointer(sdkgo.FailureValidation, listModuleFieldsOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, listModuleFieldsOperation)
	if failure != nil {
		return queryAttemptForSession[ListModuleFieldsOutput](failure, branches)
	}
	defer cancel()
	result := operation.client.exchange(session, listModuleFieldsOperation, crmRequest{
		method: http.MethodGet, path: fieldsMetadataPath, query: url.Values{"module": {input.Module}},
	})
	receipt := operation.client.receipt(call, result.response, "")
	if attempt, isTerminal := queryAttemptForExchange[ListModuleFieldsOutput](result, receipt, branches); isTerminal {
		return attempt
	}
	fields, err := decodeModuleFields(result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListModuleFieldsBranchInvalidResponse, ListModuleFieldsOutput{}, crmFailurePointer(sdkgo.FailureProtocol, listModuleFieldsOperation, "Zoho CRM returned an invalid field list: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListModuleFieldsBranchListed, ListModuleFieldsOutput{Module: input.Module, Fields: fields}, nil, receipt)
}

// FieldNamed returns the module field with apiName, or false.
func (output ListModuleFieldsOutput) FieldNamed(apiName string) (ModuleField, bool) {
	for _, field := range output.Fields {
		if field.APIName == apiName {
			return field, true
		}
	}
	return ModuleField{}, false
}

// HasPicklistValue reports whether a picklist offers actualValue among its used options.
func (field ModuleField) HasPicklistValue(actualValue string) bool {
	for _, value := range field.PicklistValues {
		if value.ActualValue == actualValue && !value.IsUnused {
			return true
		}
	}
	return false
}

func decodeModuleFields(body []byte) ([]ModuleField, error) {
	var document struct {
		Fields []fieldMetadataWire `json:"fields"`
	}
	if err := json.Unmarshal(body, &document); err != nil || document.Fields == nil {
		return nil, errors.New("the response is not a JSON object with a fields list")
	}
	fields := make([]ModuleField, 0, len(document.Fields))
	for _, wire := range document.Fields {
		if !isFieldAPIName(wire.APIName) || wire.DataType == "" {
			return nil, errors.New("a field has no valid api_name or data_type")
		}
		field := ModuleField{
			APIName: wire.APIName, DisplayLabel: wire.DisplayLabel, DataType: wire.DataType, IsCustom: wire.CustomField,
			IsReadOnly: wire.ReadOnly, IsSystemMandatory: wire.SystemMandatory, IsUnique: isUniqueFieldMetadata(wire.Unique),
			Length: max(wire.Length, 0),
		}
		if moduleAPINamePattern.MatchString(wire.Lookup.Module.APIName) {
			field.LookupModule = wire.Lookup.Module.APIName
		}
		for _, value := range wire.PicklistValues {
			field.PicklistValues = append(field.PicklistValues, PicklistValue{
				ActualValue: value.ActualValue, DisplayValue: value.DisplayValue, IsUnused: value.Type == "unused",
			})
		}
		fields = append(fields, field)
	}
	return fields, nil
}

// isUniqueFieldMetadata reads unique: {} for an ordinary field and {"case_sensitive": false} for a unique one.
func isUniqueFieldMetadata(raw json.RawMessage) bool {
	var unique map[string]json.RawMessage
	return json.Unmarshal(raw, &unique) == nil && len(unique) != 0
}
