// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/front/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// frontUIConstants resolves the front identifiers the Flow source names to their values.
var frontUIConstants = map[string]string{
	"UIUnitInboxPicker": front.UIUnitInboxPicker, "UIInboxPickerPortInboxID": front.UIInboxPickerPortInboxID,
	"UIUnitTeammatePicker": front.UIUnitTeammatePicker, "UITeammatePickerPortTeammateID": front.UITeammatePickerPortTeammateID,
	"UIUnitTagPicker": front.UIUnitTagPicker, "UITagPickerPortTagID": front.UITagPickerPortTagID,
}

// TestFlowConfigurationUnitsAreTheVisibleFields reads every Step unit from the Flow source, which Dex Web analyzes statically.
func TestFlowConfigurationUnitsAreTheVisibleFields(t *testing.T) {
	unitsByStepType := readStepConfigurationUnits(t, filepath.Join("flow", "workflow.go"))
	searchStepType, routingStepType := conversationtriage.SearchConfigurationRef().StepType, conversationtriage.RoutingConfigurationRef().StepType
	require.Len(t, unitsByStepType, 2, "only the search and routing Steps expose units")

	searchUnits := unitsByStepType[searchStepType]
	require.NoError(t, sdkgo.ConnectorConfigurationUI{Units: searchUnits}.Validate())
	require.Len(t, searchUnits, 1)
	inbox := searchUnits[0]
	require.Equal(t, "searchInbox", inbox.ID)
	require.Equal(t, front.UIUnitInboxPicker, inbox.UnitID)
	require.False(t, inbox.Required, "blank searches every inbox")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: front.UIInboxPickerPortInboxID, JSONPointer: "/inboxId"}}, inbox.Bindings)
	require.Contains(t, inbox.Description, "stores the inbox ID, such as inb_")
	require.Contains(t, inbox.Description, "Leave it empty to search every inbox")

	routingUnits := unitsByStepType[routingStepType]
	require.NoError(t, sdkgo.ConnectorConfigurationUI{Units: routingUnits}.Validate())
	require.Len(t, routingUnits, 2)
	tag, assignee := routingUnits[0], routingUnits[1]
	require.Equal(t, "triageTag", tag.ID)
	require.Equal(t, front.UIUnitTagPicker, tag.UnitID)
	require.True(t, tag.Required)
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: front.UITagPickerPortTagID, JSONPointer: "/tagId"}}, tag.Bindings)
	require.Contains(t, tag.Description, "stores the tag ID, such as tag_")
	require.Contains(t, tag.Description, "while it is unset, every Flow fails before calling Front")
	require.Equal(t, "triageAssignee", assignee.ID)
	require.Equal(t, front.UIUnitTeammatePicker, assignee.UnitID)
	require.False(t, assignee.Required, "blank keeps the current assignee")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: front.UITeammatePickerPortTeammateID, JSONPointer: "/assigneeId"}}, assignee.Bindings)
	require.Contains(t, assignee.Description, "stores the teammate ID, such as tea_")
	require.Contains(t, assignee.Description, "Leave it empty to keep each conversation's current assignee")

	pickedValues := map[string]string{
		front.UIInboxPickerPortInboxID: fakeInboxID, front.UITagPickerPortTagID: fakeTriageTagID, front.UITeammatePickerPortTeammateID: fakeTeammateID,
	}
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{
		savedOperationConfiguration(t, conversationtriage.SearchConfigurationRef(), searchUnits, pickedValues),
		savedOperationConfiguration(t, conversationtriage.RoutingConfigurationRef(), routingUnits, pickedValues),
	}}
	search, err := loadSearchConfiguration(configuration)
	require.NoError(t, err)
	require.Equal(t, conversationtriage.SearchConfiguration{InboxID: fakeInboxID}, search.Value, "the inbox pick reaches the search")
	routing, err := loadRoutingConfiguration(configuration, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.Equal(t, conversationtriage.RoutingConfiguration{AssigneeID: fakeTeammateID, TagID: fakeTriageTagID}, routing.Value,
		"the tag and teammate picks reach the update")
}

// savedOperationConfiguration writes each bound port's picked value where Dex Web saves it.
func savedOperationConfiguration(
	t *testing.T, reference sdkgo.ConnectorConfigurationRef, units []sdkgo.ConnectorUIUnit, pickedValues map[string]string,
) projectconfig.OperationConfiguration {
	t.Helper()
	saved := map[string]string{}
	for _, unit := range units {
		for _, binding := range unit.Bindings {
			saved[strings.TrimPrefix(binding.JSONPointer, "/")] = pickedValues[binding.Port]
		}
	}
	encoded, err := json.Marshal(saved)
	require.NoError(t, err)
	return projectconfig.OperationConfiguration{
		ConnectorID: reference.ConnectorID, ConnectionName: reference.ConnectionName, OperationID: reference.OperationID,
		FlowType: reference.FlowType, StepType: reference.StepType, Configuration: encoded,
	}
}

// readStepConfigurationUnits maps each Step type to the units its factory config literal declares.
func readStepConfigurationUnits(t *testing.T, path string) map[string][]sdkgo.ConnectorUIUnit {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)
	stringConstants := map[string]string{}
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value := spec.(*ast.ValueSpec)
			for index, name := range value.Names {
				if index < len(value.Values) {
					if literal, isLiteral := value.Values[index].(*ast.BasicLit); isLiteral && literal.Kind == token.STRING {
						stringConstants[name.Name] = unquoteLiteral(t, literal)
					}
				}
			}
		}
	}
	unitsByStepType := map[string][]sdkgo.ConnectorUIUnit{}
	ast.Inspect(file, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.CompositeLit)
		if !isLiteral {
			return true
		}
		fields := keyedFields(literal)
		configurationUI, hasConfigurationUI := fields["ConfigurationUI"]
		if !hasConfigurationUI {
			return true
		}
		stepType, hasStepType := fields["StepType"].(*ast.Ident)
		require.True(t, hasStepType, "a Step with units names its StepType constant")
		resolvedStepType, isKnown := stringConstants[stepType.Name]
		require.True(t, isKnown, stepType.Name)
		unitsLiteral := keyedFields(configurationUI.(*ast.CompositeLit))["Units"].(*ast.CompositeLit)
		for _, element := range unitsLiteral.Elts {
			unitsByStepType[resolvedStepType] = append(unitsByStepType[resolvedStepType], readUnit(t, element.(*ast.CompositeLit)))
		}
		return true
	})
	return unitsByStepType
}

func readUnit(t *testing.T, literal *ast.CompositeLit) sdkgo.ConnectorUIUnit {
	t.Helper()
	unit := sdkgo.ConnectorUIUnit{}
	for key, value := range keyedFields(literal) {
		switch key {
		case "ID":
			unit.ID = unquoteLiteral(t, value.(*ast.BasicLit))
		case "UnitID":
			unit.UnitID = frontConstant(t, value)
		case "Label":
			unit.Label = unquoteLiteral(t, value.(*ast.BasicLit))
		case "Description":
			unit.Description = unquoteLiteral(t, value.(*ast.BasicLit))
		case "Required":
			unit.Required = value.(*ast.Ident).Name == "true"
		case "Bindings":
			for _, element := range value.(*ast.CompositeLit).Elts {
				binding := keyedFields(element.(*ast.CompositeLit))
				unit.Bindings = append(unit.Bindings, sdkgo.ConnectorUIBinding{
					Port: frontConstant(t, binding["Port"]), JSONPointer: unquoteLiteral(t, binding["JSONPointer"].(*ast.BasicLit)),
				})
			}
		default:
			require.Failf(t, "unexpected unit field", "%s", key)
		}
	}
	return unit
}

func keyedFields(literal *ast.CompositeLit) map[string]ast.Expr {
	fields := map[string]ast.Expr{}
	for _, element := range literal.Elts {
		if keyValue, isKeyValue := element.(*ast.KeyValueExpr); isKeyValue {
			if key, isIdent := keyValue.Key.(*ast.Ident); isIdent {
				fields[key.Name] = keyValue.Value
			}
		}
	}
	return fields
}

func frontConstant(t *testing.T, expression ast.Expr) string {
	t.Helper()
	selector, isSelector := expression.(*ast.SelectorExpr)
	require.True(t, isSelector, "the Flow names the front package constant")
	require.Equal(t, "front", selector.X.(*ast.Ident).Name)
	value, isKnown := frontUIConstants[selector.Sel.Name]
	require.True(t, isKnown, selector.Sel.Name)
	return value
}

func unquoteLiteral(t *testing.T, literal *ast.BasicLit) string {
	t.Helper()
	value, err := strconv.Unquote(literal.Value)
	require.NoError(t, err)
	return value
}
