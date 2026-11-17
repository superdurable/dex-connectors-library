// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onespansign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SignerRoleID identifies a signer role without exposing signer identity.
type SignerRoleID struct {
	// RoleID is the role identifier within one package.
	RoleID string `json:"roleId"`
}

// SignerRoleIDsPage lists signer roles without identity details.
type SignerRoleIDsPage struct {
	// RoleIDs are the package's signer role identifiers.
	RoleIDs []string `json:"roleIds"`
}

// ListSignerRolesInput selects one transaction's roles.
type ListSignerRolesInput struct {
	// PackageID is the transaction ID.
	PackageID string `json:"packageId"`
}

// GetSignerRoleInput selects one role in a transaction.
type GetSignerRoleInput struct {
	// PackageID is the transaction ID.
	PackageID string `json:"packageId"`
	// RoleID is the signer role ID.
	RoleID string `json:"roleId"`
}

// AddSignerInput supplies one signer role for a draft transaction.
type AddSignerInput struct {
	// PackageID is the draft transaction ID.
	PackageID string `json:"packageId"`
	// RoleID is a stable role identifier chosen by the application.
	RoleID string `json:"roleId"`
	// Email is the signer's delivery address; it is not returned or logged.
	Email string `json:"email"`
	// FirstName is the signer's given name.
	FirstName string `json:"firstName"`
	// LastName is the signer's family name.
	LastName string `json:"lastName"`
}

// UpdateSignerInput replaces one draft signer role's identity.
type UpdateSignerInput struct {
	// PackageID is the draft transaction ID.
	PackageID string `json:"packageId"`
	// RoleID is the signer role ID.
	RoleID string `json:"roleId"`
	// Email is the new delivery address.
	Email string `json:"email"`
	// FirstName is the new given name.
	FirstName string `json:"firstName"`
	// LastName is the new family name.
	LastName string `json:"lastName"`
}

// RemoveSignerInput selects one draft signer role to remove.
type RemoveSignerInput struct {
	// PackageID is the draft transaction ID.
	PackageID string `json:"packageId"`
	// RoleID is the signer role ID.
	RoleID string `json:"roleId"`
}

type signerRoleWire struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Signers []struct {
		Email string `json:"email"`
	} `json:"signers"`
}

func decodeSignerRoles(body []byte) ([]signerRoleWire, error) {
	var wire struct {
		Roles []signerRoleWire `json:"roles"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Roles == nil {
		return nil, errors.New("package roles are invalid")
	}
	roles := make([]signerRoleWire, 0, len(wire.Roles))
	for _, role := range wire.Roles {
		if role.Type != "SIGNER" {
			continue
		}
		if !validPackageID(role.ID) {
			return nil, errors.New("signer role ID is invalid")
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func validSignerInput(packageID, roleID, email, firstName, lastName string) bool {
	return validPackageID(packageID) && validPackageID(roleID) && len(email) <= 320 && strings.Count(email, "@") == 1 && !strings.ContainsAny(email, "\r\n\x00") && validPackageName(firstName) && validPackageName(lastName)
}

func signerRoleBody(roleID, email, firstName, lastName string) []byte {
	body, _ := json.Marshal(map[string]any{"id": roleID, "name": roleID, "type": "SIGNER", "signers": []any{map[string]any{"email": email, "firstName": firstName, "lastName": lastName}}})
	return body
}

func querySignerRoles(call sdkgo.Call, client *Client, operation string, packageID string, found, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.QueryAttempt[SignerRoleIDsPage] {
	response := client.requestPackage(call, call.Context, http.MethodGet, "/packages/"+url.PathEscape(packageID), nil)
	if errors.Is(response.err, errCredential) {
		return packageQueryBranch[SignerRoleIDsPage](operation, defect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SignerRoleIDsPage](packageOperationFailure(operation, sdkgo.FailureTransport, "package read failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SignerRoleIDsPage](packageOperationFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the read"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SignerRoleIDsPage](packageOperationFailure(operation, sdkgo.FailureAvailability, "provider is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return packageQueryBranch[SignerRoleIDsPage](operation, notFound, sdkgo.FailureNotFound, "package was not found")
	}
	if response.status != http.StatusOK {
		return packageQueryBranch[SignerRoleIDsPage](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected package read")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return packageQueryBranch[SignerRoleIDsPage](operation, invalid, sdkgo.FailureResponseTooLarge, "package response exceeded the limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SignerRoleIDsPage](packageOperationFailure(operation, sdkgo.FailureTransport, "package response was interrupted"), 0)
	}
	roles, err := decodeSignerRoles(response.body)
	if err != nil {
		return packageQueryBranch[SignerRoleIDsPage](operation, invalid, sdkgo.FailureProtocol, "provider returned invalid signer roles")
	}
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}
	return sdkgo.NewQueryBranch(found, SignerRoleIDsPage{RoleIDs: ids}, nil, sdkgo.Receipt{})
}

// ListSignerRolesOperation lists role IDs without signer identities.
type ListSignerRolesOperation struct{ client *Client }

// ListSignerRoles binds the signer role list Query.
func (client *Client) ListSignerRoles() ListSignerRolesOperation {
	return ListSignerRolesOperation{client}
}

// Definition returns the generated role list contract.
func (ListSignerRolesOperation) Definition() sdkgo.QueryDefinition { return ListSignerRolesDefinition }

// Invoke lists signer role IDs in one package.
func (operation ListSignerRolesOperation) Invoke(call sdkgo.Call, input ListSignerRolesInput) sdkgo.QueryAttempt[SignerRoleIDsPage] {
	if !validPackageID(input.PackageID) {
		return packageQueryBranch[SignerRoleIDsPage]("listSignerRoles", ListSignerRolesBranchDefect, sdkgo.FailureValidation, "package ID is invalid")
	}
	return querySignerRoles(call, operation.client, "listSignerRoles", input.PackageID, ListSignerRolesBranchListed, "", ListSignerRolesBranchProviderRejected, ListSignerRolesBranchInvalidResponse, ListSignerRolesBranchDefect)
}

// GetSignerRoleOperation checks one role without returning identity details.
type GetSignerRoleOperation struct{ client *Client }

// GetSignerRole binds the role lookup Query.
func (client *Client) GetSignerRole() GetSignerRoleOperation { return GetSignerRoleOperation{client} }

// Definition returns the generated role lookup contract.
func (GetSignerRoleOperation) Definition() sdkgo.QueryDefinition { return GetSignerRoleDefinition }

// Invoke confirms a role is present in a package.
func (operation GetSignerRoleOperation) Invoke(call sdkgo.Call, input GetSignerRoleInput) sdkgo.QueryAttempt[SignerRoleID] {
	if !validPackageID(input.PackageID) || !validPackageID(input.RoleID) {
		return packageQueryBranch[SignerRoleID]("getSignerRole", GetSignerRoleBranchDefect, sdkgo.FailureValidation, "package or role ID is invalid")
	}
	response := operation.client.requestPackage(call, call.Context, http.MethodGet, "/packages/"+url.PathEscape(input.PackageID), nil)
	if errors.Is(response.err, errCredential) {
		return packageQueryBranch[SignerRoleID]("getSignerRole", GetSignerRoleBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SignerRoleID](packageOperationFailure("getSignerRole", sdkgo.FailureTransport, "package read failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SignerRoleID](packageOperationFailure("getSignerRole", sdkgo.FailureRateLimit, "provider rate limited the read"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SignerRoleID](packageOperationFailure("getSignerRole", sdkgo.FailureAvailability, "provider is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return packageQueryBranch[SignerRoleID]("getSignerRole", GetSignerRoleBranchNotFound, sdkgo.FailureNotFound, "package was not found")
	}
	if response.status != http.StatusOK {
		return packageQueryBranch[SignerRoleID]("getSignerRole", GetSignerRoleBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected package read")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return packageQueryBranch[SignerRoleID]("getSignerRole", GetSignerRoleBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "package response exceeded the limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SignerRoleID](packageOperationFailure("getSignerRole", sdkgo.FailureTransport, "package response was interrupted"), 0)
	}
	roles, err := decodeSignerRoles(response.body)
	if err != nil {
		return packageQueryBranch[SignerRoleID]("getSignerRole", GetSignerRoleBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid signer roles")
	}
	for _, role := range roles {
		if role.ID == input.RoleID {
			return sdkgo.NewQueryBranch(GetSignerRoleBranchFound, SignerRoleID{RoleID: input.RoleID}, nil, sdkgo.Receipt{})
		}
	}
	return packageQueryBranch[SignerRoleID]("getSignerRole", GetSignerRoleBranchNotFound, sdkgo.FailureNotFound, "signer role was not found")
}

// AddSignerOperation adds a signer role to a draft transaction.
type AddSignerOperation struct{ client *Client }

// AddSigner binds signer role creation.
func (client *Client) AddSigner() AddSignerOperation { return AddSignerOperation{client} }

// Definition returns the generated signer creation contract.
func (AddSignerOperation) Definition() sdkgo.MutationDefinition { return AddSignerDefinition }

// IdempotencyKey identifies this call; OneSpan Sign accepts no idempotency header.
func (AddSignerOperation) IdempotencyKey(callID sdkgo.CallID, _ AddSignerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one signer, reading back the role after an ambiguous send.
func (operation AddSignerOperation) Invoke(call sdkgo.Call, input AddSignerInput) sdkgo.MutationAttempt[SignerRoleID] {
	if !validSignerInput(input.PackageID, input.RoleID, input.Email, input.FirstName, input.LastName) {
		return packageMutationBranch[SignerRoleID]("addSigner", AddSignerBranchDefect, sdkgo.FailureValidation, "signer fields are invalid")
	}
	fence, err := singledispatch.NewFence(call, AddSignerDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return packageMutationBranch[SignerRoleID]("addSigner", AddSignerBranchDefect, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.MaximumPriorSends > 0 || decision.Kind != singledispatch.SendAllowed {
		found, readError := operation.client.readBackSigner(call, input)
		if found {
			return sdkgo.NewMutationBranch(AddSignerBranchAdded, SignerRoleID{RoleID: input.RoleID}, nil, sdkgo.Receipt{})
		}
		if readError != nil {
			return sdkgo.NewMutationRetry[SignerRoleID](packageOperationFailure("addSigner", sdkgo.FailureAvailability, "signer read-back is unavailable"), 0)
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SignerRoleID{}, packageOperationFailure("addSigner", sdkgo.FailureTransport, "signer addition may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestPackage(call, requestContext, http.MethodPost, "/packages/"+url.PathEscape(input.PackageID)+"/roles", signerRoleBody(input.RoleID, input.Email, input.FirstName, input.LastName))
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SignerRoleID](packageOperationFailure("addSigner", sdkgo.FailureAvailability, "signer was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SignerRoleID{}, packageOperationFailure("addSigner", sdkgo.FailureAuthentication, "earlier addition may have succeeded"), sdkgo.Receipt{})
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[SignerRoleID](packageOperationFailure("addSigner", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return packageMutationBranch[SignerRoleID]("addSigner", AddSignerBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[SignerRoleID](packageOperationFailure("addSigner", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SignerRoleID](packageOperationFailure("addSigner", kind, "provider temporarily rejected the signer"), response.retryAfter)
	}
	if err != nil || response.status >= 500 {
		failure := packageOperationFailure("addSigner", sdkgo.FailureAvailability, "signer addition outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[SignerRoleID](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(SignerRoleID{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SignerRoleID{}, packageOperationFailure("addSigner", sdkgo.FailureProviderRejection, "earlier addition may have succeeded"), sdkgo.Receipt{})
		}
		return packageMutationBranch[SignerRoleID]("addSigner", AddSignerBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected the signer")
	}
	var created struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(response.body, &created) != nil || created.ID != input.RoleID {
		return sdkgo.NewMutationUncertain(SignerRoleID{}, packageOperationFailure("addSigner", sdkgo.FailureProtocol, "provider accepted signer without the requested role ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(AddSignerBranchAdded, SignerRoleID{RoleID: input.RoleID}, nil, sdkgo.Receipt{})
}

func (client *Client) readBackSigner(call sdkgo.Call, input AddSignerInput) (bool, error) {
	response := client.requestPackage(call, call.Context, http.MethodGet, "/packages/"+url.PathEscape(input.PackageID), nil)
	if response.err != nil || response.status != http.StatusOK {
		return false, errors.New("signer read-back failed")
	}
	roles, err := decodeSignerRoles(response.body)
	if err != nil {
		return false, err
	}
	for _, role := range roles {
		if role.ID == input.RoleID {
			return len(role.Signers) == 1 && role.Signers[0].Email == input.Email, nil
		}
	}
	return false, nil
}

// UpdateSignerOperation replaces a draft signer role's identity.
type UpdateSignerOperation struct{ client *Client }

// UpdateSigner binds signer role replacement.
func (client *Client) UpdateSigner() UpdateSignerOperation { return UpdateSignerOperation{client} }

// Definition returns the generated signer update contract.
func (UpdateSignerOperation) Definition() sdkgo.MutationDefinition { return UpdateSignerDefinition }

// IdempotencyKey identifies the absolute role replacement across retries.
func (UpdateSignerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSignerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the selected role's signer identity to the supplied values.
func (operation UpdateSignerOperation) Invoke(call sdkgo.Call, input UpdateSignerInput) sdkgo.MutationAttempt[SignerRoleID] {
	if !validSignerInput(input.PackageID, input.RoleID, input.Email, input.FirstName, input.LastName) {
		return packageMutationBranch[SignerRoleID]("updateSigner", UpdateSignerBranchDefect, sdkgo.FailureValidation, "signer fields are invalid")
	}
	return invokePackageWrite(call, operation.client, "updateSigner", http.MethodPut, "/packages/"+url.PathEscape(input.PackageID)+"/roles/"+url.PathEscape(input.RoleID), signerRoleBody(input.RoleID, input.Email, input.FirstName, input.LastName), UpdateSignerBranchUpdated, UpdateSignerBranchNotFound, UpdateSignerBranchProviderRejected, UpdateSignerBranchInvalidResponse, UpdateSignerBranchDefect, SignerRoleID{RoleID: input.RoleID})
}

// RemoveSignerOperation deletes a draft signer role.
type RemoveSignerOperation struct{ client *Client }

// RemoveSigner binds signer role deletion.
func (client *Client) RemoveSigner() RemoveSignerOperation { return RemoveSignerOperation{client} }

// Definition returns the generated signer removal contract.
func (RemoveSignerOperation) Definition() sdkgo.MutationDefinition { return RemoveSignerDefinition }

// IdempotencyKey identifies repeated removal of the same role.
func (RemoveSignerOperation) IdempotencyKey(callID sdkgo.CallID, _ RemoveSignerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one signer role from the package.
func (operation RemoveSignerOperation) Invoke(call sdkgo.Call, input RemoveSignerInput) sdkgo.MutationAttempt[SignerRoleID] {
	if !validPackageID(input.PackageID) || !validPackageID(input.RoleID) {
		return packageMutationBranch[SignerRoleID]("removeSigner", RemoveSignerBranchDefect, sdkgo.FailureValidation, "package or role ID is invalid")
	}
	return invokePackageWrite(call, operation.client, "removeSigner", http.MethodDelete, "/packages/"+url.PathEscape(input.PackageID)+"/roles/"+url.PathEscape(input.RoleID), nil, RemoveSignerBranchRemoved, RemoveSignerBranchNotFound, RemoveSignerBranchProviderRejected, RemoveSignerBranchInvalidResponse, RemoveSignerBranchDefect, SignerRoleID{RoleID: input.RoleID})
}
