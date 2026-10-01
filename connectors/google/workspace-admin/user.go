// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

import (
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// creationKeyExternalIDType is the custom external ID type that records createUser's stable key on the account.
	creationKeyExternalIDType = "dexIdempotencyKey"
	maxDirectoryKeyLength     = 254
)

var (
	// userIDPattern accepts Google's numeric unique user IDs.
	userIDPattern = regexp.MustCompile(`^[0-9]{1,64}$`)
	// groupIDPattern accepts Google's opaque lowercase alphanumeric group IDs.
	groupIDPattern = regexp.MustCompile(`^[0-9a-z]{1,64}$`)
)

// User is the bounded account record every user operation returns. It omits
// passwords, recovery details, phones, addresses, and custom schema fields.
type User struct {
	// ID is Google's stable unique user ID; prefer it over the address as a durable key, because addresses can be renamed.
	ID string `json:"id"`
	// PrimaryEmail is the account's sign-in address.
	PrimaryEmail string `json:"primaryEmail"`
	// Name holds the given, family, and full names.
	Name UserName `json:"name"`
	// OrgUnitPath is the account's organizational unit, such as /Engineering; / is the top level.
	OrgUnitPath string `json:"orgUnitPath"`
	// IsSuspended reports whether the account is suspended and cannot sign in.
	IsSuspended bool `json:"isSuspended"`
	// SuspensionReason is Google's reason, such as ADMIN, when IsSuspended is true.
	SuspensionReason string `json:"suspensionReason,omitempty"`
	// IsArchived reports whether the account is archived.
	IsArchived bool `json:"isArchived"`
	// IsAdmin reports super administrator privileges.
	IsAdmin bool `json:"isAdmin"`
	// IsDelegatedAdmin reports delegated administrator privileges.
	IsDelegatedAdmin bool `json:"isDelegatedAdmin"`
	// IsEnrolledIn2SV reports enrollment in 2-Step Verification.
	IsEnrolledIn2SV bool `json:"isEnrolledIn2Sv"`
	// IsEnforcedIn2SV reports that 2-Step Verification is enforced for the account.
	IsEnforcedIn2SV bool `json:"isEnforcedIn2Sv"`
	// ChangePasswordAtNextLogin reports that Google asks for a new password at the next password sign-in.
	ChangePasswordAtNextLogin bool `json:"changePasswordAtNextLogin"`
	// IsMailboxSetup reports whether Google has finished setting up the Gmail mailbox.
	IsMailboxSetup bool `json:"isMailboxSetup"`
	// Aliases lists the account's editable alias addresses.
	Aliases []string `json:"aliases,omitempty"`
	// CustomerID is the Workspace customer account that owns the user.
	CustomerID string `json:"customerId,omitempty"`
	// CreationTime is the account creation time exactly as Google reports it, normally RFC 3339.
	CreationTime string `json:"creationTime,omitempty"`
	// LastLoginTime is the last sign-in time exactly as Google reports it; Google may report an epoch time for never.
	LastLoginTime string `json:"lastLoginTime,omitempty"`
}

// UserName is an account's name.
type UserName struct {
	// GivenName is the first name.
	GivenName string `json:"givenName"`
	// FamilyName is the last name.
	FamilyName string `json:"familyName"`
	// FullName is Google's read-only concatenation of the two.
	FullName string `json:"fullName,omitempty"`
}

type directoryUser struct {
	ID           string `json:"id"`
	PrimaryEmail string `json:"primaryEmail"`
	Name         struct {
		GivenName  string `json:"givenName"`
		FamilyName string `json:"familyName"`
		FullName   string `json:"fullName"`
	} `json:"name"`
	OrgUnitPath               string          `json:"orgUnitPath"`
	Suspended                 bool            `json:"suspended"`
	SuspensionReason          string          `json:"suspensionReason"`
	Archived                  bool            `json:"archived"`
	IsAdmin                   bool            `json:"isAdmin"`
	IsDelegatedAdmin          bool            `json:"isDelegatedAdmin"`
	IsEnrolledIn2SV           bool            `json:"isEnrolledIn2Sv"`
	IsEnforcedIn2SV           bool            `json:"isEnforcedIn2Sv"`
	ChangePasswordAtNextLogin bool            `json:"changePasswordAtNextLogin"`
	IsMailboxSetup            bool            `json:"isMailboxSetup"`
	Aliases                   []string        `json:"aliases"`
	CustomerID                string          `json:"customerId"`
	CreationTime              string          `json:"creationTime"`
	LastLoginTime             string          `json:"lastLoginTime"`
	ExternalIDs               json.RawMessage `json:"externalIds"`
}

type directoryExternalID struct {
	Type       string `json:"type"`
	CustomType string `json:"customType"`
	Value      string `json:"value"`
}

// decodeUser validates one untrusted Directory API user resource.
func decodeUser(body []byte) (User, directoryUser, error) {
	var resource directoryUser
	if err := json.Unmarshal(body, &resource); err != nil {
		return User{}, directoryUser{}, errors.New("account is not valid JSON")
	}
	user, err := convertUser(resource)
	return user, resource, err
}

func convertUser(resource directoryUser) (User, error) {
	if !userIDPattern.MatchString(resource.ID) {
		return User{}, errors.New("account lacks a valid unique ID")
	}
	if !isBareEmailAddress(resource.PrimaryEmail) {
		return User{}, errors.New("account lacks a valid primary address")
	}
	return User{
		ID: resource.ID, PrimaryEmail: resource.PrimaryEmail,
		Name:        UserName{GivenName: resource.Name.GivenName, FamilyName: resource.Name.FamilyName, FullName: resource.Name.FullName},
		OrgUnitPath: resource.OrgUnitPath, IsSuspended: resource.Suspended, SuspensionReason: resource.SuspensionReason,
		IsArchived: resource.Archived, IsAdmin: resource.IsAdmin, IsDelegatedAdmin: resource.IsDelegatedAdmin,
		IsEnrolledIn2SV: resource.IsEnrolledIn2SV, IsEnforcedIn2SV: resource.IsEnforcedIn2SV,
		ChangePasswordAtNextLogin: resource.ChangePasswordAtNextLogin, IsMailboxSetup: resource.IsMailboxSetup,
		Aliases: resource.Aliases, CustomerID: resource.CustomerID,
		CreationTime: resource.CreationTime, LastLoginTime: resource.LastLoginTime,
	}, nil
}

// hasCreationKey reports whether createUser recorded key on the account. An
// unreadable externalIds value carries no key.
func (resource directoryUser) hasCreationKey(key string) bool {
	var externalIDs []directoryExternalID
	if len(resource.ExternalIDs) == 0 || json.Unmarshal(resource.ExternalIDs, &externalIDs) != nil {
		return false
	}
	for _, externalID := range externalIDs {
		if externalID.Type == "custom" && externalID.CustomType == creationKeyExternalIDType && externalID.Value == key {
			return true
		}
	}
	return false
}

// validateUserKey accepts a primary or alias address or a numeric unique user ID.
func validateUserKey(value string) (string, error) {
	userKey := strings.TrimSpace(value)
	if userIDPattern.MatchString(userKey) || isBareEmailAddress(userKey) {
		return userKey, nil
	}
	return "", errors.New("userKey must be one bare email address or a numeric user ID")
}

// validateGroupKey accepts a group address, a group alias, or an opaque group ID.
func validateGroupKey(value string) (string, error) {
	groupKey := strings.TrimSpace(value)
	if groupIDPattern.MatchString(groupKey) || isBareEmailAddress(groupKey) {
		return groupKey, nil
	}
	return "", errors.New("groupKey must be one bare group email address or a group ID")
}

// isBareEmailAddress reports one address without a display name, comments, or surrounding text.
func isBareEmailAddress(value string) bool {
	if value == "" || len(value) > maxDirectoryKeyLength || strings.ContainsAny(value, "<>\"()[]\\,; \t\r\n/?#%") {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && strings.Count(value, "@") == 1
}

// validateDisplayName accepts a trimmed name of 1 to 60 characters without control characters.
func validateDisplayName(field string, value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", errors.New(field + " is required")
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 60 {
		return "", errors.New(field + " must be valid text of at most 60 characters")
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", errors.New(field + " cannot contain control characters")
		}
	}
	return name, nil
}

func userPath(userKey string) string { return "/users/" + url.PathEscape(userKey) }

func groupMembersPath(groupKey string) string {
	return "/groups/" + url.PathEscape(groupKey) + "/members"
}

func groupMemberPath(groupKey string, memberKey string) string {
	return groupMembersPath(groupKey) + "/" + url.PathEscape(memberKey)
}
