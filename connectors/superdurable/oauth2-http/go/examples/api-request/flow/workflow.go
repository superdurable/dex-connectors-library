// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package apirequest demonstrates one authenticated GET request to an OAuth 2.0 protected API.
package apirequest

import (
	"errors"

	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow type shared by Dex Web Start Flow, the Worker registry, and open runs.
	FlowType       = "OAuth2HTTPRequest"
	ConnectionName = "oauth2-http-api"
	// Application Step types match the Flow Definition Graph so Dex Web starts and displays the same Steps.
	sendRequestStepType    = "SendRequest"
	startRequestStepType   = "startRequest"
	recordResponseStepType = "recordResponse"
)

var reportAttribute = dex.DefineAttribute[Report]("oauth2-http-request-report")

type Status string

const (
	StatusRequesting Status = "requesting"
	StatusCompleted  Status = "completed"
)

// Input names the path to read, relative to the connection's apiBaseUrl, such as /users/42 or the
// authorization server's UserInfo path.
type Input struct {
	Path string `json:"path"`
}

// Report is the bounded record of the answer that the Flow returns.
type Report struct {
	Input           Input  `json:"input"`
	Status          Status `json:"status"`
	StatusCode      int    `json:"statusCode,omitempty"`
	ContentType     string `json:"contentType,omitempty"`
	Body            string `json:"body,omitempty"`
	IsBodyTruncated bool   `json:"bodyTruncated,omitempty"`
}

type Flow struct {
	dex.FlowDefaults
	connection oauth2http.Connection
}

func NewFlow(connection oauth2http.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType overrides the SDK's package-qualified default so that Dex Web starts the type this Worker
// registers.
func (*Flow) GetFlowType() string {
	return FlowType
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(oauth2http.NewSendRequestStep(oauth2http.SendRequestStepConfig[Input]{
			StepType: sendRequestStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "oauth2-http", GroupLabel: "OAuth 2.0 HTTP", Explanation: "Send a GET request to the connection's API with the OAuth access token."},
			Connection:  flow.connection,
			MapToOperationInput: func(input Input) oauth2http.SendRequestInput {
				return oauth2http.SendRequestInput{Method: "GET", Path: input.Path}
			},
			Completed: sdkgo.GoTo(recordResponse{}),
		})),
		dex.DefineStep(recordResponse{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{reportAttribute}}
}

// dex:field attribute-key:oauth2-http-request-report value-type:json editable:false description:"OAuth 2.0 HTTP request status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := optionalReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oauth2-http-request-report": report}}, nil
}

// dex:field attribute-key:oauth2-http-request-report value-type:json editable:false description:"OAuth 2.0 HTTP request report"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := optionalReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oauth2-http-request-report": report}}, nil
}

// optionalReport returns an empty report before the start Step records one.
func optionalReport(ctx dex.Context) (Report, error) {
	report, err := reportAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return Report{}, nil
	}
	return report, err
}

// dex:group group-id:report group-label:"Report"
// dex:explanation text:"Record the requested path before calling the API."
type startRequest struct {
	dex.StepDefaults
}

func (startRequest) GetStepType() string { return startRequestStepType }

// WaitFor skips at once; Dex Web Start Flow (Dex CLI v0.13.8) calls it, and execute-only start Steps
// reject that.
func (startRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := reportAttribute.Set(ctx, Report{Input: input, Status: StatusRequesting}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](sendRequestStepType), input), nil
}

// dex:group group-id:report group-label:"Report"
// dex:explanation text:"Record the API's 2xx answer and complete the report."
type recordResponse struct {
	dex.StepDefaultsNoWaitFor[oauth2http.SendRequestResult]
}

func (recordResponse) GetStepType() string { return recordResponseStepType }

func (recordResponse) Execute(ctx dex.Context, result oauth2http.SendRequestResult) (*dex.StepDecision, error) {
	report, err := recordAnswer(ctx, result)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(report), nil
}

// recordAnswer stores the connector Result in the report Attribute and returns the report.
func recordAnswer(ctx dex.Context, result oauth2http.SendRequestResult) (Report, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return Report{}, err
	}
	report.Status = StatusCompleted
	report.StatusCode = result.Value.StatusCode
	report.ContentType = result.Value.ContentType
	report.Body = result.Value.Body
	report.IsBodyTruncated = result.Value.IsBodyTruncated
	return report, reportAttribute.Set(ctx, report)
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
