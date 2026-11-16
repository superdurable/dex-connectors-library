// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ProfileField names one tracked profile claim in ProfileChangedEvent.ChangedFields. Each value is the
// ProfileClaims JSON field; the comment names the LinkedIn UserInfo claim it comes from.
type ProfileField string

const (
	// ProfileFieldName is the full name, from the name claim.
	ProfileFieldName ProfileField = "name"
	// ProfileFieldGivenName is the first name, from the given_name claim.
	ProfileFieldGivenName ProfileField = "givenName"
	// ProfileFieldFamilyName is the last name, from the family_name claim.
	ProfileFieldFamilyName ProfileField = "familyName"
	// ProfileFieldEmail is the primary email address, from the email claim.
	ProfileFieldEmail ProfileField = "email"
	// ProfileFieldEmailVerified is whether LinkedIn verified the primary email, from the email_verified claim.
	ProfileFieldEmailVerified ProfileField = "emailVerified"
	// ProfileFieldPictureURL is the profile picture, from the picture claim. Only the picture's address
	// without its query counts, so LinkedIn re-signing the same picture's URL is not a change.
	ProfileFieldPictureURL ProfileField = "pictureUrl"
	// ProfileFieldLocale is the member's locale, from the locale claim.
	ProfileFieldLocale ProfileField = "locale"
)

// ProfileClaims is the connected member's bounded LinkedIn OpenID Connect UserInfo profile. Unlike
// AuthenticatedProfile it keeps an unverified or absent email, since a change of verification is itself
// a profile change.
type ProfileClaims struct {
	// Subject is LinkedIn's sub claim: the member's identifier, stable for this LinkedIn app.
	Subject string `json:"subject"`
	// Name is the full name.
	Name string `json:"name,omitempty"`
	// GivenName is the first name.
	GivenName string `json:"givenName,omitempty"`
	// FamilyName is the last name.
	FamilyName string `json:"familyName,omitempty"`
	// Email is the lower-cased primary email address; empty when LinkedIn returns none or an invalid one.
	Email string `json:"email,omitempty"`
	// EmailVerified reports LinkedIn's email_verified claim; false when LinkedIn omits it.
	EmailVerified bool `json:"emailVerified"`
	// PictureURL is the HTTPS profile picture URL as LinkedIn returned it, including any signed query.
	PictureURL string `json:"pictureUrl,omitempty"`
	// Locale is the member's locale, such as en-US.
	Locale string `json:"locale,omitempty"`
}

// ProfileChangedTriggerConfiguration is the profileChanged binding configuration. The binding always
// watches the connected member's own profile, so it has no field and Dex Web shows none.
type ProfileChangedTriggerConfiguration struct{}

// Validate accepts the empty configuration.
func (ProfileChangedTriggerConfiguration) Validate() error { return nil }

// ProfileChangedEvent is one change of the connected member's own profile claims between two polls.
type ProfileChangedEvent struct {
	// Subject is the member whose profile changed; the connection's own member, never another one.
	Subject string `json:"subject"`
	// Previous is the profile the previous poll returned.
	Previous ProfileClaims `json:"previous"`
	// Current is the profile this poll returned.
	Current ProfileClaims `json:"current"`
	// ChangedFields lists the claims that differ, in the order of the ProfileField constants.
	ChangedFields []ProfileField `json:"changedFields"`
}

// trackedProfile is what a profile hash covers; the picture drops its query, which LinkedIn re-signs.
type trackedProfile struct {
	Name          string `json:"name"`
	GivenName     string `json:"givenName"`
	FamilyName    string `json:"familyName"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	Picture       string `json:"picture"`
	Locale        string `json:"locale"`
}

func (profile ProfileClaims) tracked() trackedProfile {
	return trackedProfile{
		Name: profile.Name, GivenName: profile.GivenName, FamilyName: profile.FamilyName, Email: profile.Email,
		EmailVerified: profile.EmailVerified, Picture: pictureIdentity(profile.PictureURL), Locale: profile.Locale,
	}
}

// profileHash is the first 16 bytes of SHA-256 over the tracked claims, hex encoded.
func profileHash(profile ProfileClaims) (string, error) {
	encoded, err := json.Marshal(profile.tracked())
	if err != nil {
		return "", fmt.Errorf("encode LinkedIn profile: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:16]), nil
}

// changedProfileFields lists the tracked claims that differ between two profiles.
func changedProfileFields(previous, current ProfileClaims) []ProfileField {
	before, after := previous.tracked(), current.tracked()
	var changed []ProfileField
	for _, field := range []struct {
		name      ProfileField
		isChanged bool
	}{
		{ProfileFieldName, before.Name != after.Name},
		{ProfileFieldGivenName, before.GivenName != after.GivenName},
		{ProfileFieldFamilyName, before.FamilyName != after.FamilyName},
		{ProfileFieldEmail, before.Email != after.Email},
		{ProfileFieldEmailVerified, before.EmailVerified != after.EmailVerified},
		{ProfileFieldPictureURL, before.Picture != after.Picture},
		{ProfileFieldLocale, before.Locale != after.Locale},
	} {
		if field.isChanged {
			changed = append(changed, field.name)
		}
	}
	return changed
}

// pictureIdentity drops the query and fragment, which carry LinkedIn's expiring signature.
func pictureIdentity(pictureURL string) string {
	parsed, err := url.Parse(pictureURL)
	if err != nil {
		return pictureURL
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}

// profileChangeID includes the delivered-change count, so a profile edited back and forth gets new IDs.
func profileChangeID(subject, previousHash, currentHash string, sequence int64) string {
	parts := []string{subject, previousHash, currentHash, strconv.FormatInt(sequence, 10)}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "linkedin-profile-" + hex.EncodeToString(digest[:16])
}

func newProfileChangedTriggerEvent(previousHash, currentHash string, sequence int64, previous, current ProfileClaims, observedAt time.Time) sdkgo.TriggerEvent[ProfileChangedEvent] {
	return sdkgo.TriggerEvent[ProfileChangedEvent]{
		ID: profileChangeID(current.Subject, previousHash, currentHash, sequence), OccurredAt: observedAt.UTC(),
		Payload: ProfileChangedEvent{
			Subject: current.Subject, Previous: previous, Current: current, ChangedFields: changedProfileFields(previous, current),
		},
	}
}

// profileClaimsFromUserInfo bounds UserInfo claims the way GetAuthenticatedProfile does.
func profileClaimsFromUserInfo(claims userInfo) (ProfileClaims, bool) {
	subject := strings.TrimSpace(claims.Subject)
	if !validSubject(subject) {
		return ProfileClaims{}, false
	}
	return ProfileClaims{
		Subject: subject, Name: boundedString(claims.Name, 256),
		GivenName: boundedString(claims.GivenName, 128), FamilyName: boundedString(claims.FamilyName, 128),
		Email: canonicalEmailAddress(claims.Email), EmailVerified: claims.EmailVerified,
		PictureURL: safeURL(claims.Picture), Locale: localeStringFromLinkedInClaim(claims.Locale),
	}, true
}

func profileChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("linkedin profileChanged configuration: %w", err)
}
