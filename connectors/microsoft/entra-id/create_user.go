// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// creationKeyPrefix marks an extension attribute value written by createUser, so administrators can recognize it.
	creationKeyPrefix = "dex-creation-key:"
	// initialPasswordLength stays inside Microsoft Entra's 8 to 256 character limit with about 270 random bits.
	initialPasswordLength      = 44
	maxDisplayNameLength       = 256
	maxNamePartLength          = 64
	maxJobTitleLength          = 128
	maxDepartmentLength        = 64
	maxMailNicknameLength      = 64
	maxUserPrincipalNameLength = 113
	maxUPNLocalPartLength      = 64
	maxUPNDomainLength         = 48
	// objectConflictDetailCode is the error detail Microsoft Entra reports for a taken unique property.
	objectConflictDetailCode = "ObjectConflict"
)

var (
	// creationKeyPattern bounds a key that is stored in an extension attribute.
	creationKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	// userPrincipalNameLocalPartPattern is Microsoft Entra's documented user name character set.
	userPrincipalNameLocalPartPattern = regexp.MustCompile(`^[A-Za-z0-9'.\-_!#^~]+$`)
	domainNamePattern                 = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))+$`)
	// mailNicknamePattern is the Exchange alias character set.
	mailNicknamePattern  = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+\\-/=?^_`{|}~.]+$")
	usageLocationPattern = regexp.MustCompile(`^[A-Z]{2}$`)
	// errorDetailCodePointers locate the per-property codes Microsoft Entra adds to a 400.
	errorDetailCodePointers = []string{"/error/details/0/code", "/error/details/1/code", "/error/details/2/code"}
)

// initialPasswordCharacterClasses are the four classes Microsoft Entra counts; every password draws from each.
var initialPasswordCharacterClasses = []string{
	"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789", "!#%*+-=?@^_~",
}

// CreateUserInput describes one enabled account to create. The connector generates the
// initial password itself: no password is accepted, returned, stored, or logged.
type CreateUserInput struct {
	// UserPrincipalName is the new sign-in name in one of the tenant's verified managed domains, such as
	// ada.lovelace@contoso.com: at most 64 letters, digits, and ' . - _ ! # ^ ~ before the @, at most 113 characters in all.
	UserPrincipalName string `json:"userPrincipalName"`
	// DisplayName is the address-book name, at most 256 characters; blank joins GivenName and Surname.
	DisplayName string `json:"displayName,omitempty"`
	// GivenName is the optional first name, at most 64 characters.
	GivenName string `json:"givenName,omitempty"`
	// Surname is the optional family name, at most 64 characters.
	Surname string `json:"surname,omitempty"`
	// MailNickname is the mail alias, at most 64 characters; blank uses the part of UserPrincipalName before the @.
	MailNickname string `json:"mailNickname,omitempty"`
	// JobTitle is the optional job title, at most 128 characters.
	JobTitle string `json:"jobTitle,omitempty"`
	// Department is the optional department, at most 64 characters.
	Department string `json:"department,omitempty"`
	// UsageLocation is an optional two-letter ISO 3166 country code, such as US, which Microsoft requires before
	// a license can be assigned.
	UsageLocation string `json:"usageLocation,omitempty"`
	// ProvisioningKey optionally identifies one logical account across Step executions, such as an HR hire ID.
	// It is 1 to 128 letters, digits, and . _ : - characters; blank uses the Step's Call ID.
	ProvisioningKey string `json:"provisioningKey,omitempty"`
}

// CreateUserOutput is the account that holds the requested user principal name.
type CreateUserOutput struct {
	// User is the created account, or for alreadyExists the other account Microsoft returns for the name, if any.
	User User `json:"user"`
	// WasAlreadyCreated reports that an earlier attempt or Step execution with the same key created the account.
	WasAlreadyCreated bool `json:"wasAlreadyCreated"`
}

// CreateUserOperation implements the createUser connector Mutation.
type CreateUserOperation struct{ client *Client }

type createUserRequest struct {
	AccountEnabled                bool                      `json:"accountEnabled"`
	DisplayName                   string                    `json:"displayName"`
	GivenName                     string                    `json:"givenName,omitempty"`
	Surname                       string                    `json:"surname,omitempty"`
	MailNickname                  string                    `json:"mailNickname"`
	UserPrincipalName             string                    `json:"userPrincipalName"`
	JobTitle                      string                    `json:"jobTitle,omitempty"`
	Department                    string                    `json:"department,omitempty"`
	UsageLocation                 string                    `json:"usageLocation,omitempty"`
	PasswordProfile               createUserPasswordProfile `json:"passwordProfile"`
	OnPremisesExtensionAttributes map[string]string         `json:"onPremisesExtensionAttributes"`
}

type createUserPasswordProfile struct {
	ForceChangePasswordNextSignIn bool   `json:"forceChangePasswordNextSignIn"`
	Password                      string `json:"password"`
}

var createUserReadBackFailureBranches = failureBranches{
	notFound: CreateUserBranchProviderRejected, rejected: CreateUserBranchProviderRejected,
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

// Invoke creates the account with the creation key in the connection's extension attribute.
// Microsoft Graph has no idempotency key and answers 400 for a taken user principal name, so
// every 400 reads the name back: an account carrying the key is this Step's account, so a
// repeated or concurrent attempt converges on it instead of creating another. Because of that
// read-back, transport failures, 5xx responses, and unusable success responses are retried.
func (operation CreateUserOperation) Invoke(call sdkgo.Call, input CreateUserInput) sdkgo.MutationAttempt[CreateUserOutput] {
	const operationID = "createUser"
	creationKey := string(call.IdempotencyKey)
	request, err := buildCreateUserRequest(input, operation.client.creationKeyAttribute, creationKey)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, failure, sdkgo.Receipt{})
	}
	if request.PasswordProfile.Password, err = generateInitialPassword(); err != nil {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, failurePointer(sdkgo.FailureLocalDefect, operationID, "initial password could not be generated"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, &credential, operationID, graphRequest{method: http.MethodPost, path: "/users", payload: request})
	switch exchange.outcome {
	case exchangeSucceeded:
		user, _, err := decodeUser(exchange.response.body, false)
		if err != nil || !strings.EqualFold(user.UserPrincipalName, request.UserPrincipalName) {
			return sdkgo.NewMutationRetry[CreateUserOutput](graphFailure(sdkgo.FailureProtocol, operationID, "create response is unusable; the retry reads the account back"), 0)
		}
		return sdkgo.NewMutationBranch(CreateUserBranchCreated, CreateUserOutput{User: completeCreatedUser(user, request)}, nil, operation.client.receipt(call, exchange.response, user.ID))
	case exchangeBadRequest:
		return operation.convergeOnExistingUserPrincipalName(call, &credential, request.UserPrincipalName, creationKey, exchange)
	case exchangeInvalidResponse:
		return sdkgo.NewMutationRetry[CreateUserOutput](graphFailure(exchange.failure.Kind, operationID, "create response is unusable; the retry reads the account back"), 0)
	case exchangeNotFound, exchangeRejected:
		failure := exchange.failure
		return sdkgo.NewMutationBranch(CreateUserBranchProviderRejected, CreateUserOutput{}, &failure, operation.client.receipt(call, exchange.response, ""))
	default:
		return mutationAttemptFromExchange[CreateUserOutput](operation.client, call, exchange, createUserReadBackFailureBranches)
	}
}

// convergeOnExistingUserPrincipalName reads the name behind a 400; an unreadable name is retried within the window.
func (operation CreateUserOperation) convergeOnExistingUserPrincipalName(
	call sdkgo.Call, credential *Credentials, userPrincipalName string, creationKey string, createExchange graphExchange,
) sdkgo.MutationAttempt[CreateUserOutput] {
	const operationID = "createUser"
	read := operation.client.readUser(call, credential, operationID, userPrincipalName, creationKeySelectedProperties)
	switch read.outcome {
	case exchangeSucceeded:
		receipt := operation.client.receipt(call, read.response, "")
		user, resource, err := decodeUser(read.response.body, true)
		if err != nil {
			return sdkgo.NewMutationBranch(CreateUserBranchInvalidResponse, CreateUserOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid account: "+err.Error()), receipt)
		}
		receipt.ProviderObjectID = user.ID
		if resource.hasCreationKey(operation.client.creationKeyAttribute, creationKey) {
			return sdkgo.NewMutationBranch(CreateUserBranchCreated, CreateUserOutput{User: user, WasAlreadyCreated: true}, nil, receipt)
		}
		return sdkgo.NewMutationBranch(CreateUserBranchAlreadyExists, CreateUserOutput{User: user},
			failurePointer(sdkgo.FailureConflict, operationID, "user principal name belongs to an account this Step did not create"), receipt)
	case exchangeNotFound:
		if operation.client.isWithinPropagationWindow(call) {
			return sdkgo.NewMutationRetry[CreateUserOutput](graphFailure(sdkgo.FailureAvailability, operationID, "Microsoft Graph rejected the account but does not return the user principal name yet"), 0)
		}
		if hasObjectConflictDetail(createExchange.response.body) {
			return sdkgo.NewMutationBranch(CreateUserBranchAlreadyExists, CreateUserOutput{},
				failurePointer(sdkgo.FailureConflict, operationID, "user principal name or mail alias belongs to an object outside the user directory, such as a deleted account"),
				operation.client.receipt(call, read.response, ""))
		}
		failure := createExchange.failure
		return sdkgo.NewMutationBranch(CreateUserBranchProviderRejected, CreateUserOutput{}, &failure, operation.client.receipt(call, createExchange.response, ""))
	default:
		return mutationAttemptFromExchange[CreateUserOutput](operation.client, call, read, createUserReadBackFailureBranches)
	}
}

func buildCreateUserRequest(input CreateUserInput, attribute CreationKeyAttribute, creationKey string) (createUserRequest, error) {
	userPrincipalName, localPart, err := validateNewUserPrincipalName(input.UserPrincipalName)
	if err != nil {
		return createUserRequest{}, err
	}
	if !creationKeyPattern.MatchString(creationKey) {
		return createUserRequest{}, errors.New("provisioningKey must be 1 to 128 letters, digits, or . _ : - characters")
	}
	givenName, err := validateText("givenName", input.GivenName, maxNamePartLength)
	if err != nil {
		return createUserRequest{}, err
	}
	surname, err := validateText("surname", input.Surname, maxNamePartLength)
	if err != nil {
		return createUserRequest{}, err
	}
	displayName, err := validateText("displayName", input.DisplayName, maxDisplayNameLength)
	if err != nil {
		return createUserRequest{}, err
	}
	if displayName == "" {
		displayName = strings.TrimSpace(givenName + " " + surname)
	}
	if displayName == "" {
		return createUserRequest{}, errors.New("displayName, givenName, or surname is required")
	}
	jobTitle, err := validateText("jobTitle", input.JobTitle, maxJobTitleLength)
	if err != nil {
		return createUserRequest{}, err
	}
	department, err := validateText("department", input.Department, maxDepartmentLength)
	if err != nil {
		return createUserRequest{}, err
	}
	mailNickname := strings.TrimSpace(input.MailNickname)
	if mailNickname == "" {
		mailNickname = localPart
	}
	if len(mailNickname) > maxMailNicknameLength || !mailNicknamePattern.MatchString(mailNickname) {
		return createUserRequest{}, errors.New("mailNickname must be at most 64 letters, digits, or alias punctuation without spaces or @")
	}
	usageLocation := strings.TrimSpace(input.UsageLocation)
	if usageLocation != "" && !usageLocationPattern.MatchString(usageLocation) {
		return createUserRequest{}, errors.New("usageLocation must be a two-letter uppercase ISO 3166 country code such as US")
	}
	return createUserRequest{
		AccountEnabled: true, DisplayName: displayName, GivenName: givenName, Surname: surname,
		MailNickname: mailNickname, UserPrincipalName: userPrincipalName, JobTitle: jobTitle, Department: department,
		UsageLocation: usageLocation,
		// Nobody receives the generated password, so the next password sign-in must set a new one.
		PasswordProfile:               createUserPasswordProfile{ForceChangePasswordNextSignIn: true},
		OnPremisesExtensionAttributes: map[string]string{string(attribute): creationKeyValue(creationKey)},
	}, nil
}

// validateNewUserPrincipalName applies Microsoft Entra's documented user name policy and returns the name and its local part.
func validateNewUserPrincipalName(value string) (string, string, error) {
	userPrincipalName := strings.TrimSpace(value)
	invalid := errors.New("userPrincipalName must be alias@domain with at most 64 letters, digits, or ' . - _ ! # ^ ~ before the @ and a verified domain after it")
	localPart, domain, hasAt := strings.Cut(userPrincipalName, "@")
	if !hasAt || strings.Contains(domain, "@") || len(userPrincipalName) > maxUserPrincipalNameLength {
		return "", "", invalid
	}
	if len(localPart) > maxUPNLocalPartLength || !userPrincipalNameLocalPartPattern.MatchString(localPart) ||
		strings.HasPrefix(localPart, ".") || strings.HasSuffix(localPart, ".") {
		return "", "", invalid
	}
	if len(domain) > maxUPNDomainLength || !domainNamePattern.MatchString(domain) {
		return "", "", invalid
	}
	return userPrincipalName, localPart, nil
}

// completeCreatedUser fills properties the 201 body omits from the request Microsoft accepted.
func completeCreatedUser(user User, request createUserRequest) User {
	user.IsAccountEnabled = request.AccountEnabled
	if user.MailNickname == "" {
		user.MailNickname = request.MailNickname
	}
	if user.Department == "" {
		user.Department = request.Department
	}
	if user.UsageLocation == "" {
		user.UsageLocation = request.UsageLocation
	}
	return user
}

func creationKeyValue(key string) string { return creationKeyPrefix + key }

// hasObjectConflictDetail reports Microsoft Entra's ObjectConflict detail code; message text is never read.
func hasObjectConflictDetail(body []byte) bool {
	for _, code := range providerhttp.ReadErrorTokens(body, errorDetailCodePointers) {
		if code == objectConflictDetailCode {
			return true
		}
	}
	return false
}

// generateInitialPassword returns a random password with every Microsoft Entra character class
// that lives only in the request body.
func generateInitialPassword() (string, error) {
	alphabet := strings.Join(initialPasswordCharacterClasses, "")
	password := make([]byte, 0, initialPasswordLength)
	for _, characterClass := range initialPasswordCharacterClasses {
		character, err := randomCharacter(characterClass)
		if err != nil {
			return "", err
		}
		password = append(password, character)
	}
	for len(password) < initialPasswordLength {
		character, err := randomCharacter(alphabet)
		if err != nil {
			return "", err
		}
		password = append(password, character)
	}
	for index := len(password) - 1; index > 0; index-- {
		swapIndex, err := rand.Int(rand.Reader, big.NewInt(int64(index+1)))
		if err != nil {
			return "", err
		}
		password[index], password[swapIndex.Int64()] = password[swapIndex.Int64()], password[index]
	}
	return string(password), nil
}

func randomCharacter(characters string) (byte, error) {
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(characters))))
	if err != nil {
		return 0, err
	}
	return characters[index.Int64()], nil
}
