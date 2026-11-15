// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fileintake demonstrates the Google Drive fileCreated Trigger: every file created in the
// configured folder starts one Flow, which reads the file's current metadata and completes.
package fileintake

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/google/drive/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity.
	FlowType = "GoogleDriveFileIntake"
	// ConnectionName is the static Dex Web connection for Google Drive.
	ConnectionName = "google-drive-files"
	// NewFileTriggerBinding is the fileCreated binding that starts the Flow.
	NewFileTriggerBinding = "google-drive-file-intake-new-file"

	recordNewFileStepType      = "RecordNewFile"
	readNewFileStepType        = "ReadNewFile"
	completeFileIntakeStepType = "CompleteFileIntake"
)

var outcomeAttribute = dex.DefineAttribute[Outcome]("google-drive-file-intake-outcome")

// Input is the Flow start input that the fileCreated Trigger maps from one event.
type Input struct {
	// EventID is the Trigger event ID, such as 1AbC@created.
	EventID string `json:"eventId"`
	// File is the file metadata the change log reported.
	File drive.FileEvent `json:"file"`
}

// Outcome is the durable result of one intake and the Flow's completion output.
type Outcome struct {
	// EventID is the Trigger event that started the Flow.
	EventID string `json:"eventId"`
	// Reported is the metadata the change log reported.
	Reported drive.FileEvent `json:"reported"`
	// Current is the metadata read from Drive when the Flow ran; nil until it is read.
	Current *drive.File `json:"current,omitempty"`
}

// Flow handles one newly created Drive file.
type Flow struct {
	dex.FlowDefaults
	connection drive.Connection
}

// NewFlow binds the Google Drive Connection used by the metadata read.
func NewFlow(connection drive.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the record, Google Drive read, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordNewFile{}),
		dex.DefineStep(drive.NewGetFileStep(drive.GetFileStepConfig[Input]{
			StepType: readNewFileStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-drive", GroupLabel: "Google Drive",
				Explanation: "Read the new file's current metadata from Drive.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetFileInput,
			Found: sdkgo.GoTo(completeFileIntake{}),
		})),
		dex.DefineStep(completeFileIntake{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetDexSummary returns the intake outcome.
//
// dex:field attribute-key:google-drive-file-intake-outcome value-type:json editable:false description:"New file intake outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := fileIntakeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-drive-file-intake-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the intake outcome.
//
// dex:field attribute-key:google-drive-file-intake-outcome value-type:json editable:false description:"Reported and current metadata of the new file"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := fileIntakeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-drive-file-intake-outcome": outcome,
	}}, nil
}

// GetPersistenceSchema registers the outcome Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetConnectorTriggerBindings declares the fileCreated binding and its Dex Web configuration units.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		drive.DefineFileCreatedTriggerBinding(drive.FileCreatedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: NewFileTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "folder", UnitID: drive.UIUnitFolderPicker, Label: "Watched folder",
					Description: "Choose the folder whose new files start the intake Flow; only files directly inside it count, not files in its subfolders. Leave blank to start a Flow for every new file the connection can see.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: drive.UIFolderPickerPortFolderID, JSONPointer: "/folderId"},
						{Port: drive.UIFolderPickerPortFolderName, JSONPointer: "/folderName"},
					},
				},
				{
					ID: "mimeType", UnitID: drive.UIUnitMimeTypePicker, Label: "File type",
					Description: "Choose the exact file type that starts the intake Flow, such as PDF for scanned invoices; leave blank to start a Flow for new files of every type, including folders.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: drive.UIMimeTypePickerPortMimeType, JSONPointer: "/mimeType"}},
				},
			}},
		}),
	}
}

// ResolveFlowID derives one Flow ID per created file, so a redelivered event starts no second Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[drive.FileEvent]) string {
	return "google-drive-file-intake-" + ConnectionName + "-" + event.Payload.FileID
}

// MapToFlowInput maps one fileCreated event to the Flow start input.
func MapToFlowInput(event sdkgo.TriggerEvent[drive.FileEvent]) Input {
	return Input{EventID: event.ID, File: event.Payload}
}

// AcceptCreatedFile accepts the binding's fileCreated events, which the folder and type filters already matched.
func AcceptCreatedFile(event sdkgo.TriggerEvent[drive.FileEvent]) bool {
	return strings.HasSuffix(event.ID, "@created")
}

// MapToGetFileInput reads the reported file.
func MapToGetFileInput(input Input) drive.GetFileInput {
	return drive.GetFileInput{FileID: input.File.FileID}
}

// dex:group group-id:file-intake-receive group-label:"Receive file"
// dex:explanation text:"Record the new file that the fileCreated Trigger reported."
type recordNewFile struct {
	dex.StepDefaultsNoWaitFor[Input]
}

func (recordNewFile) GetStepType() string { return recordNewFileStepType }

func (recordNewFile) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, Outcome{EventID: input.EventID, Reported: input.File}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readNewFileStepType), input), nil
}

// dex:group group-id:file-intake-complete group-label:"Complete intake"
// dex:explanation text:"Record the file's current metadata and complete the Flow."
type completeFileIntake struct {
	dex.StepDefaultsNoWaitFor[drive.GetFileResult]
}

func (completeFileIntake) GetStepType() string { return completeFileIntakeStepType }

func (completeFileIntake) Execute(ctx dex.Context, result drive.GetFileResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	current := result.Value
	outcome.Current = &current
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// fileIntakeInspection returns the outcome, or an empty one before the start Step records it.
func fileIntakeInspection(ctx dex.Context) (Outcome, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return Outcome{}, nil
	}
	return outcome, err
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
