// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package conversationtriage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/front/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	directoryGetContactStepType   = "DirectoryGetContact"
	directoryListContactsStepType = "DirectoryListContacts"
)

var directoryContactIDAttribute = dex.DefineAttribute[string]("front-directory-contact-id")

// ContactDirectoryFlow reads one contact and one bounded company contact page.
type ContactDirectoryFlow struct {
	dex.FlowDefaults
	connection front.Connection
}

// NewContactDirectoryFlow binds the Front connection at registration time.
func NewContactDirectoryFlow(connection front.Connection) *ContactDirectoryFlow {
	return &ContactDirectoryFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*ContactDirectoryFlow) GetFlowType() string { return "FrontContactDirectory" }

// GetPersistenceSchema registers the read contact ID for Dex Web.
func (*ContactDirectoryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{directoryContactIDAttribute}}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *ContactDirectoryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:front-directory-contact-id value-type:string editable:false description:"Read contact ID"
// GetDexSummary shows the selected contact ID.
func (*ContactDirectoryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	id, err := readDirectoryContactID(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-directory-contact-id": id}}, nil
}

// dex:field attribute-key:front-directory-contact-id value-type:string editable:false description:"Read contact ID"
// GetDexDisplay shows the selected contact ID.
func (*ContactDirectoryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	id, err := readDirectoryContactID(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-directory-contact-id": id}}, nil
}

// GetSteps defines the contact read and page list path.
func (flow *ContactDirectoryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContactDirectory{}),
		dex.DefineStep(front.NewGetContactStep(front.GetContactStepConfig[front.GetContactInput]{
			StepType: directoryGetContactStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "front-read", GroupLabel: "Front reads", Explanation: "Read the requested contact by ID."},
			MapToOperationInput: mapDirectoryContactInput, Found: sdkgo.GoTo(afterDirectoryContact{}),
		})),
		dex.DefineStep(afterDirectoryContact{}),
		dex.DefineStep(front.NewListContactsStep(front.ListContactsStepConfig[front.ListContactsInput]{
			StepType: directoryListContactsStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "front-read", GroupLabel: "Front reads", Explanation: "Read one company contact page."},
			MapToOperationInput: mapDirectoryContactsInput, Listed: sdkgo.GoTo(completeContactDirectory{}),
		})),
		dex.DefineStep(completeContactDirectory{}),
	}
}

func readDirectoryContactID(ctx dex.Context) (string, error) {
	id, err := directoryContactIDAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return "", nil
	}
	return id, err
}

func mapDirectoryContactInput(input front.GetContactInput) front.GetContactInput      { return input }
func mapDirectoryContactsInput(input front.ListContactsInput) front.ListContactsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start a contact read using the supplied Front contact ID."
type startContactDirectory struct{ dex.StepDefaults }

func (startContactDirectory) GetStepType() string { return "StartContactDirectory" }
func (startContactDirectory) WaitFor(dex.Context, front.GetContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContactDirectory) Execute(_ dex.Context, input front.GetContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[front.GetContactInput](directoryGetContactStepType), input), nil
}

// dex:group group-id:front-read group-label:"Front reads"
// dex:explanation text:"Save the contact ID and list the first contact page."
type afterDirectoryContact struct {
	dex.StepDefaultsNoWaitFor[front.GetContactResult]
}

func (afterDirectoryContact) GetStepType() string { return "AfterDirectoryContact" }
func (afterDirectoryContact) Execute(ctx dex.Context, result front.GetContactResult) (*dex.StepDecision, error) {
	if err := directoryContactIDAttribute.Set(ctx, result.Value.ID); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[front.ListContactsInput](directoryListContactsStepType), front.ListContactsInput{PageSize: 25}), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the typed company contact page."
type completeContactDirectory struct {
	dex.StepDefaultsNoWaitFor[front.ListContactsResult]
}

func (completeContactDirectory) GetStepType() string { return "CompleteContactDirectory" }
func (completeContactDirectory) Execute(_ dex.Context, result front.ListContactsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContactDirectoryFlow)(nil)
