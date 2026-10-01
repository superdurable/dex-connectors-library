// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// creationVisibilityWindow tolerates Google's propagation delay before an unreadable taken address selects alreadyExists.
	creationVisibilityWindow = time.Minute
	// initialPasswordRandomBytes yields a 43-character password; Google accepts 8 to 100 ASCII characters.
	initialPasswordRandomBytes = 32
	maxOrgUnitPathLength       = 1024
)

var (
	// creationKeyPattern bounds a key that is stored as a Google external ID value.
	creationKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	// e164PhonePattern is the recovery phone format Google requires.
	e164PhonePattern = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)
)

// CreateUserInput describes one account to create. The connector generates the
// initial password itself: no password is accepted, returned, stored, or logged.
type CreateUserInput struct {
	// PrimaryEmail is the new sign-in address in one of the account's verified domains, such as ada@example.com.
	PrimaryEmail string `json:"primaryEmail"`
	// GivenName is the required first name, at most 60 characters.
	GivenName string `json:"givenName"`
	// FamilyName is the required last name, at most 60 characters.
	FamilyName string `json:"familyName"`
	// OrgUnitPath places the account in an organizational unit, such as /Engineering; blank uses the top-level unit /.
	OrgUnitPath string `json:"orgUnitPath,omitempty"`
	// RecoveryEmail is an optional personal address Google can use for account recovery.
	RecoveryEmail string `json:"recoveryEmail,omitempty"`
	// RecoveryPhone is an optional E.164 recovery phone number, such as +16506661212.
	RecoveryPhone string `json:"recoveryPhone,omitempty"`
	// ProvisioningKey optionally identifies one logical account across Step executions, such as an HR hire ID.
	// It is 1 to 128 letters, digits, and . _ : - characters; blank uses the Step's Call ID.
	ProvisioningKey string `json:"provisioningKey,omitempty"`
}

// CreateUserOutput is the account that holds the requested address.
type CreateUserOutput struct {
	// User is the created account, or for alreadyExists the other account Google returns for the address, if any.
	User User `json:"user"`
	// WasAlreadyCreated reports that an earlier attempt or Step execution with the same key created the account.
	WasAlreadyCreated bool `json:"wasAlreadyCreated"`
}

// CreateUserOperation implements the createUser connector Mutation.
type CreateUserOperation struct{ client *Client }

type createUserRequest struct {
	PrimaryEmail              string                `json:"primaryEmail"`
	Name                      createUserName        `json:"name"`
	Password                  string                `json:"password"`
	ChangePasswordAtNextLogin bool                  `json:"changePasswordAtNextLogin"`
	OrgUnitPath               string                `json:"orgUnitPath"`
	ExternalIDs               []directoryExternalID `json:"externalIds"`
	RecoveryEmail             string                `json:"recoveryEmail,omitempty"`
	RecoveryPhone             string                `json:"recoveryPhone,omitempty"`
}

type createUserName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

var createUserReadBackFailureBranches = failureBranches{
	notFound: CreateUserBranchProviderRejected, conflict: CreateUserBranchProviderRejected, rejected: CreateUserBranchProviderRejected,
	invalidResponse: CreateUserBranchInvalidResponse, defect: CreateUserBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (CreateUserOperation) Definition() sdkgo.MutationDefinition { return CreateUserDefinition }

// IdempotencyKey returns the creation key recorded on the account: input.ProvisioningKey when
// set, otherwise the Call ID, which every attempt of one Step execution shares.
func (CreateUserOperation) IdempotencyKey(callID sdkgo.CallID, input CreateUserInput) sdkgo.IdempotencyKey {
	if key := strings.TrimSpace(input.ProvisioningKey); key != "" {
		return sdkgo.IdempotencyKey(key)
	}
	return sdkgo.IdempotencyKey(callID)
}

// Invoke inserts the account with the creation key as a custom external ID. When Google
// reports the address as taken, it reads the account back: one carrying the key is this
// Step's account, so a repeated or concurrent attempt converges on it instead of creating
// another. Because of that read-back, transport failures and 5xx responses are retried.
func (operation CreateUserOperation) Invoke(call sdkgo.Call, input CreateUserInput) sdkgo.MutationAttempt[CreateUserOutput] {
	const operationID = "createUser"
	creationKey := string(call.IdempotencyKey)
	request, err := buildCreateUserRequest(input, creationKey)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, failure, sdkgo.Receipt{})
	}
	if request.Password, err = generateInitialPassword(); err != nil {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, failurePointer(sdkgo.FailureLocalDefect, operationID, "initial password could not be generated"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, &credential, operationID, directoryRequest{method: http.MethodPost, path: "/users", payload: request})
	switch exchange.outcome {
	case exchangeSucceeded:
		user, _, err := decodeUser(exchange.response.body)
		if err != nil || !strings.EqualFold(user.PrimaryEmail, request.PrimaryEmail) {
			return sdkgo.NewMutationRetry[CreateUserOutput](directoryFailure(sdkgo.FailureProtocol, operationID, "insert response is unusable; the retry reads the account back"), 0)
		}
		return sdkgo.NewMutationBranch(CreateUserBranchCreated, CreateUserOutput{User: user}, nil, operation.client.receipt(call, exchange.response, user.ID))
	case exchangeConflict:
		return operation.convergeOnExistingAddress(call, &credential, request.PrimaryEmail, creationKey)
	case exchangeInvalidResponse:
		return sdkgo.NewMutationRetry[CreateUserOutput](directoryFailure(exchange.failure.Kind, operationID, "insert response is unusable; the retry reads the account back"), 0)
	case exchangeNotFound, exchangeRejected:
		failure := exchange.failure
		return sdkgo.NewMutationBranch(CreateUserBranchProviderRejected, CreateUserOutput{}, &failure, operation.client.receipt(call, exchange.response, ""))
	default:
		return mutationAttemptFromExchange[CreateUserOutput](operation.client, call, exchange, createUserReadBackFailureBranches)
	}
}

// convergeOnExistingAddress reads the account behind a 409 and decides whether this Step created it.
func (operation CreateUserOperation) convergeOnExistingAddress(call sdkgo.Call, credential *Credentials, primaryEmail string, creationKey string) sdkgo.MutationAttempt[CreateUserOutput] {
	const operationID = "createUser"
	read := operation.client.readUser(call, credential, operationID, primaryEmail)
	switch read.exchange.outcome {
	case exchangeSucceeded:
		receipt := operation.client.receipt(call, read.exchange.response, read.user.ID)
		if read.resource.hasCreationKey(creationKey) {
			return sdkgo.NewMutationBranch(CreateUserBranchCreated, CreateUserOutput{User: read.user, WasAlreadyCreated: true}, nil, receipt)
		}
		return sdkgo.NewMutationBranch(CreateUserBranchAlreadyExists, CreateUserOutput{User: read.user},
			failurePointer(sdkgo.FailureConflict, operationID, "address belongs to an account this Step did not create"), receipt)
	case exchangeNotFound:
		if operation.isWithinCreationVisibilityWindow(call) {
			return sdkgo.NewMutationRetry[CreateUserOutput](directoryFailure(sdkgo.FailureAvailability, operationID, "Google reports the address as taken but does not return the account yet"), 0)
		}
		return sdkgo.NewMutationBranch(CreateUserBranchAlreadyExists, CreateUserOutput{},
			failurePointer(sdkgo.FailureConflict, operationID, "address belongs to a group or an account outside the directory"),
			operation.client.receipt(call, read.exchange.response, ""))
	default:
		return mutationAttemptFromExchange[CreateUserOutput](operation.client, call, read.exchange, createUserReadBackFailureBranches)
	}
}

// isWithinCreationVisibilityWindow tolerates Google's documented propagation delay after an account is created.
func (operation CreateUserOperation) isWithinCreationVisibilityWindow(call sdkgo.Call) bool {
	if call.Context == nil {
		return false
	}
	firstAttemptAt := call.Context.FirstAttemptAt()
	return !firstAttemptAt.IsZero() && operation.client.now().Sub(firstAttemptAt) < creationVisibilityWindow
}

func buildCreateUserRequest(input CreateUserInput, creationKey string) (createUserRequest, error) {
	primaryEmail := strings.TrimSpace(input.PrimaryEmail)
	if !isBareEmailAddress(primaryEmail) {
		return createUserRequest{}, errors.New("primaryEmail must be one bare email address")
	}
	if !creationKeyPattern.MatchString(creationKey) {
		return createUserRequest{}, errors.New("provisioningKey must be 1 to 128 letters, digits, or . _ : - characters")
	}
	givenName, err := validateDisplayName("givenName", input.GivenName)
	if err != nil {
		return createUserRequest{}, err
	}
	familyName, err := validateDisplayName("familyName", input.FamilyName)
	if err != nil {
		return createUserRequest{}, err
	}
	orgUnitPath := strings.TrimSpace(input.OrgUnitPath)
	if orgUnitPath == "" {
		orgUnitPath = "/"
	}
	if !strings.HasPrefix(orgUnitPath, "/") || len(orgUnitPath) > maxOrgUnitPathLength || strings.ContainsFunc(orgUnitPath, isControlCharacter) {
		return createUserRequest{}, errors.New("orgUnitPath must be a full path such as /Engineering")
	}
	recoveryEmail := strings.TrimSpace(input.RecoveryEmail)
	if recoveryEmail != "" && !isBareEmailAddress(recoveryEmail) {
		return createUserRequest{}, errors.New("recoveryEmail must be one bare email address")
	}
	recoveryPhone := strings.TrimSpace(input.RecoveryPhone)
	if recoveryPhone != "" && !e164PhonePattern.MatchString(recoveryPhone) {
		return createUserRequest{}, errors.New("recoveryPhone must be an E.164 number such as +16506661212")
	}
	return createUserRequest{
		PrimaryEmail: primaryEmail, Name: createUserName{GivenName: givenName, FamilyName: familyName},
		// Nobody receives the generated password, so the next password sign-in must set a new one.
		ChangePasswordAtNextLogin: true, OrgUnitPath: orgUnitPath,
		ExternalIDs:   []directoryExternalID{{Type: "custom", CustomType: creationKeyExternalIDType, Value: creationKey}},
		RecoveryEmail: recoveryEmail, RecoveryPhone: recoveryPhone,
	}, nil
}

// generateInitialPassword returns 256 random bits as URL-safe text that lives only in the request body.
func generateInitialPassword() (string, error) {
	random := make([]byte, initialPasswordRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}
