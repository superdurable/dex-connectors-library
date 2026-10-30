// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// userSelectedProperties bounds every user read; accountEnabled is returned only when selected.
	userSelectedProperties = "id,userPrincipalName,displayName,givenName,surname,mail,mailNickname,accountEnabled," +
		"userType,jobTitle,department,usageLocation,onPremisesSyncEnabled,createdDateTime,signInSessionsValidFromDateTime"
	// creationKeySelectedProperties adds the extension attributes that carry createUser's key.
	creationKeySelectedProperties    = userSelectedProperties + ",onPremisesExtensionAttributes"
	maxUserPrincipalNameLookupLength = 256
)

var (
	// objectIDPattern accepts Microsoft Entra object IDs, which are GUIDs.
	objectIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	// userPrincipalNameLookupPattern accepts member and guest (#EXT#) sign-in names without separators or spaces.
	userPrincipalNameLookupPattern = regexp.MustCompile(`^[A-Za-z0-9'.\-_!#^~$&+=]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
)

// User is the bounded account record every user operation returns. It never
// includes passwords, authentication methods, phones, addresses, or extension attributes.
type User struct {
	// ID is the account's object ID, a GUID; prefer it over the user principal name as a durable key, because sign-in names can change.
	ID string `json:"id"`
	// UserPrincipalName is the account's sign-in name, such as ada.lovelace@contoso.com.
	UserPrincipalName string `json:"userPrincipalName"`
	// DisplayName is the address-book name.
	DisplayName string `json:"displayName,omitempty"`
	// GivenName is the first name.
	GivenName string `json:"givenName,omitempty"`
	// Surname is the family name.
	Surname string `json:"surname,omitempty"`
	// Mail is the primary SMTP address, which Microsoft sets once a mailbox exists.
	Mail string `json:"mail,omitempty"`
	// MailNickname is the mail alias.
	MailNickname string `json:"mailNickname,omitempty"`
	// IsAccountEnabled reports whether the account can sign in.
	IsAccountEnabled bool `json:"isAccountEnabled"`
	// UserType is Member or Guest.
	UserType string `json:"userType,omitempty"`
	// JobTitle is the job title.
	JobTitle string `json:"jobTitle,omitempty"`
	// Department is the department.
	Department string `json:"department,omitempty"`
	// UsageLocation is the two-letter country code Microsoft requires before licenses are assigned.
	UsageLocation string `json:"usageLocation,omitempty"`
	// IsOnPremisesSyncEnabled reports that on-premises Active Directory owns the account, so Graph cannot change most of its properties.
	IsOnPremisesSyncEnabled bool `json:"isOnPremisesSyncEnabled"`
	// CreatedDateTime is the creation time exactly as Microsoft reports it, in ISO 8601 UTC; it is empty for some older accounts.
	CreatedDateTime string `json:"createdDateTime,omitempty"`
	// SignInSessionsValidFromDateTime is the time before which every refresh token and session cookie is invalid, as Microsoft reports it.
	SignInSessionsValidFromDateTime string `json:"signInSessionsValidFromDateTime,omitempty"`
}

type graphUser struct {
	ID                              string                   `json:"id"`
	UserPrincipalName               string                   `json:"userPrincipalName"`
	DisplayName                     string                   `json:"displayName"`
	GivenName                       string                   `json:"givenName"`
	Surname                         string                   `json:"surname"`
	Mail                            string                   `json:"mail"`
	MailNickname                    string                   `json:"mailNickname"`
	AccountEnabled                  *bool                    `json:"accountEnabled"`
	UserType                        string                   `json:"userType"`
	JobTitle                        string                   `json:"jobTitle"`
	Department                      string                   `json:"department"`
	UsageLocation                   string                   `json:"usageLocation"`
	OnPremisesSyncEnabled           *bool                    `json:"onPremisesSyncEnabled"`
	CreatedDateTime                 string                   `json:"createdDateTime"`
	SignInSessionsValidFromDateTime string                   `json:"signInSessionsValidFromDateTime"`
	OnPremisesExtensionAttributes   graphExtensionAttributes `json:"onPremisesExtensionAttributes"`
}

// graphExtensionAttributes holds extensionAttribute1 to 15; Microsoft reports unset values as null.
type graphExtensionAttributes map[string]*string

// decodeUser validates one untrusted user; a create's 201 body omits accountEnabled, so it may be optional.
func decodeUser(body []byte, isAccountEnabledRequired bool) (User, graphUser, error) {
	var resource graphUser
	if err := json.Unmarshal(body, &resource); err != nil {
		return User{}, graphUser{}, errors.New("account is not valid JSON")
	}
	user, err := convertUser(resource, isAccountEnabledRequired)
	return user, resource, err
}

func convertUser(resource graphUser, isAccountEnabledRequired bool) (User, error) {
	if !objectIDPattern.MatchString(resource.ID) {
		return User{}, errors.New("account lacks a valid object ID")
	}
	if !isUserPrincipalNameForLookup(resource.UserPrincipalName) {
		return User{}, errors.New("account lacks a valid user principal name")
	}
	if isAccountEnabledRequired && resource.AccountEnabled == nil {
		return User{}, errors.New("account lacks accountEnabled; the token may lack User.Read.All")
	}
	return User{
		ID: strings.ToLower(resource.ID), UserPrincipalName: resource.UserPrincipalName, DisplayName: resource.DisplayName,
		GivenName: resource.GivenName, Surname: resource.Surname, Mail: resource.Mail, MailNickname: resource.MailNickname,
		IsAccountEnabled: resource.AccountEnabled != nil && *resource.AccountEnabled, UserType: resource.UserType,
		JobTitle: resource.JobTitle, Department: resource.Department, UsageLocation: resource.UsageLocation,
		IsOnPremisesSyncEnabled: resource.OnPremisesSyncEnabled != nil && *resource.OnPremisesSyncEnabled,
		CreatedDateTime:         resource.CreatedDateTime, SignInSessionsValidFromDateTime: resource.SignInSessionsValidFromDateTime,
	}, nil
}

// hasCreationKey reports whether createUser recorded key in attribute on the account.
func (resource graphUser) hasCreationKey(attribute CreationKeyAttribute, key string) bool {
	value := resource.OnPremisesExtensionAttributes[string(attribute)]
	return value != nil && *value == creationKeyValue(key)
}

// validateUserKey accepts an object ID or a user principal name.
func validateUserKey(value string) (string, error) {
	userKey := strings.TrimSpace(value)
	if objectIDPattern.MatchString(userKey) {
		return strings.ToLower(userKey), nil
	}
	if isUserPrincipalNameForLookup(userKey) {
		return userKey, nil
	}
	return "", errors.New("userKey must be an object ID or one user principal name such as ada@contoso.com")
}

// validateObjectID accepts one Microsoft Entra object ID and returns it in lowercase.
func validateObjectID(field string, value string) (string, error) {
	objectID := strings.TrimSpace(value)
	if !objectIDPattern.MatchString(objectID) {
		return "", errors.New(field + " must be an object ID, a GUID such as 00000000-0000-0000-0000-000000000000")
	}
	return strings.ToLower(objectID), nil
}

// isUserPrincipalNameForLookup reports one sign-in name, including a guest's #EXT# name, safe to place in a path.
func isUserPrincipalNameForLookup(value string) bool {
	return len(value) <= maxUserPrincipalNameLookupLength && strings.Count(value, "@") == 1 && userPrincipalNameLookupPattern.MatchString(value)
}

// validateText accepts trimmed optional text of at most maxRunes characters without control characters.
func validateText(field string, value string, maxRunes int) (string, error) {
	text := strings.TrimSpace(value)
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxRunes {
		return "", errors.New(field + " must be valid text of at most " + strconv.Itoa(maxRunes) + " characters")
	}
	if strings.ContainsFunc(text, unicode.IsControl) {
		return "", errors.New(field + " cannot contain control characters")
	}
	return text, nil
}

// userPath addresses a user; Microsoft requires /users('...') for a name starting with $.
func userPath(userKey string) string {
	if strings.HasPrefix(userKey, "$") {
		return "/users('" + url.PathEscape(strings.ReplaceAll(userKey, "'", "''")) + "')"
	}
	return "/users/" + url.PathEscape(userKey)
}

func groupPath(groupID string) string { return "/groups/" + groupID }

func groupMembersPath(groupID string) string { return groupPath(groupID) + "/members" }

// groupMemberReferencePath always ends in /$ref: without it, Microsoft deletes the member object itself.
func groupMemberReferencePath(groupID string, userID string) string {
	return groupMembersPath(groupID) + "/" + userID + "/$ref"
}
