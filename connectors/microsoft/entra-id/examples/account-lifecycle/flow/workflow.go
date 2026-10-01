// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountlifecycle onboards, reinstates, or offboards one Microsoft
// Entra ID account from Dex Web Start Flow: onboarding creates the hire's
// account, or finds the one an earlier run created for the same hire ID, and
// adds it to a team group; reinstating enables a returning person's account
// instead of creating a second one; offboarding finds the account, disables
// it, revokes its sign-in sessions, and removes the group membership. Every
// path records a hand-off for the next owner.
package accountlifecycle

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "EntraAccountLifecycle"
	// ConnectionName is the static Dex Web connection for Microsoft Entra ID.
	ConnectionName = "microsoft-entra-id"

	recordAccountRequestStepType       = "RecordAccountRequest"
	createEntraAccountStepType         = "CreateEntraAccount"
	recordCreatedAccountStepType       = "RecordCreatedAccount"
	recordAddressTakenHandOffStepType  = "RecordAddressTakenHandOff"
	enableEntraAccountStepType         = "EnableEntraAccount"
	recordReinstatedAccountStepType    = "RecordReinstatedAccount"
	addAccountToTeamGroupStepType      = "AddAccountToTeamGroup"
	recordOnboardingHandOffStepType    = "RecordOnboardingHandOff"
	findDepartingAccountStepType       = "FindDepartingAccount"
	disableEntraAccountStepType        = "DisableEntraAccount"
	recordDisabledAccountStepType      = "RecordDisabledAccount"
	revokeAccountSessionsStepType      = "RevokeAccountSessions"
	recordRevokedSessionsStepType      = "RecordRevokedSessions"
	removeAccountFromTeamGroupStepType = "RemoveAccountFromTeamGroup"
	recordOffboardingHandOffStepType   = "RecordOffboardingHandOff"

	newAccountSignInHandOff = "The connector set a random password that nobody received, and Microsoft requires a new one at the next sign-in. " +
		"Sign in through your federated identity provider, or have an administrator open Microsoft Entra admin center > Users > the user > " +
		"Reset password, or issue a Temporary Access Pass, and deliver it out of band."
	reinstatedAccountSignInHandOff = "The account's existing password and authentication methods are unchanged. If the person no longer " +
		"knows the password, an administrator resets it in Microsoft Entra admin center > Users > the user > Reset password."
	offboardedAccountHandOff = "Sign-in is blocked and existing sessions are revoked; Microsoft notes that tokens can keep working for a few " +
		"minutes. The account's data is kept: remove its licenses, transfer its files and mailbox, and delete it when your retention policy allows."
	addressTakenHandOff = "The user principal name already belongs to another account, or to a deleted one, so nothing was created. " +
		"Confirm with IT whether it is this person's account; if it is, start the reinstate action, otherwise choose another name."
)

var (
	accountRequestAttribute  = dex.DefineAttribute[AccountRequest]("entra-account-request")
	accountCreationAttribute = dex.DefineAttribute[entraid.CreateUserResult]("entra-account-creation")
	accountAttribute         = dex.DefineAttribute[entraid.User]("entra-account")
	groupMembershipAttribute = dex.DefineAttribute[entraid.AddUserToGroupResult]("entra-group-membership")
	accountHandOffAttribute  = dex.DefineAttribute[AccountHandOff]("entra-account-hand-off")

	objectIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// AccountAction selects the lifecycle path.
type AccountAction string

const (
	// AccountActionOnboard creates the account and adds it to the team group.
	AccountActionOnboard AccountAction = "onboard"
	// AccountActionReinstate enables a returning person's existing account and adds it to the team group.
	AccountActionReinstate AccountAction = "reinstate"
	// AccountActionOffboard disables the account, revokes its sessions, and removes it from the team group.
	AccountActionOffboard AccountAction = "offboard"
)

// Input is the request entered in Dex Web Start Flow.
type Input struct {
	// Action is onboard, reinstate, or offboard.
	Action AccountAction `json:"action"`
	// UserPrincipalName is the account's sign-in name, such as ada.lovelace@contoso.com.
	UserPrincipalName string `json:"userPrincipalName"`
	// DisplayName is the address-book name for onboarding; blank joins GivenName and Surname.
	DisplayName string `json:"displayName,omitempty"`
	// GivenName is the first name for onboarding.
	GivenName string `json:"givenName,omitempty"`
	// Surname is the family name for onboarding.
	Surname string `json:"surname,omitempty"`
	// JobTitle is the optional job title for onboarding.
	JobTitle string `json:"jobTitle,omitempty"`
	// Department is the optional department for onboarding.
	Department string `json:"department,omitempty"`
	// UsageLocation is the optional two-letter country code, such as US, that licensing needs.
	UsageLocation string `json:"usageLocation,omitempty"`
	// GroupID is the team group's object ID, from Microsoft Entra admin center > Groups > the group > Overview.
	GroupID string `json:"groupId"`
	// ProvisioningKey is an optional HR hire ID that keeps one account per hire across Flow runs.
	ProvisioningKey string `json:"provisioningKey,omitempty"`
}

// AccountRequest is the validated request every later Step reads.
type AccountRequest struct {
	// Action is the lifecycle path.
	Action AccountAction `json:"action"`
	// UserPrincipalName is the account's sign-in name.
	UserPrincipalName string `json:"userPrincipalName"`
	// DisplayName is the onboarding display name.
	DisplayName string `json:"displayName,omitempty"`
	// GivenName is the onboarding first name.
	GivenName string `json:"givenName,omitempty"`
	// Surname is the onboarding family name.
	Surname string `json:"surname,omitempty"`
	// JobTitle is the onboarding job title.
	JobTitle string `json:"jobTitle,omitempty"`
	// Department is the onboarding department.
	Department string `json:"department,omitempty"`
	// UsageLocation is the onboarding usage location.
	UsageLocation string `json:"usageLocation,omitempty"`
	// GroupID is the team group's object ID in lowercase.
	GroupID string `json:"groupId"`
	// ProvisioningKey is the optional HR hire ID.
	ProvisioningKey string `json:"provisioningKey,omitempty"`
}

// TeamGroupMembership is the membership to add or remove once the account's object ID is known.
type TeamGroupMembership struct {
	// GroupID is the team group's object ID.
	GroupID string `json:"groupId"`
	// UserID is the account's object ID.
	UserID string `json:"userId"`
}

// HandOffStatus is the Flow's terminal business outcome.
type HandOffStatus string

const (
	// HandOffOnboarded means the account exists, is enabled, and is in the team group.
	HandOffOnboarded HandOffStatus = "onboarded"
	// HandOffReinstated means the existing account is enabled again and is in the team group.
	HandOffReinstated HandOffStatus = "reinstated"
	// HandOffOffboarded means the account is disabled, its sessions are revoked, and it is no longer a direct team group member.
	HandOffOffboarded HandOffStatus = "offboarded"
	// HandOffAddressTaken means the user principal name belongs to something else, so nothing was created.
	HandOffAddressTaken HandOffStatus = "addressTaken"
)

// AccountHandOff is the Flow result and the record the next owner acts on.
type AccountHandOff struct {
	// Status is the terminal business outcome.
	Status HandOffStatus `json:"status"`
	// UserPrincipalName is the requested sign-in name.
	UserPrincipalName string `json:"userPrincipalName"`
	// UserID is the account's object ID; for addressTaken it is the conflicting account's ID, when Microsoft returns one.
	UserID string `json:"userId,omitempty"`
	// IsAccountEnabled reports the account's final sign-in state.
	IsAccountEnabled bool `json:"isAccountEnabled"`
	// GroupID is the team group's object ID.
	GroupID string `json:"groupId"`
	// WasAlreadyCreated reports that the account already carried this hire's provisioning key or this Step's key.
	WasAlreadyCreated bool `json:"wasAlreadyCreated,omitempty"`
	// WasAlreadyMember reports that the account was already a direct group member.
	WasAlreadyMember bool `json:"wasAlreadyMember,omitempty"`
	// AreSessionsRevoked reports that offboarding revoked every refresh token and session cookie.
	AreSessionsRevoked bool `json:"areSessionsRevoked,omitempty"`
	// WasAlreadyRemoved reports that the account was not a direct group member when offboarding ran.
	WasAlreadyRemoved bool `json:"wasAlreadyRemoved,omitempty"`
	// NextStep tells the human owner what this Flow did not do.
	NextStep string `json:"nextStep"`
}

// Flow runs one account lifecycle action.
type Flow struct {
	dex.FlowDefaults
	connection entraid.Connection
}

// NewFlow binds the Microsoft Entra ID Connection at registration time.
func NewFlow(connection entraid.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Microsoft Entra ID connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordAccountRequest{}),
		dex.DefineStep(entraid.NewCreateUserStep(entraid.CreateUserStepConfig[AccountRequest]{
			StepType: createEntraAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "microsoft-entra-id", GroupLabel: "Microsoft Entra ID", Explanation: "Create the account with a password nobody receives, tagged so a repeated Step finds it instead of creating a second one."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateUserInput, ResultAttribute: &accountCreationAttribute,
			Created:       sdkgo.GoTo(recordCreatedAccount{}),
			AlreadyExists: sdkgo.GoTo(recordAddressTakenHandOff{}),
		})),
		dex.DefineStep(recordCreatedAccount{}),
		dex.DefineStep(recordAddressTakenHandOff{}),
		dex.DefineStep(entraid.NewEnableUserStep(entraid.EnableUserStepConfig[AccountRequest]{
			StepType: enableEntraAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "microsoft-entra-id", GroupLabel: "Microsoft Entra ID", Explanation: "Enable the returning person's existing account instead of creating another."},
			Connection:  flow.connection, MapToOperationInput: MapToEnableUserInput,
			Enabled: sdkgo.GoTo(recordReinstatedAccount{}),
		})),
		dex.DefineStep(recordReinstatedAccount{}),
		dex.DefineStep(entraid.NewAddUserToGroupStep(entraid.AddUserToGroupStepConfig[TeamGroupMembership]{
			StepType: addAccountToTeamGroupStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "microsoft-entra-id", GroupLabel: "Microsoft Entra ID", Explanation: "Add the account to the team group; an existing membership is confirmed, not duplicated."},
			Connection:  flow.connection, MapToOperationInput: MapToAddUserToGroupInput, ResultAttribute: &groupMembershipAttribute,
			Added: sdkgo.GoTo(recordOnboardingHandOff{}),
		})),
		dex.DefineStep(recordOnboardingHandOff{}),
		dex.DefineStep(entraid.NewGetUserStep(entraid.GetUserStepConfig[AccountRequest]{
			StepType: findDepartingAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "microsoft-entra-id", GroupLabel: "Microsoft Entra ID", Explanation: "Find the departing account by its user principal name before changing anything."},
			Connection:  flow.connection, MapToOperationInput: MapToGetUserInput,
			Found: sdkgo.GoTo(sdkgo.StepRef[entraid.GetUserResult](disableEntraAccountStepType)),
		})),
		dex.DefineStep(entraid.NewDisableUserStep(entraid.DisableUserStepConfig[entraid.GetUserResult]{
			StepType: disableEntraAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "microsoft-entra-id", GroupLabel: "Microsoft Entra ID", Explanation: "Disable the departing account by its object ID so it cannot sign in while its data is kept."},
			Connection:  flow.connection, MapToOperationInput: MapToDisableUserInput,
			Disabled: sdkgo.GoTo(recordDisabledAccount{}),
		})),
		dex.DefineStep(recordDisabledAccount{}),
		dex.DefineStep(entraid.NewRevokeSignInSessionsStep(entraid.RevokeSignInSessionsStepConfig[entraid.User]{
			StepType: revokeAccountSessionsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "microsoft-entra-id", GroupLabel: "Microsoft Entra ID", Explanation: "Revoke every refresh token and session cookie so signed-in apps must sign in again."},
			Connection:  flow.connection, MapToOperationInput: MapToRevokeSignInSessionsInput,
			Revoked: sdkgo.GoTo(recordRevokedSessions{}),
		})),
		dex.DefineStep(recordRevokedSessions{}),
		dex.DefineStep(entraid.NewRemoveUserFromGroupStep(entraid.RemoveUserFromGroupStepConfig[TeamGroupMembership]{
			StepType: removeAccountFromTeamGroupStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "microsoft-entra-id", GroupLabel: "Microsoft Entra ID", Explanation: "Remove the disabled account from the team group; a membership already gone counts as removed."},
			Connection:  flow.connection, MapToOperationInput: MapToRemoveUserFromGroupInput,
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
// dex:field attribute-key:entra-account-request value-type:json editable:false description:"Requested lifecycle action"
// dex:field attribute-key:entra-account-hand-off value-type:json editable:false description:"Hand-off for the next owner"
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
		"entra-account-request":  request,
		"entra-account-hand-off": handOff,
	}}, nil
}

// GetDexDisplay returns the request, account, membership, and hand-off.
//
// dex:field attribute-key:entra-account-request value-type:json editable:false description:"Action, user principal name, and group"
// dex:field attribute-key:entra-account value-type:json editable:false description:"Account as Microsoft Graph last returned it"
// dex:field attribute-key:entra-group-membership value-type:object editable:false description:"Team group membership result"
// dex:field attribute-key:entra-account-hand-off value-type:json editable:false description:"Status and what the next owner must do"
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
		"entra-account-request":  request,
		"entra-account":          account,
		"entra-group-membership": membership,
		"entra-account-hand-off": handOff,
	}}, nil
}

// MapToCreateUserInput creates the account with the optional hire ID as its creation key.
func MapToCreateUserInput(request AccountRequest) entraid.CreateUserInput {
	return entraid.CreateUserInput{
		UserPrincipalName: request.UserPrincipalName, DisplayName: request.DisplayName, GivenName: request.GivenName,
		Surname: request.Surname, JobTitle: request.JobTitle, Department: request.Department,
		UsageLocation: request.UsageLocation, ProvisioningKey: request.ProvisioningKey,
	}
}

// MapToEnableUserInput enables the requested user principal name.
func MapToEnableUserInput(request AccountRequest) entraid.EnableUserInput {
	return entraid.EnableUserInput{UserKey: request.UserPrincipalName}
}

// MapToGetUserInput finds the departing account by its user principal name.
func MapToGetUserInput(request AccountRequest) entraid.GetUserInput {
	return entraid.GetUserInput{UserKey: request.UserPrincipalName}
}

// MapToDisableUserInput disables the found account by its object ID, which survives a rename.
func MapToDisableUserInput(result entraid.GetUserResult) entraid.DisableUserInput {
	return entraid.DisableUserInput{UserKey: result.Value.ID}
}

// MapToRevokeSignInSessionsInput revokes the disabled account's sessions by its object ID.
func MapToRevokeSignInSessionsInput(account entraid.User) entraid.RevokeSignInSessionsInput {
	return entraid.RevokeSignInSessionsInput{UserKey: account.ID}
}

// MapToAddUserToGroupInput adds the account to the team group.
func MapToAddUserToGroupInput(membership TeamGroupMembership) entraid.AddUserToGroupInput {
	return entraid.AddUserToGroupInput{GroupID: membership.GroupID, UserID: membership.UserID}
}

// MapToRemoveUserFromGroupInput removes the account from the team group.
func MapToRemoveUserFromGroupInput(membership TeamGroupMembership) entraid.RemoveUserFromGroupInput {
	return entraid.RemoveUserFromGroupInput{GroupID: membership.GroupID, UserID: membership.UserID}
}

// BuildTeamGroupMembership pairs the requested group with the account's object ID.
func BuildTeamGroupMembership(request AccountRequest, account entraid.User) TeamGroupMembership {
	return TeamGroupMembership{GroupID: request.GroupID, UserID: account.ID}
}

// BuildAccountRequest validates Start Flow input before any provider call.
func BuildAccountRequest(input Input) (AccountRequest, error) {
	request := AccountRequest{
		Action: input.Action, UserPrincipalName: strings.TrimSpace(input.UserPrincipalName),
		DisplayName: strings.TrimSpace(input.DisplayName), GivenName: strings.TrimSpace(input.GivenName),
		Surname: strings.TrimSpace(input.Surname), JobTitle: strings.TrimSpace(input.JobTitle),
		Department: strings.TrimSpace(input.Department), UsageLocation: strings.ToUpper(strings.TrimSpace(input.UsageLocation)),
		GroupID: strings.ToLower(strings.TrimSpace(input.GroupID)), ProvisioningKey: strings.TrimSpace(input.ProvisioningKey),
	}
	localPart, domain, hasAt := strings.Cut(request.UserPrincipalName, "@")
	if !hasAt || localPart == "" || domain == "" || strings.ContainsAny(request.UserPrincipalName, " <>\"") {
		return AccountRequest{}, errors.New("userPrincipalName must be one sign-in name such as ada.lovelace@contoso.com")
	}
	if !objectIDPattern.MatchString(request.GroupID) {
		return AccountRequest{}, errors.New("groupId must be the team group's object ID, a GUID")
	}
	switch request.Action {
	case AccountActionOnboard:
		if request.DisplayName == "" && request.GivenName == "" && request.Surname == "" {
			return AccountRequest{}, errors.New("onboarding requires displayName, givenName, or surname")
		}
	case AccountActionReinstate, AccountActionOffboard:
	default:
		return AccountRequest{}, fmt.Errorf("action must be %s, %s, or %s", AccountActionOnboard, AccountActionReinstate, AccountActionOffboard)
	}
	return request, nil
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
		return dex.GoTo(sdkgo.StepRef[AccountRequest](createEntraAccountStepType), request), nil
	case AccountActionReinstate:
		return dex.GoTo(sdkgo.StepRef[AccountRequest](enableEntraAccountStepType), request), nil
	default:
		return dex.GoTo(sdkgo.StepRef[AccountRequest](findDepartingAccountStepType), request), nil
	}
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the created or found account, then add it to the team group."
type recordCreatedAccount struct {
	dex.StepDefaultsNoWaitFor[entraid.CreateUserResult]
}

func (recordCreatedAccount) GetStepType() string { return recordCreatedAccountStepType }

func (recordCreatedAccount) Execute(ctx dex.Context, result entraid.CreateUserResult) (*dex.StepDecision, error) {
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
// dex:explanation text:"Record that the name belongs to something else and hand the decision to IT without creating anything."
type recordAddressTakenHandOff struct {
	dex.StepDefaultsNoWaitFor[entraid.CreateUserResult]
}

func (recordAddressTakenHandOff) GetStepType() string { return recordAddressTakenHandOffStepType }

func (recordAddressTakenHandOff) Execute(ctx dex.Context, result entraid.CreateUserResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	existing := result.Value.User
	handOff := AccountHandOff{
		Status: HandOffAddressTaken, UserPrincipalName: request.UserPrincipalName, UserID: existing.ID,
		IsAccountEnabled: existing.IsAccountEnabled, GroupID: request.GroupID, NextStep: addressTakenHandOff,
	}
	if err := accountHandOffAttribute.Set(ctx, handOff); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(handOff), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the reinstated account, then add it to the team group."
type recordReinstatedAccount struct {
	dex.StepDefaultsNoWaitFor[entraid.EnableUserResult]
}

func (recordReinstatedAccount) GetStepType() string { return recordReinstatedAccountStepType }

func (recordReinstatedAccount) Execute(ctx dex.Context, result entraid.EnableUserResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := accountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TeamGroupMembership](addAccountToTeamGroupStepType), BuildTeamGroupMembership(request, result.Value)), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the onboarding or reinstating hand-off with the account and its team group membership."
type recordOnboardingHandOff struct {
	dex.StepDefaultsNoWaitFor[entraid.AddUserToGroupResult]
}

func (recordOnboardingHandOff) GetStepType() string { return recordOnboardingHandOffStepType }

func (recordOnboardingHandOff) Execute(ctx dex.Context, result entraid.AddUserToGroupResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	account, err := accountAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	handOff := AccountHandOff{
		Status: HandOffReinstated, UserPrincipalName: account.UserPrincipalName, UserID: account.ID,
		IsAccountEnabled: account.IsAccountEnabled, GroupID: result.Value.GroupID,
		WasAlreadyMember: result.Value.WasAlreadyMember, NextStep: reinstatedAccountSignInHandOff,
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
// dex:explanation text:"Record the disabled account, then revoke its sign-in sessions."
type recordDisabledAccount struct {
	dex.StepDefaultsNoWaitFor[entraid.DisableUserResult]
}

func (recordDisabledAccount) GetStepType() string { return recordDisabledAccountStepType }

func (recordDisabledAccount) Execute(ctx dex.Context, result entraid.DisableUserResult) (*dex.StepDecision, error) {
	if err := accountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[entraid.User](revokeAccountSessionsStepType), result.Value), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Confirm the sessions were revoked, then remove the account from the team group by its object ID."
type recordRevokedSessions struct {
	dex.StepDefaultsNoWaitFor[entraid.RevokeSignInSessionsResult]
}

func (recordRevokedSessions) GetStepType() string { return recordRevokedSessionsStepType }

func (recordRevokedSessions) Execute(ctx dex.Context, _ entraid.RevokeSignInSessionsResult) (*dex.StepDecision, error) {
	request, err := accountRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	account, err := accountAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TeamGroupMembership](removeAccountFromTeamGroupStepType), BuildTeamGroupMembership(request, account)), nil
}

// dex:group group-id:account group-label:"Account"
// dex:explanation text:"Record the offboarding hand-off with the disabled account, revoked sessions, and removed membership."
type recordOffboardingHandOff struct {
	dex.StepDefaultsNoWaitFor[entraid.RemoveUserFromGroupResult]
}

func (recordOffboardingHandOff) GetStepType() string { return recordOffboardingHandOffStepType }

func (recordOffboardingHandOff) Execute(ctx dex.Context, result entraid.RemoveUserFromGroupResult) (*dex.StepDecision, error) {
	account, err := accountAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	handOff := AccountHandOff{
		Status: HandOffOffboarded, UserPrincipalName: account.UserPrincipalName, UserID: account.ID,
		IsAccountEnabled: account.IsAccountEnabled, GroupID: result.Value.GroupID, AreSessionsRevoked: true,
		WasAlreadyRemoved: result.Value.WasAlreadyRemoved, NextStep: offboardedAccountHandOff,
	}
	if err := accountHandOffAttribute.Set(ctx, handOff); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(handOff), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
