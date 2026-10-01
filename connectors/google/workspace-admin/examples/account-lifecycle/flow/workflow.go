// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountlifecycle onboards, reinstates, or offboards one Google
// Workspace account from Dex Web Start Flow: onboarding creates the account
// and adds it to a team group, reinstating restores a returning person's
// account instead of creating a second one, offboarding suspends the account
// and removes the group membership, and every path records a hand-off.
package accountlifecycle

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "GoogleWorkspaceAccountLifecycle"
	// ConnectionName is the static Dex Web connection for Google Workspace Admin.
	ConnectionName = "google-workspace-admin"

	recordAccountRequestStepType       = "RecordAccountRequest"
	createWorkspaceAccountStepType     = "CreateWorkspaceAccount"
	recordCreatedAccountStepType       = "RecordCreatedAccount"
	recordAddressTakenHandOffStepType  = "RecordAddressTakenHandOff"
	reinstateWorkspaceAccountStepType  = "ReinstateWorkspaceAccount"
	recordReinstatedAccountStepType    = "RecordReinstatedAccount"
	addAccountToTeamGroupStepType      = "AddAccountToTeamGroup"
	readBackWorkspaceAccountStepType   = "ReadBackWorkspaceAccount"
	recordOnboardingHandOffStepType    = "RecordOnboardingHandOff"
	suspendWorkspaceAccountStepType    = "SuspendWorkspaceAccount"
	recordSuspendedAccountStepType     = "RecordSuspendedAccount"
	removeAccountFromTeamGroupStepType = "RemoveAccountFromTeamGroup"
	recordOffboardingHandOffStepType   = "RecordOffboardingHandOff"

	newAccountSignInHandOff = "The connector set a random password that nobody received. Sign in through your SSO provider, " +
		"or have an administrator open Admin console > Directory > Users > Reset password and deliver the new password out of band."
	reinstatedAccountSignInHandOff = "The account's existing password and sign-in methods are unchanged. " +
		"If the person no longer knows the password, an administrator resets it in Admin console > Directory > Users > Reset password."
	offboardedAccountHandOff = "Sign-in is blocked and the account's data is kept. Transfer its files and delete or archive the account " +
		"in Admin console > Directory > Users when your retention policy allows."
	addressTakenHandOff = "The address already belongs to another account, alias, or group, so nothing was created. " +
		"Confirm with IT whether it is this person's account; if it is, start the reinstate action, otherwise choose another address."
)

var (
	accountRequestAttribute  = dex.DefineAttribute[AccountRequest]("google-workspace-account-request")
	accountCreationAttribute = dex.DefineAttribute[workspaceadmin.CreateUserResult]("google-workspace-account-creation")
	accountAttribute         = dex.DefineAttribute[workspaceadmin.User]("google-workspace-account")
	groupMembershipAttribute = dex.DefineAttribute[workspaceadmin.AddUserToGroupResult]("google-workspace-group-membership")
	accountHandOffAttribute  = dex.DefineAttribute[AccountHandOff]("google-workspace-account-hand-off")
)

// AccountAction selects the lifecycle path.
type AccountAction string

const (
	// AccountActionOnboard creates the account and adds it to the team group.
	AccountActionOnboard AccountAction = "onboard"
	// AccountActionReinstate restores a returning person's suspended account and adds it to the team group.
	AccountActionReinstate AccountAction = "reinstate"
	// AccountActionOffboard suspends the account and removes it from the team group.
	AccountActionOffboard AccountAction = "offboard"
)

// Input is the request entered in Dex Web Start Flow.
type Input struct {
	// Action is onboard, reinstate, or offboard.
	Action AccountAction `json:"action"`
	// PrimaryEmail is the account's address, such as ada.lovelace@example.com.
	PrimaryEmail string `json:"primaryEmail"`
	// GivenName is the first name; onboarding requires it.
	GivenName string `json:"givenName,omitempty"`
	// FamilyName is the last name; onboarding requires it.
	FamilyName string `json:"familyName,omitempty"`
	// OrgUnitPath places a new account, such as /Engineering; blank uses the top-level unit.
	OrgUnitPath string `json:"orgUnitPath,omitempty"`
	// RecoveryEmail is an optional personal address for account recovery when onboarding.
	RecoveryEmail string `json:"recoveryEmail,omitempty"`
	// GroupEmail is the team group the account joins or leaves, such as engineering@example.com.
	GroupEmail string `json:"groupEmail"`
	// GroupRole is MEMBER, MANAGER, or OWNER when joining; blank joins as MEMBER.
	GroupRole workspaceadmin.GroupRole `json:"groupRole,omitempty"`
	// ProvisioningKey is an optional HR hire ID that keeps one account per hire across Flow runs.
	ProvisioningKey string `json:"provisioningKey,omitempty"`
}

// AccountRequest is the validated request every later Step reads.
type AccountRequest struct {
	// Action is the lifecycle path.
	Action AccountAction `json:"action"`
	// PrimaryEmail is the account's address.
	PrimaryEmail string `json:"primaryEmail"`
	// GivenName is the first name for onboarding.
	GivenName string `json:"givenName,omitempty"`
	// FamilyName is the last name for onboarding.
	FamilyName string `json:"familyName,omitempty"`
	// OrgUnitPath is the organizational unit for onboarding.
	OrgUnitPath string `json:"orgUnitPath,omitempty"`
	// RecoveryEmail is the optional recovery address for onboarding.
	RecoveryEmail string `json:"recoveryEmail,omitempty"`
	// GroupEmail is the team group.
	GroupEmail string `json:"groupEmail"`
	// GroupRole is the role when joining.
	GroupRole workspaceadmin.GroupRole `json:"groupRole"`
	// ProvisioningKey is the optional HR hire ID.
	ProvisioningKey string `json:"provisioningKey,omitempty"`
}

// TeamGroupMembership is the membership to add after the account exists.
type TeamGroupMembership struct {
	// GroupEmail is the team group.
	GroupEmail string `json:"groupEmail"`
	// MemberEmail is the account's primary address.
	MemberEmail string `json:"memberEmail"`
	// Role is the role to join with.
	Role workspaceadmin.GroupRole `json:"role"`
}

// TeamGroupRemoval is the membership to remove after the account is suspended.
type TeamGroupRemoval struct {
	// GroupEmail is the team group.
	GroupEmail string `json:"groupEmail"`
	// MemberID is the suspended account's unique user ID.
	MemberID string `json:"memberId"`
}

// HandOffStatus is the Flow's terminal business outcome.
type HandOffStatus string

const (
	// HandOffOnboarded means the account exists, is active, and is in the team group.
	HandOffOnboarded HandOffStatus = "onboarded"
	// HandOffReinstated means the existing account is active again and is in the team group.
	HandOffReinstated HandOffStatus = "reinstated"
	// HandOffOffboarded means the account is suspended and no longer a direct team group member.
	HandOffOffboarded HandOffStatus = "offboarded"
	// HandOffAddressTaken means the address belongs to something else, so nothing was created.
	HandOffAddressTaken HandOffStatus = "addressTaken"
)

// AccountHandOff is the Flow result and the record the next owner acts on.
type AccountHandOff struct {
	// Status is the terminal business outcome.
	Status HandOffStatus `json:"status"`
	// PrimaryEmail is the requested address.
	PrimaryEmail string `json:"primaryEmail"`
	// UserID is the account's unique user ID; for addressTaken it is the conflicting account's ID, when Google returns one.
	UserID string `json:"userId,omitempty"`
	// OrgUnitPath is the account's organizational unit as Google reports it.
	OrgUnitPath string `json:"orgUnitPath,omitempty"`
	// IsSuspended reports the account's final suspension state.
	IsSuspended bool `json:"isSuspended"`
	// GroupEmail is the team group.
	GroupEmail string `json:"groupEmail"`
	// GroupRole is the account's role in the group after onboarding or reinstating.
	GroupRole workspaceadmin.GroupRole `json:"groupRole,omitempty"`
	// WasAlreadyCreated reports that the account already carried this hire's provisioning key.
	WasAlreadyCreated bool `json:"wasAlreadyCreated,omitempty"`
	// WasAlreadyMember reports that the account was already a direct group member.
	WasAlreadyMember bool `json:"wasAlreadyMember,omitempty"`
	// WasAlreadyRemoved reports that the account was not a direct group member when offboarding ran.
	WasAlreadyRemoved bool `json:"wasAlreadyRemoved,omitempty"`
	// NextStep tells the human owner what this Flow did not do.
	NextStep string `json:"nextStep"`
}

// Flow runs one account lifecycle action.
type Flow struct {
	dex.FlowDefaults
	connection workspaceadmin.Connection
}

// NewFlow binds the Google Workspace Admin Connection at registration time.
func NewFlow(connection workspaceadmin.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Google Workspace Admin connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordAccountRequest{}),
		dex.DefineStep(workspaceadmin.NewCreateUserStep(workspaceadmin.CreateUserStepConfig[AccountRequest]{
			StepType: createWorkspaceAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-workspace", GroupLabel: "Google Workspace",
				Explanation: "Create the account with a password nobody receives, tagged so a repeated Step finds it instead of creating a second one.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateUserInput,
			ResultAttribute: &accountCreationAttribute,
			Created:         sdkgo.GoTo(recordCreatedAccount{}),
			AlreadyExists:   sdkgo.GoTo(recordAddressTakenHandOff{}),
		})),
		dex.DefineStep(recordCreatedAccount{}),
		dex.DefineStep(recordAddressTakenHandOff{}),
		dex.DefineStep(workspaceadmin.NewUnsuspendUserStep(workspaceadmin.UnsuspendUserStepConfig[AccountRequest]{
			StepType: reinstateWorkspaceAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-workspace", GroupLabel: "Google Workspace",
				Explanation: "Restore sign-in for the returning person's existing account instead of creating another.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUnsuspendUserInput,
			Unsuspended: sdkgo.GoTo(recordReinstatedAccount{}),
		})),
		dex.DefineStep(recordReinstatedAccount{}),
		dex.DefineStep(workspaceadmin.NewAddUserToGroupStep(workspaceadmin.AddUserToGroupStepConfig[TeamGroupMembership]{
			StepType: addAccountToTeamGroupStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-workspace", GroupLabel: "Google Workspace",
				Explanation: "Add the account to the team group; an existing membership is read back, not duplicated.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddUserToGroupInput,
			ResultAttribute: &groupMembershipAttribute,
			Added:           sdkgo.GoTo(sdkgo.StepRef[workspaceadmin.AddUserToGroupResult](readBackWorkspaceAccountStepType)),
		})),
		dex.DefineStep(workspaceadmin.NewGetUserStep(workspaceadmin.GetUserStepConfig[workspaceadmin.AddUserToGroupResult]{
			StepType: readBackWorkspaceAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-workspace", GroupLabel: "Google Workspace",
				Explanation: "Read the account back by its unique user ID to confirm it is active before the hand-off.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetUserInput,
			Found: sdkgo.GoTo(recordOnboardingHandOff{}),
		})),
		dex.DefineStep(recordOnboardingHandOff{}),
		dex.DefineStep(workspaceadmin.NewSuspendUserStep(workspaceadmin.SuspendUserStepConfig[AccountRequest]{
			StepType: suspendWorkspaceAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-workspace", GroupLabel: "Google Workspace",
				Explanation: "Suspend the departing account so it cannot sign in while its data is kept.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSuspendUserInput,
			Suspended: sdkgo.GoTo(recordSuspendedAccount{}),
		})),
		dex.DefineStep(recordSuspendedAccount{}),
		dex.DefineStep(workspaceadmin.NewRemoveUserFromGroupStep(workspaceadmin.RemoveUserFromGroupStepConfig[TeamGroupRemoval]{
			StepType: removeAccountFromTeamGroupStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-workspace", GroupLabel: "Google Workspace",
				Explanation: "Remove the suspended account from the team group; a membership already gone counts as removed.",
			},
			Connection: flow.connection, MapToOperationInput: MapToRemoveUserFromGroupInput,
			Removed: sdkgo.GoTo(recordOffboardingHandOff{}),
		})),
		dex.DefineStep(recordOffboardingHandOff{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, creation, account, membership, and hand-off Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		accountRequestAttribute, accountCreationAttribute, accountAttribute, groupMembershipAttribute, accountHandOffAttribute,
	}}
}

// GetDexSummary returns the request and hand-off.
//
// dex:field attribute-key:google-workspace-account-request value-type:json editable:false description:"Requested lifecycle action"
// dex:field attribute-key:google-workspace-account-hand-off value-type:json editable:false description:"Hand-off for the next owner"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := optionalAttribute(ctx, accountRequestAttribute)
	if err != nil {
		return nil, err
	}
	handOff, err := optionalAttribute(ctx, accountHandOffAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-workspace-account-request":  request,
		"google-workspace-account-hand-off": handOff,
	}}, nil
}

// GetDexDisplay returns the request, account, membership, and hand-off.
//
// dex:field attribute-key:google-workspace-account-request value-type:json editable:false description:"Action, address, organizational unit, and group"
// dex:field attribute-key:google-workspace-account value-type:json editable:false description:"Account as Google last returned it"
// dex:field attribute-key:google-workspace-group-membership value-type:object editable:false description:"Team group membership result"
// dex:field attribute-key:google-workspace-account-hand-off value-type:json editable:false description:"Status and what the next owner must do"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := optionalAttribute(ctx, accountRequestAttribute)
	if err != nil {
		return nil, err
	}
	account, err := optionalAttribute(ctx, accountAttribute)
	if err != nil {
		return nil, err
	}
	membership, err := optionalAttribute(ctx, groupMembershipAttribute)
	if err != nil {
		return nil, err
	}
	handOff, err := optionalAttribute(ctx, accountHandOffAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-workspace-account-request":  request,
		"google-workspace-account":          account,
		"google-workspace-group-membership": membership,
		"google-workspace-account-hand-off": handOff,
	}}, nil
}

// MapToCreateUserInput creates the account in the requested organizational unit with the optional hire ID as its key.
func MapToCreateUserInput(request AccountRequest) workspaceadmin.CreateUserInput {
	return workspaceadmin.CreateUserInput{
		PrimaryEmail: request.PrimaryEmail, GivenName: request.GivenName, FamilyName: request.FamilyName,
		OrgUnitPath: request.OrgUnitPath, RecoveryEmail: request.RecoveryEmail, ProvisioningKey: request.ProvisioningKey,
	}
}

// MapToUnsuspendUserInput restores the requested address.
func MapToUnsuspendUserInput(request AccountRequest) workspaceadmin.UnsuspendUserInput {
	return workspaceadmin.UnsuspendUserInput{UserKey: request.PrimaryEmail}
}

// MapToAddUserToGroupInput adds the account to the team group with the requested role.
func MapToAddUserToGroupInput(membership TeamGroupMembership) workspaceadmin.AddUserToGroupInput {
	return workspaceadmin.AddUserToGroupInput{GroupKey: membership.GroupEmail, MemberEmail: membership.MemberEmail, Role: membership.Role}
}

// MapToGetUserInput reads the member back by the unique ID Google returned for the membership.
func MapToGetUserInput(result workspaceadmin.AddUserToGroupResult) workspaceadmin.GetUserInput {
	return workspaceadmin.GetUserInput{UserKey: result.Value.Membership.MemberID}
}

// MapToSuspendUserInput suspends the requested address.
func MapToSuspendUserInput(request AccountRequest) workspaceadmin.SuspendUserInput {
	return workspaceadmin.SuspendUserInput{UserKey: request.PrimaryEmail}
}

// MapToRemoveUserFromGroupInput removes the suspended account by its unique ID, which survives a rename.
func MapToRemoveUserFromGroupInput(removal TeamGroupRemoval) workspaceadmin.RemoveUserFromGroupInput {
	return workspaceadmin.RemoveUserFromGroupInput{GroupKey: removal.GroupEmail, MemberKey: removal.MemberID}
}

// BuildAccountRequest validates Start Flow input before any provider call.
func BuildAccountRequest(input Input) (AccountRequest, error) {
	request := AccountRequest{
		Action: input.Action, PrimaryEmail: strings.ToLower(strings.TrimSpace(input.PrimaryEmail)),
		GivenName: strings.TrimSpace(input.GivenName), FamilyName: strings.TrimSpace(input.FamilyName),
		OrgUnitPath: strings.TrimSpace(input.OrgUnitPath), RecoveryEmail: strings.TrimSpace(input.RecoveryEmail),
		GroupEmail: strings.ToLower(strings.TrimSpace(input.GroupEmail)), GroupRole: input.GroupRole,
		ProvisioningKey: strings.TrimSpace(input.ProvisioningKey),
	}
	if request.GroupRole == "" {
		request.GroupRole = workspaceadmin.GroupRoleMember
	}
	if err := requireBareAddress("primaryEmail", request.PrimaryEmail); err != nil {
		return AccountRequest{}, err
	}
	if err := requireBareAddress("groupEmail", request.GroupEmail); err != nil {
		return AccountRequest{}, err
	}
	switch request.Action {
	case AccountActionOnboard:
		if request.GivenName == "" || request.FamilyName == "" {
			return AccountRequest{}, errors.New("onboarding requires givenName and familyName")
		}
	case AccountActionReinstate, AccountActionOffboard:
	default:
		return AccountRequest{}, fmt.Errorf("action must be %s, %s, or %s", AccountActionOnboard, AccountActionReinstate, AccountActionOffboard)
	}
	return request, nil
}

func requireBareAddress(field string, value string) error {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return fmt.Errorf("%s must be one bare email address", field)
	}
	return nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Validate the request and record it, then route to onboarding, reinstating, or offboarding."
type recordAccountRequest struct {
	dex.StepDefaults
}

func (recordAccountRequest) GetStepType() string { return recordAccountRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordAccountRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordAccountRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildAccountRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := accountRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	switch request.Action {
	case AccountActionOnboard:
		return dex.GoTo(sdkgo.StepRef[AccountRequest](createWorkspaceAccountStepType), request), nil
	case AccountActionReinstate:
		return dex.GoTo(sdkgo.StepRef[AccountRequest](reinstateWorkspaceAccountStepType), request), nil
	default:
		return dex.GoTo(sdkgo.StepRef[AccountRequest](suspendWorkspaceAccountStepType), request), nil
	}
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the created account, then add it to the team group."
type recordCreatedAccount struct {
	dex.StepDefaultsNoWaitFor[workspaceadmin.CreateUserResult]
}

func (recordCreatedAccount) GetStepType() string { return recordCreatedAccountStepType }

func (recordCreatedAccount) Execute(ctx dex.Context, result workspaceadmin.CreateUserResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := accountAttribute.Set(ctx, result.Value.User); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TeamGroupMembership](addAccountToTeamGroupStepType), BuildTeamGroupMembership(request, result.Value.User)), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the reinstated account, then add it to the team group."
type recordReinstatedAccount struct {
	dex.StepDefaultsNoWaitFor[workspaceadmin.UnsuspendUserResult]
}

func (recordReinstatedAccount) GetStepType() string { return recordReinstatedAccountStepType }

func (recordReinstatedAccount) Execute(ctx dex.Context, result workspaceadmin.UnsuspendUserResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := accountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TeamGroupMembership](addAccountToTeamGroupStepType), BuildTeamGroupMembership(request, result.Value)), nil
}

// BuildTeamGroupMembership joins the account to the requested group by its primary address.
func BuildTeamGroupMembership(request AccountRequest, account workspaceadmin.User) TeamGroupMembership {
	return TeamGroupMembership{GroupEmail: request.GroupEmail, MemberEmail: account.PrimaryEmail, Role: request.GroupRole}
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Confirm the read-back account is the recorded one and active, then record the onboarding hand-off."
type recordOnboardingHandOff struct {
	dex.StepDefaultsNoWaitFor[workspaceadmin.GetUserResult]
}

func (recordOnboardingHandOff) GetStepType() string { return recordOnboardingHandOffStepType }

func (recordOnboardingHandOff) Execute(ctx dex.Context, result workspaceadmin.GetUserResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	recorded, err := accountAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	membership, err := groupMembershipAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	account := result.Value
	if account.ID != recorded.ID || account.IsSuspended {
		return dex.ForceFail(fmt.Sprintf("read-back account %s is not the active account %s", account.ID, recorded.ID)), nil
	}
	if err := accountAttribute.Set(ctx, account); err != nil {
		return nil, err
	}
	handOff := AccountHandOff{
		Status: HandOffReinstated, PrimaryEmail: account.PrimaryEmail, UserID: account.ID, OrgUnitPath: account.OrgUnitPath,
		GroupEmail: request.GroupEmail, GroupRole: membership.Value.Membership.Role,
		WasAlreadyMember: membership.Value.WasAlreadyMember, NextStep: reinstatedAccountSignInHandOff,
	}
	if request.Action == AccountActionOnboard {
		creation, err := accountCreationAttribute.Get(ctx)
		if err != nil {
			return nil, err
		}
		handOff.Status, handOff.NextStep = HandOffOnboarded, newAccountSignInHandOff
		handOff.WasAlreadyCreated = creation.Value.WasAlreadyCreated
	}
	if err := accountHandOffAttribute.Set(ctx, handOff); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(handOff), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record that the address belongs to something else and hand the decision to IT without creating anything."
type recordAddressTakenHandOff struct {
	dex.StepDefaultsNoWaitFor[workspaceadmin.CreateUserResult]
}

func (recordAddressTakenHandOff) GetStepType() string { return recordAddressTakenHandOffStepType }

func (recordAddressTakenHandOff) Execute(ctx dex.Context, result workspaceadmin.CreateUserResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	existing := result.Value.User
	handOff := AccountHandOff{
		Status: HandOffAddressTaken, PrimaryEmail: request.PrimaryEmail, UserID: existing.ID, OrgUnitPath: existing.OrgUnitPath,
		IsSuspended: existing.IsSuspended, GroupEmail: request.GroupEmail, NextStep: addressTakenHandOff,
	}
	if err := accountHandOffAttribute.Set(ctx, handOff); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(handOff), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the suspended account, then remove it from the team group by its unique ID."
type recordSuspendedAccount struct {
	dex.StepDefaultsNoWaitFor[workspaceadmin.SuspendUserResult]
}

func (recordSuspendedAccount) GetStepType() string { return recordSuspendedAccountStepType }

func (recordSuspendedAccount) Execute(ctx dex.Context, result workspaceadmin.SuspendUserResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := accountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TeamGroupRemoval](removeAccountFromTeamGroupStepType), TeamGroupRemoval{
		GroupEmail: request.GroupEmail, MemberID: result.Value.ID,
	}), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the offboarding hand-off with the suspended account and the removed membership."
type recordOffboardingHandOff struct {
	dex.StepDefaultsNoWaitFor[workspaceadmin.RemoveUserFromGroupResult]
}

func (recordOffboardingHandOff) GetStepType() string { return recordOffboardingHandOffStepType }

func (recordOffboardingHandOff) Execute(ctx dex.Context, result workspaceadmin.RemoveUserFromGroupResult) (*dex.StepDecision, error) {
	account, err := accountAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	handOff := AccountHandOff{
		Status: HandOffOffboarded, PrimaryEmail: account.PrimaryEmail, UserID: account.ID, OrgUnitPath: account.OrgUnitPath,
		IsSuspended: account.IsSuspended, GroupEmail: result.Value.GroupKey, WasAlreadyRemoved: result.Value.WasAlreadyRemoved,
		NextStep: offboardedAccountHandOff,
	}
	if err := accountHandOffAttribute.Set(ctx, handOff); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(handOff), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
