// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package newhireonboarding demonstrates the BambooHR person record in one Flow started from Dex
// Web Start Flow: find a new hire by personal email or add them, read the record, check that it
// holds what IT needs to provision accounts, list time off in the first two weeks, and record the
// provisioning hand-off in a BambooHR custom field.
package newhireonboarding

import (
	"cmp"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "BambooHRNewHireOnboarding"
	// ConnectionName is the static Dex Web connection for BambooHR.
	ConnectionName = "bamboohr-company"

	recordNewHireStepType             = "RecordNewHire"
	findNewHireStepType               = "FindNewHire"
	adoptExistingEmployeeStepType     = "AdoptExistingEmployee"
	recordAmbiguousHireStepType       = "RecordAmbiguousHire"
	prepareNewHireRecordStepType      = "PrepareNewHireRecord"
	addNewHireStepType                = "AddNewHire"
	adoptAddedEmployeeStepType        = "AdoptAddedEmployee"
	recordUncertainHireStepType       = "RecordUncertainHire"
	reconcileUncertainHireStepType    = "ReconcileUncertainHire"
	adoptReconciledEmployeeStepType   = "AdoptReconciledEmployee"
	recordUnreconciledHireStepType    = "RecordUnreconciledHire"
	readNewHireRecordStepType         = "ReadNewHireRecord"
	checkOnboardingReadinessStepType  = "CheckOnboardingReadiness"
	listStartWindowTimeOffStepType    = "ListStartWindowTimeOff"
	decideProvisioningHandoffStepType = "DecideProvisioningHandoff"
	recordProvisioningHandoffStepType = "RecordProvisioningHandoff"
	completeOnboardingCheckStepType   = "CompleteOnboardingCheck"

	// startWindowDays is the length of the first-weeks window whose time off IT should know about.
	startWindowDays = 14
	// maxListedTimeOff bounds the time off the hand-off text names.
	maxListedTimeOff = 5
	timeOffPageSize  = 50

	reviewReasonAmbiguousHire      = "ambiguousHire"
	reviewReasonAdditionUncertain  = "additionUncertain"
	reviewReasonHandoffUnavailable = "handoffFieldUnavailable"
	reviewReasonHandoffUnconfirmed = "handoffUnconfirmed"
)

var (
	newHireAttribute           = dex.DefineAttribute[NewHire]("bamboohr-new-hire")
	onboardingOutcomeAttribute = dex.DefineAttribute[OnboardingOutcome]("bamboohr-onboarding-outcome")

	handoffFieldNamePattern = regexp.MustCompile(`^custom[A-Za-z0-9_]{1,57}$`)
)

// RequiredProvisioningFields are the BambooHR fields IT needs before it can provision accounts:
// where the hire works, in which role, and for which manager.
var RequiredProvisioningFields = []string{"department", "jobTitle", "location", "supervisorEId", "hireDate"}

// Input is the new hire entered in Dex Web Start Flow.
type Input struct {
	// FirstName is the hire's legal first name, used only when BambooHR has no record yet.
	FirstName string `json:"firstName"`
	// LastName is the hire's legal last name, used only when BambooHR has no record yet.
	LastName string `json:"lastName"`
	// PersonalEmail is the address the hire applied with, BambooHR's homeEmail, such as
	// ava.nguyen@personal.example.com. It identifies the hire before a work email exists.
	PersonalEmail string `json:"personalEmail"`
	// HireDate is the start date written YYYY-MM-DD, used only when BambooHR has no record yet.
	HireDate string `json:"hireDate"`
	// HandoffFieldName is the alias of the BambooHR custom text field that records the hand-off,
	// such as customITProvisioning.
	HandoffFieldName string `json:"handoffFieldName"`
}

// NewHire is the validated hire every later Step reads.
type NewHire struct {
	// FirstName is the legal first name.
	FirstName string `json:"firstName"`
	// LastName is the legal last name.
	LastName string `json:"lastName"`
	// PersonalEmail is the hire's homeEmail.
	PersonalEmail string `json:"personalEmail"`
	// HireDate is the start date written YYYY-MM-DD.
	HireDate string `json:"hireDate"`
	// HandoffFieldName is the custom field that records the hand-off.
	HandoffFieldName string `json:"handoffFieldName"`
}

// EmployeeRecordRequest names the employee record to read and the hand-off field to read with it.
type EmployeeRecordRequest struct {
	// EmployeeID is BambooHR's internal employee ID.
	EmployeeID string `json:"employeeId"`
	// HandoffFieldName is the custom field that records the hand-off.
	HandoffFieldName string `json:"handoffFieldName"`
}

// StartWindow is the hire's first two weeks, whose time off the hand-off names.
type StartWindow struct {
	// EmployeeID is BambooHR's internal employee ID.
	EmployeeID string `json:"employeeId"`
	// StartDate is the hire date, written YYYY-MM-DD.
	StartDate string `json:"startDate"`
	// EndDate is the last day of the window, written YYYY-MM-DD.
	EndDate string `json:"endDate"`
}

// HandoffRecord is the hand-off text to store in the custom field.
type HandoffRecord struct {
	// EmployeeID is BambooHR's internal employee ID.
	EmployeeID string `json:"employeeId"`
	// FieldName is the custom field that records the hand-off.
	FieldName string `json:"fieldName"`
	// Value is the hand-off text.
	Value string `json:"value"`
}

// StartWindowTimeOff is one approved or requested absence in the hire's first two weeks.
type StartWindowTimeOff struct {
	// RequestID is BambooHR's time off request ID.
	RequestID int64 `json:"requestId"`
	// StartDate is the first day off.
	StartDate string `json:"startDate"`
	// EndDate is the last day off.
	EndDate string `json:"endDate"`
	// Status is BambooHR's status, REQUESTED or APPROVED.
	Status bamboohr.TimeOffRequestStatus `json:"status"`
}

// OnboardingAction is what the Flow concluded about the hire.
type OnboardingAction string

const (
	// OnboardingHandedOff means the record was complete and the hand-off is stored in BambooHR.
	OnboardingHandedOff OnboardingAction = "handedOff"
	// OnboardingAlreadyHandedOff means the hand-off field already held a value, so nothing was written.
	OnboardingAlreadyHandedOff OnboardingAction = "alreadyHandedOff"
	// OnboardingHeld means fields IT needs are missing, so no hand-off was recorded.
	OnboardingHeld OnboardingAction = "held"
	// OnboardingNeedsReview means a person must check BambooHR; ReviewReason says why.
	OnboardingNeedsReview OnboardingAction = "needsReview"
)

// OnboardingOutcome is the Flow result and the value of its outcome Attribute.
type OnboardingOutcome struct {
	// Action is what the Flow concluded.
	Action OnboardingAction `json:"action,omitempty"`
	// EmployeeID is the hire's internal BambooHR employee ID, once known.
	EmployeeID string `json:"employeeId,omitempty"`
	// WasEmployeeAdded reports that this Flow added the hire to BambooHR.
	WasEmployeeAdded bool `json:"wasEmployeeAdded,omitempty"`
	// WasAdditionReconciled reports that an add with an unknown outcome was found by personal email.
	WasAdditionReconciled bool `json:"wasAdditionReconciled,omitempty"`
	// MissingFields lists required provisioning fields that are empty or not visible to the
	// connection, and status when the hire is not Active.
	MissingFields []string `json:"missingFields,omitempty"`
	// ProvisioningDetails holds the required provisioning fields as read from BambooHR.
	ProvisioningDetails map[string]string `json:"provisioningDetails,omitempty"`
	// StartWindowTimeOff is the approved or requested time off in the hire's first two weeks.
	StartWindowTimeOff []StartWindowTimeOff `json:"startWindowTimeOff,omitempty"`
	// HandoffValue is the hand-off text recorded, or found already recorded, in BambooHR.
	HandoffValue string `json:"handoffValue,omitempty"`
	// WasAlreadyApplied reports that a repeated update found the hand-off already stored.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// NeedsReview reports that a person must check BambooHR; the Flow never adds the hire twice.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewReason is ambiguousHire, additionUncertain, handoffFieldUnavailable, or handoffUnconfirmed.
	ReviewReason string `json:"reviewReason,omitempty"`
	// ReviewDetail is the connector's credential-free explanation or the candidate employee IDs;
	// after a reconciled add it keeps why the add's outcome was unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow checks one new hire's BambooHR record and records the provisioning hand-off.
type Flow struct {
	dex.FlowDefaults
	connection bamboohr.Connection
}

// NewFlow binds the BambooHR Connection at registration time.
func NewFlow(connection bamboohr.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and BambooHR connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordNewHire{}),
		dex.DefineStep(bamboohr.NewFindEmployeeByEmailStep(bamboohr.FindEmployeeByEmailStepConfig[NewHire]{
			StepType: findNewHireStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "bamboohr", GroupLabel: "BambooHR",
				Explanation: "Find the hire by personal email before adding anyone, so a re-run never adds a second record.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindEmployeeByEmailInput,
			Found:     sdkgo.GoTo(adoptExistingEmployee{}),
			NotFound:  sdkgo.GoTo(prepareNewHireRecord{}),
			Ambiguous: sdkgo.GoTo(recordAmbiguousHire{}),
		})),
		dex.DefineStep(adoptExistingEmployee{}),
		dex.DefineStep(recordAmbiguousHire{}),
		dex.DefineStep(prepareNewHireRecord{}),
		dex.DefineStep(bamboohr.NewAddEmployeeStep(bamboohr.AddEmployeeStepConfig[NewHire]{
			StepType: addNewHireStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "bamboohr", GroupLabel: "BambooHR",
				Explanation: "Add the hire with name, personal email, and hire date, sending the request at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddEmployeeInput,
			Created:   sdkgo.GoTo(adoptAddedEmployee{}),
			Uncertain: sdkgo.GoTo(recordUncertainHire{}),
		})),
		dex.DefineStep(adoptAddedEmployee{}),
		dex.DefineStep(recordUncertainHire{}),
		dex.DefineStep(bamboohr.NewFindEmployeeByEmailStep(bamboohr.FindEmployeeByEmailStepConfig[NewHire]{
			StepType: reconcileUncertainHireStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "reconciliation", GroupLabel: "Reconciliation",
				Explanation: "Look for the hire an unconfirmed add may have created instead of adding again.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindEmployeeByEmailInput,
			Found:     sdkgo.GoTo(adoptReconciledEmployee{}),
			NotFound:  sdkgo.GoTo(recordUnreconciledHire{}),
			Ambiguous: sdkgo.GoTo(recordUnreconciledHire{}),
		})),
		dex.DefineStep(adoptReconciledEmployee{}),
		dex.DefineStep(recordUnreconciledHire{}),
		dex.DefineStep(bamboohr.NewGetEmployeeStep(bamboohr.GetEmployeeStepConfig[EmployeeRecordRequest]{
			StepType: readNewHireRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "bamboohr", GroupLabel: "BambooHR",
				Explanation: "Read the fields IT needs and the hand-off field from the hire's record.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetEmployeeInput,
			Found: sdkgo.GoTo(checkOnboardingReadiness{}),
		})),
		dex.DefineStep(checkOnboardingReadiness{}),
		dex.DefineStep(bamboohr.NewListTimeOffRequestsStep(bamboohr.ListTimeOffRequestsStepConfig[StartWindow]{
			StepType: listStartWindowTimeOffStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "bamboohr", GroupLabel: "BambooHR",
				Explanation: "List approved or requested time off in the hire's first two weeks.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListTimeOffRequestsInput,
			Listed: sdkgo.GoTo(decideProvisioningHandoff{}),
		})),
		dex.DefineStep(decideProvisioningHandoff{}),
		dex.DefineStep(bamboohr.NewUpdateEmployeeStep(bamboohr.UpdateEmployeeStepConfig[HandoffRecord]{
			StepType: recordProvisioningHandoffStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "bamboohr", GroupLabel: "BambooHR",
				Explanation: "Store the hand-off text in the custom field; a repeated attempt writes nothing twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateEmployeeInput,
			Updated: sdkgo.GoTo(completeOnboardingCheck{}),
		})),
		dex.DefineStep(completeOnboardingCheck{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the hire and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{newHireAttribute, onboardingOutcomeAttribute}}
}

// GetDexSummary returns the new hire and the onboarding outcome.
//
// dex:field attribute-key:bamboohr-new-hire value-type:json editable:false description:"New hire"
// dex:field attribute-key:bamboohr-onboarding-outcome value-type:json editable:false description:"Onboarding outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	hire, outcome, err := onboardingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"bamboohr-new-hire":           hire,
		"bamboohr-onboarding-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the new hire and the hand-off the Flow recorded.
//
// dex:field attribute-key:bamboohr-new-hire value-type:json editable:false description:"Name, personal email, hire date, and hand-off field"
// dex:field attribute-key:bamboohr-onboarding-outcome value-type:json editable:false description:"Action, employee ID, missing fields, start-window time off, hand-off, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	hire, outcome, err := onboardingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"bamboohr-new-hire":           hire,
		"bamboohr-onboarding-outcome": outcome,
	}}, nil
}

// MapToFindEmployeeByEmailInput finds the hire by the personal email stored as homeEmail.
func MapToFindEmployeeByEmailInput(hire NewHire) bamboohr.FindEmployeeByEmailInput {
	return bamboohr.FindEmployeeByEmailInput{Email: hire.PersonalEmail, EmailField: bamboohr.EmployeeEmailFieldHome}
}

// MapToAddEmployeeInput adds the hire with the values HR entered at Start Flow.
func MapToAddEmployeeInput(hire NewHire) bamboohr.AddEmployeeInput {
	return bamboohr.AddEmployeeInput{FirstName: hire.FirstName, LastName: hire.LastName, HomeEmail: hire.PersonalEmail, HireDate: hire.HireDate}
}

// MapToGetEmployeeInput reads the provisioning fields, status, emails, and the hand-off field.
func MapToGetEmployeeInput(request EmployeeRecordRequest) bamboohr.GetEmployeeInput {
	fields := append([]string{"firstName", "lastName", "homeEmail", "workEmail", "status"}, RequiredProvisioningFields...)
	return bamboohr.GetEmployeeInput{EmployeeID: request.EmployeeID, Fields: append(fields, request.HandoffFieldName)}
}

// MapToListTimeOffRequestsInput lists approved or requested time off in the start window.
func MapToListTimeOffRequestsInput(window StartWindow) bamboohr.ListTimeOffRequestsInput {
	return bamboohr.ListTimeOffRequestsInput{
		StartDate: window.StartDate, EndDate: window.EndDate, EmployeeID: window.EmployeeID, PageSize: timeOffPageSize,
		Statuses: []bamboohr.TimeOffRequestStatus{bamboohr.TimeOffRequestStatusApproved, bamboohr.TimeOffRequestStatusRequested},
	}
}

// MapToUpdateEmployeeInput stores the hand-off text in the custom field.
func MapToUpdateEmployeeInput(record HandoffRecord) bamboohr.UpdateEmployeeInput {
	return bamboohr.UpdateEmployeeInput{EmployeeID: record.EmployeeID, Fields: map[string]string{record.FieldName: record.Value}}
}

// FindMissingProvisioningFields lists required fields that are empty or that BambooHR omitted
// because the connection cannot view them, plus status when the hire is not Active.
func FindMissingProvisioningFields(record bamboohr.EmployeeRecord) []string {
	var missing []string
	for _, field := range RequiredProvisioningFields {
		if value, isPresent := record.Value(field); !isPresent || strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	if status, _ := record.Value("status"); status != "Active" {
		missing = append(missing, "status")
	}
	return missing
}

// BuildStartWindow is the hire date and the following 13 days.
func BuildStartWindow(employeeID string, hireDate string) (StartWindow, error) {
	start, err := time.Parse(time.DateOnly, hireDate)
	if err != nil {
		return StartWindow{}, fmt.Errorf("hireDate %q is not YYYY-MM-DD", hireDate)
	}
	return StartWindow{EmployeeID: employeeID, StartDate: hireDate, EndDate: start.AddDate(0, 0, startWindowDays-1).Format(time.DateOnly)}, nil
}

// ProvisioningDetailsOf returns the required provisioning fields of a record, trimmed.
func ProvisioningDetailsOf(record bamboohr.EmployeeRecord) map[string]string {
	details := make(map[string]string, len(RequiredProvisioningFields))
	for _, field := range RequiredProvisioningFields {
		value, _ := record.Value(field)
		details[field] = strings.TrimSpace(value)
	}
	return details
}

// BuildHandoffValue is the hand-off text IT reads in BambooHR. It derives only from the recorded
// provisioning details and the listed time off, so every attempt writes the same text.
func BuildHandoffValue(details map[string]string, timeOff []StartWindowTimeOff) string {
	handoff := fmt.Sprintf("IT provisioning requested by Dex for a %s start: %s, %s, %s; manager employee %s; first-two-weeks time off: ",
		details["hireDate"], details["jobTitle"], details["department"], details["location"], details["supervisorEId"])
	if len(timeOff) == 0 {
		return handoff + "none"
	}
	var absences []string
	for _, absence := range timeOff[:min(len(timeOff), maxListedTimeOff)] {
		absences = append(absences, fmt.Sprintf("%s to %s (%s)", absence.StartDate, absence.EndDate, absence.Status))
	}
	if len(timeOff) > maxListedTimeOff {
		absences = append(absences, fmt.Sprintf("and %d more", len(timeOff)-maxListedTimeOff))
	}
	return handoff + strings.Join(absences, ", ")
}

// BuildNewHire validates Start Flow input so no connector Step receives an unusable hire.
func BuildNewHire(input Input) (NewHire, error) {
	hire := NewHire{
		FirstName: strings.TrimSpace(input.FirstName), LastName: strings.TrimSpace(input.LastName),
		PersonalEmail: strings.TrimSpace(input.PersonalEmail), HireDate: strings.TrimSpace(input.HireDate),
		HandoffFieldName: strings.TrimSpace(input.HandoffFieldName),
	}
	if hire.FirstName == "" || hire.LastName == "" {
		return NewHire{}, errors.New("firstName and lastName are required")
	}
	address, err := mail.ParseAddress(hire.PersonalEmail)
	if err != nil || address.Name != "" || address.Address != hire.PersonalEmail {
		return NewHire{}, fmt.Errorf("personalEmail %q must be one bare email address", input.PersonalEmail)
	}
	if _, err := BuildStartWindow("1", hire.HireDate); err != nil {
		return NewHire{}, err
	}
	if !handoffFieldNamePattern.MatchString(hire.HandoffFieldName) {
		return NewHire{}, errors.New("handoffFieldName must be a BambooHR custom-field alias such as customITProvisioning")
	}
	return hire, nil
}

func onboardingInspection(ctx dex.Context) (NewHire, OnboardingOutcome, error) {
	hire, err := optionalAttribute(ctx, newHireAttribute)
	if err != nil {
		return NewHire{}, OnboardingOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, onboardingOutcomeAttribute)
	if err != nil {
		return NewHire{}, OnboardingOutcome{}, err
	}
	return hire, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	if isAttributeNotFound(err) {
		var zero T
		return zero, nil
	}
	return value, err
}

func isAttributeNotFound(err error) bool {
	var missingAttribute *dex.AttributeNotFoundError
	return errors.As(err, &missingAttribute)
}

func failureMessage(failure *sdkgo.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

func matchedEmployeeIDs(result bamboohr.FindEmployeeByEmailResult) string {
	var employeeIDs []string
	for _, match := range result.Value.Matches {
		employeeIDs = append(employeeIDs, match.EmployeeID)
	}
	return fmt.Sprintf("%d employees contain this personal email; exact matches: [%s]", result.Value.CandidateCount, strings.Join(employeeIDs, ", "))
}

// dex:group group-id:hire group-label:"New hire"
// dex:explanation text:"Validate the new hire and record it before calling BambooHR."
type recordNewHire struct {
	dex.StepDefaults
}

func (recordNewHire) GetStepType() string { return recordNewHireStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordNewHire) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordNewHire) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	hire, err := BuildNewHire(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := newHireAttribute.Set(ctx, hire); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[NewHire](findNewHireStepType), hire), nil
}

// dex:group group-id:hire group-label:"New hire"
// dex:explanation text:"Use the existing BambooHR employee that has this personal email."
type adoptExistingEmployee struct {
	dex.StepDefaultsNoWaitFor[bamboohr.FindEmployeeByEmailResult]
}

func (adoptExistingEmployee) GetStepType() string { return adoptExistingEmployeeStepType }

func (adoptExistingEmployee) Execute(ctx dex.Context, result bamboohr.FindEmployeeByEmailResult) (*dex.StepDecision, error) {
	hire, err := newHireAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	employeeID := result.Value.Employee.EmployeeID
	if err := onboardingOutcomeAttribute.Set(ctx, OnboardingOutcome{EmployeeID: employeeID}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[EmployeeRecordRequest](readNewHireRecordStepType),
		EmployeeRecordRequest{EmployeeID: employeeID, HandoffFieldName: hire.HandoffFieldName}), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"Several employees share this personal email; complete for a person to pick one instead of guessing."
type recordAmbiguousHire struct {
	dex.StepDefaultsNoWaitFor[bamboohr.FindEmployeeByEmailResult]
}

func (recordAmbiguousHire) GetStepType() string { return recordAmbiguousHireStepType }

func (recordAmbiguousHire) Execute(ctx dex.Context, result bamboohr.FindEmployeeByEmailResult) (*dex.StepDecision, error) {
	outcome := OnboardingOutcome{
		Action: OnboardingNeedsReview, NeedsReview: true, ReviewReason: reviewReasonAmbiguousHire, ReviewDetail: matchedEmployeeIDs(result),
	}
	if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:hire group-label:"New hire"
// dex:explanation text:"No employee has this personal email, so prepare the hire's BambooHR record."
type prepareNewHireRecord struct {
	dex.StepDefaultsNoWaitFor[bamboohr.FindEmployeeByEmailResult]
}

func (prepareNewHireRecord) GetStepType() string { return prepareNewHireRecordStepType }

func (prepareNewHireRecord) Execute(ctx dex.Context, _ bamboohr.FindEmployeeByEmailResult) (*dex.StepDecision, error) {
	hire, err := newHireAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[NewHire](addNewHireStepType), hire), nil
}

// dex:group group-id:hire group-label:"New hire"
// dex:explanation text:"Record the employee BambooHR added and read the new record."
type adoptAddedEmployee struct {
	dex.StepDefaultsNoWaitFor[bamboohr.AddEmployeeResult]
}

func (adoptAddedEmployee) GetStepType() string { return adoptAddedEmployeeStepType }

func (adoptAddedEmployee) Execute(ctx dex.Context, result bamboohr.AddEmployeeResult) (*dex.StepDecision, error) {
	hire, err := newHireAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	employeeID := result.Value.EmployeeID
	if err := onboardingOutcomeAttribute.Set(ctx, OnboardingOutcome{EmployeeID: employeeID, WasEmployeeAdded: true}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[EmployeeRecordRequest](readNewHireRecordStepType),
		EmployeeRecordRequest{EmployeeID: employeeID, HandoffFieldName: hire.HandoffFieldName}), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"The add was sent with an unknown outcome; record why and look the hire up instead of adding again."
type recordUncertainHire struct {
	dex.StepDefaultsNoWaitFor[bamboohr.AddEmployeeResult]
}

func (recordUncertainHire) GetStepType() string { return recordUncertainHireStepType }

func (recordUncertainHire) Execute(ctx dex.Context, result bamboohr.AddEmployeeResult) (*dex.StepDecision, error) {
	hire, err := newHireAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := OnboardingOutcome{ReviewReason: reviewReasonAdditionUncertain, ReviewDetail: failureMessage(result.Failure)}
	if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[NewHire](reconcileUncertainHireStepType), hire), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"The unconfirmed add did create the hire; continue with that employee."
type adoptReconciledEmployee struct {
	dex.StepDefaultsNoWaitFor[bamboohr.FindEmployeeByEmailResult]
}

func (adoptReconciledEmployee) GetStepType() string { return adoptReconciledEmployeeStepType }

func (adoptReconciledEmployee) Execute(ctx dex.Context, result bamboohr.FindEmployeeByEmailResult) (*dex.StepDecision, error) {
	hire, err := newHireAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := onboardingOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	employeeID := result.Value.Employee.EmployeeID
	// ReviewDetail keeps why the add was unconfirmed; the reconciled hire itself needs no review.
	outcome.EmployeeID, outcome.WasEmployeeAdded, outcome.WasAdditionReconciled, outcome.ReviewReason = employeeID, true, true, ""
	if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[EmployeeRecordRequest](readNewHireRecordStepType),
		EmployeeRecordRequest{EmployeeID: employeeID, HandoffFieldName: hire.HandoffFieldName}), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The unconfirmed add cannot be matched to one employee; complete for a person to check BambooHR instead of adding again."
type recordUnreconciledHire struct {
	dex.StepDefaultsNoWaitFor[bamboohr.FindEmployeeByEmailResult]
}

func (recordUnreconciledHire) GetStepType() string { return recordUnreconciledHireStepType }

func (recordUnreconciledHire) Execute(ctx dex.Context, _ bamboohr.FindEmployeeByEmailResult) (*dex.StepDecision, error) {
	outcome, err := onboardingOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.NeedsReview = OnboardingNeedsReview, true
	if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:readiness group-label:"Provisioning readiness"
// dex:explanation text:"Stop when the hand-off is already recorded or fields IT needs are missing; otherwise list start-window time off."
type checkOnboardingReadiness struct {
	dex.StepDefaultsNoWaitFor[bamboohr.GetEmployeeResult]
}

func (checkOnboardingReadiness) GetStepType() string { return checkOnboardingReadinessStepType }

func (checkOnboardingReadiness) Execute(ctx dex.Context, result bamboohr.GetEmployeeResult) (*dex.StepDecision, error) {
	hire, err := newHireAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := onboardingOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record := result.Value
	handoff, isHandoffVisible := record.Value(hire.HandoffFieldName)
	switch missing := FindMissingProvisioningFields(record); {
	case !isHandoffVisible:
		outcome.Action, outcome.NeedsReview, outcome.ReviewReason = OnboardingNeedsReview, true, reviewReasonHandoffUnavailable
		outcome.ReviewDetail = "BambooHR did not return " + hire.HandoffFieldName + "; the field does not exist or the connection cannot see it"
	case strings.TrimSpace(handoff) != "":
		outcome.Action, outcome.HandoffValue = OnboardingAlreadyHandedOff, handoff
	case len(missing) != 0:
		outcome.Action, outcome.MissingFields = OnboardingHeld, missing
	default:
		details := ProvisioningDetailsOf(record)
		window, err := BuildStartWindow(record.EmployeeID, details["hireDate"])
		if err != nil {
			outcome.Action, outcome.MissingFields = OnboardingHeld, []string{"hireDate"}
			break
		}
		outcome.ProvisioningDetails = details
		if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[StartWindow](listStartWindowTimeOffStepType), window), nil
	}
	if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:readiness group-label:"Provisioning readiness"
// dex:explanation text:"Compose the hand-off text from the record and start-window time off."
type decideProvisioningHandoff struct {
	dex.StepDefaultsNoWaitFor[bamboohr.ListTimeOffRequestsResult]
}

func (decideProvisioningHandoff) GetStepType() string { return decideProvisioningHandoffStepType }

func (decideProvisioningHandoff) Execute(ctx dex.Context, result bamboohr.ListTimeOffRequestsResult) (*dex.StepDecision, error) {
	hire, err := newHireAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := onboardingOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	timeOff := make([]StartWindowTimeOff, 0, len(result.Value.Requests))
	for _, request := range result.Value.Requests {
		timeOff = append(timeOff, StartWindowTimeOff{RequestID: request.ID, StartDate: request.StartDate, EndDate: request.EndDate, Status: request.Status})
	}
	slices.SortFunc(timeOff, func(left StartWindowTimeOff, right StartWindowTimeOff) int {
		return cmp.Or(strings.Compare(left.StartDate, right.StartDate), cmp.Compare(left.RequestID, right.RequestID))
	})
	outcome.StartWindowTimeOff = timeOff
	outcome.HandoffValue = BuildHandoffValue(outcome.ProvisioningDetails, timeOff)
	if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[HandoffRecord](recordProvisioningHandoffStepType),
		HandoffRecord{EmployeeID: outcome.EmployeeID, FieldName: hire.HandoffFieldName, Value: outcome.HandoffValue}), nil
}

// dex:group group-id:readiness group-label:"Provisioning readiness"
// dex:explanation text:"Complete as handed off once BambooHR stores the text, or for review when it stored something else."
type completeOnboardingCheck struct {
	dex.StepDefaultsNoWaitFor[bamboohr.UpdateEmployeeResult]
}

func (completeOnboardingCheck) GetStepType() string { return completeOnboardingCheckStepType }

func (completeOnboardingCheck) Execute(ctx dex.Context, result bamboohr.UpdateEmployeeResult) (*dex.StepDecision, error) {
	outcome, err := onboardingOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.WasAlreadyApplied = result.Value.WasAlreadyApplied
	if len(result.Value.UnmatchedFields) != 0 {
		outcome.Action, outcome.NeedsReview, outcome.ReviewReason = OnboardingNeedsReview, true, reviewReasonHandoffUnconfirmed
		outcome.ReviewDetail = "BambooHR stored a different value for " + strings.Join(result.Value.UnmatchedFields, ", ")
	} else {
		outcome.Action = OnboardingHandedOff
	}
	if err := onboardingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
