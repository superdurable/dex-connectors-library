// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Token request settings that oauth2 and clientCredentials declarations name.
const (
	ClientSecretPostAuthMethod  = "client_secret_post"
	ClientSecretBasicAuthMethod = "client_secret_basic"
	FormTokenRequestEncoding    = "form"
	JSONTokenRequestEncoding    = "json"
)

// Scope sets that an OAuth refresh grant checks its response against.
const (
	MethodScopes = "scopes"
	UserScopes   = "userScopes"
	// ScopeSeparatorsWhitespace splits reported scopes like Go strings.Fields.
	ScopeSeparatorsWhitespace = "whitespace"
	// ScopeSeparatorsWhitespaceAndComma splits reported scopes on ASCII spaces and commas.
	ScopeSeparatorsWhitespaceAndComma = "whitespaceAndComma"
)

// keepMissingExpiry is the scalar form of MissingExpiry that keeps a token whose response has no expires_in.
const keepMissingExpiry = "keep"

const (
	// StoredMissingExpiryKeep preserves a present access token without recorded expiry.
	StoredMissingExpiryKeep = "keep"
	// StoredMissingExpiryRefresh renews a present access token without recorded expiry.
	StoredMissingExpiryRefresh = "refresh"
)

// OAuthRefresh declares how a host refreshes an OAuth method's access token with the stored refresh token. The
// method's tokenEndpoint, tokenEndpointAuthMethod, tokenRequestEncoding, client credentials and credentialMappings
// apply to the refresh request and response too.
type OAuthRefresh struct {
	// AcceptsMissingTokenType permits a successful response without token_type.
	AcceptsMissingTokenType bool `yaml:"acceptsMissingTokenType,omitempty" json:"acceptsMissingTokenType,omitempty"`
	// AcceptedTokenTypes replaces the default Bearer token type for all grants unless a grant overrides it.
	AcceptedTokenTypes []string `yaml:"acceptedTokenTypes,omitempty" json:"acceptedTokenTypes,omitempty"`
	// ExactScopes requires the returned scope set to equal the declared scope set.
	ExactScopes bool `yaml:"exactScopes,omitempty" json:"exactScopes,omitempty"`
	// ScopeSeparators selects whitespace or space-and-comma parsing for reported scopes; empty means whitespace.
	ScopeSeparators string `yaml:"scopeSeparators,omitempty" json:"scopeSeparators,omitempty"`
	// AllowsPublicClient permits refresh without a stored client secret.
	AllowsPublicClient bool `yaml:"allowsPublicClient,omitempty" json:"allowsPublicClient,omitempty"`
	// RequiredScopes overrides the authorization scopes checked on refresh responses.
	RequiredScopes []string `yaml:"requiredScopes,omitempty" json:"requiredScopes,omitempty"`
	// AcceptedScopeSets replaces the required scopes with any one of these complete accepted sets.
	AcceptedScopeSets [][]string `yaml:"acceptedScopeSets,omitempty" json:"acceptedScopeSets,omitempty"`
	// TerminalErrorCodes are the OAuth error codes, in a response below HTTP 500, after which the refresh token can
	// never succeed again, so the connection needs reauthorization. Empty means invalid_grant.
	TerminalErrorCodes []string `yaml:"terminalErrorCodes,omitempty" json:"terminalErrorCodes,omitempty"`
	// MissingExpiry decides the expiry of a refreshed token whose response has no expires_in. Nil keeps it.
	MissingExpiry *MissingExpiry `yaml:"missingExpiry,omitempty" json:"missingExpiry,omitempty"`
	// StoredMissingExpiry refreshes an existing token without recorded expiry when set to refresh; empty keeps it.
	StoredMissingExpiry string `yaml:"storedMissingExpiry,omitempty" json:"storedMissingExpiry,omitempty"`
	// AdditionalParameters are fixed, non-secret parameters added to every refresh request, such as a scope that a
	// provider expects again. They cannot replace a grant or client parameter.
	AdditionalParameters map[string]string `yaml:"additionalParameters,omitempty" json:"additionalParameters,omitempty"`
	// Grants lists the refresh tokens of a method that stores several, each refreshed with its own request. Empty
	// means one grant: the credential that credentialMappings maps from refresh_token, with those mappings.
	Grants []OAuthRefreshGrant `yaml:"grants,omitempty" json:"grants,omitempty"`
}

// OAuthRefreshGrant is one stored refresh token and how its refresh response maps into credentials.
type OAuthRefreshGrant struct {
	// AcceptsMissingTokenType overrides the refresh-level rule for this grant.
	AcceptsMissingTokenType *bool `yaml:"acceptsMissingTokenType,omitempty" json:"acceptsMissingTokenType,omitempty"`
	// AcceptedTokenTypes replaces the refresh-level token types for this grant.
	AcceptedTokenTypes []string `yaml:"acceptedTokenTypes,omitempty" json:"acceptedTokenTypes,omitempty"`
	// ExactScopes requires the returned scope set to equal this grant's declared scope set.
	ExactScopes *bool `yaml:"exactScopes,omitempty" json:"exactScopes,omitempty"`
	// RefreshTokenCredential is the secretString credential field that holds the refresh token. A connection
	// whose field is blank skips this grant.
	RefreshTokenCredential string `yaml:"refreshTokenCredential" json:"refreshTokenCredential"`
	// CredentialMappings map this grant's refresh response into credential fields.
	CredentialMappings []OAuthCredentialMapping `yaml:"credentialMappings" json:"credentialMappings"`
	// GrantedScopes names the declared scope set that the response's granted scopes must contain: scopes when
	// empty, or userScopes.
	GrantedScopes string `yaml:"grantedScopes,omitempty" json:"grantedScopes,omitempty"`
}

// MissingExpiry decides the expiry of a token whose token response has no expires_in. In YAML and JSON it is the
// string keep, which stores the token without an expiry, or {lifetime: <duration>}.
type MissingExpiry struct {
	// Lifetime is how long such a token lasts, such as 2h. Empty keeps the token without an expiry, so a host
	// replaces it only after the provider rejects it.
	Lifetime string
}

// ClientCredentialsGrant declares how a host mints an apiKey method's access token with the OAuth client
// credentials grant (RFC 6749 section 4.4) from the stored client ID and secret.
type ClientCredentialsGrant struct {
	// AcceptsMissingTokenType permits a successful response without token_type.
	AcceptsMissingTokenType bool `yaml:"acceptsMissingTokenType,omitempty" json:"acceptsMissingTokenType,omitempty"`
	// AcceptedTokenTypes replaces the default Bearer token type.
	AcceptedTokenTypes []string `yaml:"acceptedTokenTypes,omitempty" json:"acceptedTokenTypes,omitempty"`
	// TokenEndpoint is the HTTPS token endpoint: a URL, or a value source such as a template that names a
	// credential field or setting.
	TokenEndpoint ValueSource `yaml:"tokenEndpoint" json:"tokenEndpoint"`
	// TokenEndpointAuthMethod is client_secret_post when nil, or a value source that resolves to
	// client_secret_post or client_secret_basic.
	TokenEndpointAuthMethod *ValueSource `yaml:"tokenEndpointAuthMethod,omitempty" json:"tokenEndpointAuthMethod,omitempty"`
	// TokenRequestEncoding is form when empty, or json.
	TokenRequestEncoding string `yaml:"tokenRequestEncoding,omitempty" json:"tokenRequestEncoding,omitempty"`
	// ClientIDCredential is the non-secret credential field that holds the client ID.
	ClientIDCredential string `yaml:"clientIDCredential" json:"clientIDCredential"`
	// ClientSecretCredential is the secretString credential field that holds the client secret.
	ClientSecretCredential string `yaml:"clientSecretCredential" json:"clientSecretCredential"`
	// Scopes are sent space-separated as the scope parameter. Empty sends none.
	Scopes []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	// CredentialMappings map the token response into credential fields.
	CredentialMappings []OAuthCredentialMapping `yaml:"credentialMappings" json:"credentialMappings"`
	// TerminalErrorCodes are the OAuth error codes, in a response below HTTP 500, after which the client can never
	// mint again, so the connection needs new client credentials. Empty means invalid_client.
	TerminalErrorCodes []string `yaml:"terminalErrorCodes,omitempty" json:"terminalErrorCodes,omitempty"`
	// MissingExpiry decides the expiry of a token whose response has no expires_in. Nil keeps it.
	MissingExpiry *MissingExpiry `yaml:"missingExpiry,omitempty" json:"missingExpiry,omitempty"`
	// StoredMissingExpiry refreshes an existing token without recorded expiry when set to refresh; empty keeps it.
	StoredMissingExpiry string `yaml:"storedMissingExpiry,omitempty" json:"storedMissingExpiry,omitempty"`
	// AdditionalParameters are fixed, non-secret parameters added to every request.
	AdditionalParameters map[string]string `yaml:"additionalParameters,omitempty" json:"additionalParameters,omitempty"`
}

// ServiceAccount holds the minting declaration of a serviceAccount method.
type ServiceAccount struct {
	// JWTBearer mints with a signed JWT bearer assertion.
	JWTBearer *JWTBearerGrant `yaml:"jwtBearer" json:"jwtBearer"`
}

// JWTBearerGrant declares how a host mints a serviceAccount method's access token: it signs an RS256 JWT bearer
// assertion (RFC 7523) with the stored private key and exchanges it at the token endpoint.
type JWTBearerGrant struct {
	// AcceptsMissingTokenType permits a successful response without token_type.
	AcceptsMissingTokenType bool `yaml:"acceptsMissingTokenType,omitempty" json:"acceptsMissingTokenType,omitempty"`
	// AcceptedTokenTypes replaces the default Bearer token type.
	AcceptedTokenTypes []string `yaml:"acceptedTokenTypes,omitempty" json:"acceptedTokenTypes,omitempty"`
	// KeyCredential is the secretString credential field that holds a JSON key, such as a Google service account
	// key, whose members fromKey sources read. Empty when no source reads a key.
	KeyCredential string `yaml:"keyCredential,omitempty" json:"keyCredential,omitempty"`
	// PrivateKey is the PEM RSA private key that signs the assertion.
	PrivateKey ValueSource `yaml:"privateKey" json:"privateKey"`
	// Issuer is the iss claim.
	Issuer ValueSource `yaml:"issuer" json:"issuer"`
	// Subject is the sub claim. Nil omits it.
	Subject *ValueSource `yaml:"subject,omitempty" json:"subject,omitempty"`
	// SubjectValidation validates a delegated subject before signing; goMailAddress uses Go mail.ParseAddress equality.
	SubjectValidation string `yaml:"subjectValidation,omitempty" json:"subjectValidation,omitempty"`
	// Audience is the aud claim.
	Audience ValueSource `yaml:"audience" json:"audience"`
	// TokenEndpoint is the HTTPS endpoint that exchanges the assertion.
	TokenEndpoint ValueSource `yaml:"tokenEndpoint" json:"tokenEndpoint"`
	// Scopes are sent space-separated as the scope claim. Empty omits it.
	Scopes []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	// AcceptedScopeSets replaces Scopes when checking a returned token's scope set.
	AcceptedScopeSets [][]string `yaml:"acceptedScopeSets,omitempty" json:"acceptedScopeSets,omitempty"`
	// Lifetime sets the exp claim after iat, such as 1h.
	Lifetime string `yaml:"lifetime" json:"lifetime"`
	// CredentialMappings map the token response into credential fields.
	CredentialMappings []OAuthCredentialMapping `yaml:"credentialMappings" json:"credentialMappings"`
	// CredentialCopies set credential fields from other credential fields after each mint.
	CredentialCopies []CredentialCopy `yaml:"credentialCopies,omitempty" json:"credentialCopies,omitempty"`
	// TerminalErrorCodes are the OAuth error codes, in a response below HTTP 500, after which the key can never
	// mint again, so the connection needs new key material. Empty means invalid_grant.
	TerminalErrorCodes []string `yaml:"terminalErrorCodes,omitempty" json:"terminalErrorCodes,omitempty"`
	// MissingExpiry decides the expiry of a token whose response has no expires_in. Nil keeps it.
	MissingExpiry *MissingExpiry `yaml:"missingExpiry,omitempty" json:"missingExpiry,omitempty"`
	// StoredMissingExpiry refreshes an existing token without recorded expiry when set to refresh; empty keeps it.
	StoredMissingExpiry string `yaml:"storedMissingExpiry,omitempty" json:"storedMissingExpiry,omitempty"`
}

// CredentialCopy sets one credential field to the value of another.
type CredentialCopy struct {
	// Credential is the credential field that receives the value.
	Credential string `yaml:"credential" json:"credential"`
	// FromCredential is the credential field that supplies it.
	FromCredential string `yaml:"fromCredential" json:"fromCredential"`
}

// ValueSource is a value that a host resolves from the connection: an OAuth endpoint or scope list, a token request
// setting, or a JWT bearer claim. Exactly one of Value, List, FromCredential, FromSetting, FromKey and Template is
// set. In YAML and JSON a literal is a plain string, a literal list a plain array, and every other source a
// mapping, such as {fromSetting: tokenEndpoint}.
type ValueSource struct {
	// Value is a literal string.
	Value string `yaml:"-" json:"-"`
	// List is a literal list, such as fixed OAuth scopes.
	List []string `yaml:"-" json:"-"`
	// FromCredential names a credential field of the method.
	FromCredential string `yaml:"fromCredential,omitempty" json:"fromCredential,omitempty"`
	// FromSetting names a connection setting: a spec.configuration field or one of the method's configuration fields.
	FromSetting string `yaml:"fromSetting,omitempty" json:"fromSetting,omitempty"`
	// FromKey names a top-level string member of the JSON key in a JWT bearer grant's keyCredential.
	FromKey string `yaml:"fromKey,omitempty" json:"fromKey,omitempty"`
	// Template is a literal whose {name} placeholders each name a credential field or setting of the method. A host
	// inserts each value path-escaped, so a template can only fill a URL path segment.
	Template string `yaml:"template,omitempty" json:"template,omitempty"`
	// Values translates the value of FromCredential, FromSetting or FromKey, such as an environment name into a
	// URL. The key "" translates a blank value. A value missing from the map cannot be resolved.
	Values map[string]string `yaml:"values,omitempty" json:"values,omitempty"`
}

// UnmarshalYAML reads keep or {lifetime: <duration>}.
func (missingExpiry *MissingExpiry) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		if node.Value != keepMissingExpiry {
			return fmt.Errorf("missingExpiry must be keep or {lifetime: <duration>}, not %q", node.Value)
		}
		*missingExpiry = MissingExpiry{}
		return nil
	}
	var lifetime struct {
		Lifetime string `yaml:"lifetime"`
	}
	if err := decodeKnownYAMLMapping(node, &lifetime, "lifetime"); err != nil {
		return fmt.Errorf("missingExpiry: %w", err)
	}
	if lifetime.Lifetime == "" {
		return errors.New("missingExpiry must be keep or {lifetime: <duration>}")
	}
	*missingExpiry = MissingExpiry{Lifetime: lifetime.Lifetime}
	return nil
}

// MarshalJSON writes "keep" or {"lifetime": "<duration>"}.
func (missingExpiry MissingExpiry) MarshalJSON() ([]byte, error) {
	if missingExpiry.Lifetime == "" {
		return json.Marshal(keepMissingExpiry)
	}
	return json.Marshal(map[string]string{"lifetime": missingExpiry.Lifetime})
}

// UnmarshalJSON reads "keep" or {"lifetime": "<duration>"}.
func (missingExpiry *MissingExpiry) UnmarshalJSON(contents []byte) error {
	var scalar string
	if json.Unmarshal(contents, &scalar) == nil {
		if scalar != keepMissingExpiry {
			return fmt.Errorf("missingExpiry must be keep or {lifetime: <duration>}, not %q", scalar)
		}
		*missingExpiry = MissingExpiry{}
		return nil
	}
	var lifetime struct {
		Lifetime string `json:"lifetime"`
	}
	if err := json.Unmarshal(contents, &lifetime); err != nil || lifetime.Lifetime == "" {
		return errors.New("missingExpiry must be keep or {lifetime: <duration>}")
	}
	*missingExpiry = MissingExpiry{Lifetime: lifetime.Lifetime}
	return nil
}

// UnmarshalYAML reads a literal string, a literal list or a source mapping.
func (source *ValueSource) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*source = ValueSource{Value: node.Value}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return fmt.Errorf("line %d: a value source list must hold strings: %w", node.Line, err)
		}
		*source = ValueSource{List: list}
		return nil
	}
	type valueSourceMapping ValueSource
	var mapping valueSourceMapping
	if err := decodeKnownYAMLMapping(node, &mapping, "fromCredential", "fromSetting", "fromKey", "template", "values"); err != nil {
		return fmt.Errorf("line %d: value source: %w", node.Line, err)
	}
	*source = ValueSource(mapping)
	return nil
}

// MarshalYAML writes a literal as a string, a literal list as a sequence and every other source as a mapping.
func (source ValueSource) MarshalYAML() (any, error) {
	switch {
	case source.Value != "":
		return source.Value, nil
	case source.List != nil:
		return source.List, nil
	}
	type valueSourceMapping ValueSource
	return valueSourceMapping(source), nil
}

// MarshalJSON writes a literal as a string, a literal list as an array and every other source as an object.
func (source ValueSource) MarshalJSON() ([]byte, error) {
	switch {
	case source.Value != "":
		return json.Marshal(source.Value)
	case source.List != nil:
		return json.Marshal(source.List)
	}
	type valueSourceMapping ValueSource
	return json.Marshal(valueSourceMapping(source))
}

// sourceCount counts the sources that are set, which a valid value source has exactly one of.
// isLiteralOrSettingSource reports whether source is exactly a literal or a setting reference, without values.
func isLiteralOrSettingSource(source ValueSource) bool {
	return source.sourceCount() == 1 && len(source.Values) == 0 && (source.Value != "" || source.FromSetting != "")
}

func (source ValueSource) sourceCount() int {
	count := 0
	for _, isSet := range []bool{
		source.Value != "", len(source.List) > 0, source.FromCredential != "", source.FromSetting != "", source.FromKey != "", source.Template != "",
	} {
		if isSet {
			count++
		}
	}
	return count
}

// UnmarshalJSON reads a literal string, a literal array or a source object with one source.
func (source *ValueSource) UnmarshalJSON(contents []byte) error {
	var literal string
	if json.Unmarshal(contents, &literal) == nil {
		*source = ValueSource{Value: literal}
		return nil
	}
	var list []string
	if json.Unmarshal(contents, &list) == nil {
		*source = ValueSource{List: list}
		return nil
	}
	type valueSourceMapping ValueSource
	var mapping valueSourceMapping
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&mapping); err != nil {
		return fmt.Errorf("value source: %w", err)
	}
	if ValueSource(mapping).sourceCount() != 1 {
		return errors.New("value source: an object must name exactly one source")
	}
	*source = ValueSource(mapping)
	return nil
}

// decodeKnownYAMLMapping decodes a mapping node into target and rejects keys outside knownKeys, because a custom
// unmarshaler does not inherit the decoder's strict field check.
func decodeKnownYAMLMapping(node *yaml.Node, target any, knownKeys ...string) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("must be a mapping")
	}
	for index := 0; index < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if !containsString(knownKeys, key) {
			return fmt.Errorf("field %s is not supported", key)
		}
	}
	return node.Decode(target)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// templatePlaceholderPattern matches one {name} placeholder of a ValueSource template.
var templatePlaceholderPattern = regexp.MustCompile(`\{([^{}]*)\}`)

// grantRequestParameters are the token request parameters that a grant or client authentication owns.
var grantRequestParameters = map[string]bool{
	"grant_type": true, "refresh_token": true, "client_id": true, "client_secret": true, "assertion": true,
	"client_assertion": true, "client_assertion_type": true,
}

// tokenGrantContext is what a grant declaration may reference: the method's credential fields and settings.
type tokenGrantContext struct {
	prefix      string
	credentials map[string]Field
	settings    map[string]Field
	// hostKeptCredentials are the method's fields that a host never serves to applications.
	hostKeptCredentials []string
}

// validateTokenGrants checks the refresh and minting declarations of one method.
func validateTokenGrants(prefix string, method AuthMethod, specConfigurationFields []Field) []string {
	grantContext := tokenGrantContext{
		prefix: prefix, credentials: fieldsByName(method.Fields), settings: authMethodSettings(specConfigurationFields, method.Configuration),
		hostKeptCredentials: HostKeptCredentials(method),
	}
	var problems []string
	if method.OAuth2 != nil {
		problems = append(problems, grantContext.validateOAuthTokenRequests(method.OAuth2)...)
	}
	if method.ClientCredentials != nil {
		if method.Type != "apiKey" {
			problems = append(problems, prefix+" clientCredentials requires apiKey auth")
		}
		problems = append(problems, grantContext.validateClientCredentials(method.ClientCredentials)...)
	}
	if method.ServiceAccount != nil {
		if method.Type != "serviceAccount" {
			problems = append(problems, prefix+" serviceAccount metadata requires serviceAccount auth")
		}
		if method.ServiceAccount.JWTBearer == nil {
			problems = append(problems, prefix+" serviceAccount requires jwtBearer")
		} else {
			problems = append(problems, grantContext.validateJWTBearer(method.ServiceAccount.JWTBearer)...)
		}
	}
	return problems
}

func (grantContext tokenGrantContext) validateOAuthTokenRequests(oauth *OAuth2) []string {
	problems := grantContext.validateTokenRequestSettings(grantContext.prefix+" oauth2", oauth.TokenEndpointAuthMethod, oauth.TokenRequestEncoding)
	// Hosts read oauth2 fields as literals or {fromSetting} only; other sources belong to minting grants.
	if source := oauth.TokenEndpointAuthMethod; source != nil && !isLiteralOrSettingSource(*source) {
		problems = append(problems, grantContext.prefix+" oauth2 tokenEndpointAuthMethod must be a literal or {fromSetting}")
	}
	label := grantContext.prefix + " oauth2 refresh"
	refresh := oauth.Refresh
	if (refresh != nil || mapsSource(oauth.CredentialMappings, "refresh_token")) && oauth.ClientIDCredential == "" {
		problems = append(problems, label+" requires clientIDCredential")
	}
	if refresh == nil {
		return problems
	}
	problems = append(problems, grantContext.validateResponseRules(label, refresh.TerminalErrorCodes, refresh.MissingExpiry, refresh.AdditionalParameters)...)
	problems = append(problems, validateStoredMissingExpiry(label, refresh.StoredMissingExpiry)...)
	problems = append(problems, validateAcceptedTokenTypes(label, refresh.AcceptedTokenTypes)...)
	problems = append(problems, validateScopeList(label+" requiredScopes", refresh.RequiredScopes)...)
	problems = append(problems, validateAcceptedScopeSets(label+" acceptedScopeSets", refresh.AcceptedScopeSets)...)
	if refresh.ScopeSeparators != "" && refresh.ScopeSeparators != ScopeSeparatorsWhitespace && refresh.ScopeSeparators != ScopeSeparatorsWhitespaceAndComma {
		problems = append(problems, label+" scopeSeparators must be whitespace or whitespaceAndComma")
	}
	if len(refresh.Grants) == 0 {
		if !mapsSource(oauth.CredentialMappings, "refresh_token") {
			problems = append(problems, label+" requires a credential mapped from refresh_token or explicit grants")
		}
		return problems
	}
	seenRefreshTokens := map[string]bool{}
	for _, grant := range refresh.Grants {
		if !grantContext.isCredentialOfType(grant.RefreshTokenCredential, "secretString") || seenRefreshTokens[grant.RefreshTokenCredential] {
			problems = append(problems, label+" grants require unique secretString refreshTokenCredential fields")
		}
		seenRefreshTokens[grant.RefreshTokenCredential] = true
		problems = append(problems, grantContext.validateCredentialMappings(label+" grant "+grant.RefreshTokenCredential, grant.CredentialMappings)...)
		problems = append(problems, validateAcceptedTokenTypes(label+" grant "+grant.RefreshTokenCredential, grant.AcceptedTokenTypes)...)
		if grant.GrantedScopes != "" && grant.GrantedScopes != MethodScopes && grant.GrantedScopes != UserScopes {
			problems = append(problems, label+" grant grantedScopes must be scopes or userScopes")
		}
		if grant.GrantedScopes == UserScopes && len(oauth.UserScopes) == 0 {
			problems = append(problems, label+" grant grantedScopes userScopes requires oauth2 userScopes")
		}
	}
	return problems
}

func (grantContext tokenGrantContext) validateClientCredentials(grant *ClientCredentialsGrant) []string {
	label := grantContext.prefix + " clientCredentials"
	problems := grantContext.validateTokenRequestSettings(label, grant.TokenEndpointAuthMethod, grant.TokenRequestEncoding)
	problems = append(problems, grantContext.validateEndpointSource(label+" tokenEndpoint", grant.TokenEndpoint, false)...)
	if !grantContext.isCredential(grant.ClientIDCredential) || grantContext.isCredentialOfType(grant.ClientIDCredential, "secretString") ||
		!grantContext.isCredentialOfType(grant.ClientSecretCredential, "secretString") {
		problems = append(problems, label+" requires a non-secret clientIDCredential and a secretString clientSecretCredential")
	}
	problems = append(problems, validateScopeList(label+" scopes", grant.Scopes)...)
	problems = append(problems, grantContext.validateCredentialMappings(label, grant.CredentialMappings)...)
	problems = append(problems, grantContext.validateResponseRules(label, grant.TerminalErrorCodes, grant.MissingExpiry, grant.AdditionalParameters)...)
	problems = append(problems, validateStoredMissingExpiry(label, grant.StoredMissingExpiry)...)
	problems = append(problems, validateAcceptedTokenTypes(label, grant.AcceptedTokenTypes)...)
	return problems
}

func (grantContext tokenGrantContext) validateJWTBearer(grant *JWTBearerGrant) []string {
	label := grantContext.prefix + " serviceAccount jwtBearer"
	var problems []string
	problems = append(problems, validateAcceptedTokenTypes(label, grant.AcceptedTokenTypes)...)
	hasKey := grant.KeyCredential != ""
	if hasKey && !grantContext.isCredentialOfType(grant.KeyCredential, "secretString") {
		problems = append(problems, label+" keyCredential must be a secretString credential field")
	}
	for name, source := range map[string]ValueSource{"privateKey": grant.PrivateKey, "issuer": grant.Issuer, "audience": grant.Audience} {
		problems = append(problems, grantContext.validateValueSource(label+" "+name, source, hasKey)...)
	}
	// A host keeps the private key from applications, which it can do only for a key in a secret credential field.
	isKeyFromSecret := grant.PrivateKey.FromKey != "" || grantContext.isCredentialOfType(grant.PrivateKey.FromCredential, "secretString")
	if !isKeyFromSecret || len(grant.PrivateKey.Values) > 0 {
		problems = append(problems, label+" privateKey must be {fromKey} or {fromCredential} of a secretString field, without values")
	}
	if grant.Subject != nil {
		problems = append(problems, grantContext.validateValueSource(label+" subject", *grant.Subject, hasKey)...)
	}
	if grant.SubjectValidation != "" && (grant.SubjectValidation != "goMailAddress" || grant.Subject == nil) {
		problems = append(problems, label+" subjectValidation requires a subject and must be goMailAddress")
	}
	problems = append(problems, grantContext.validateEndpointSource(label+" tokenEndpoint", grant.TokenEndpoint, hasKey)...)
	problems = append(problems, validateScopeList(label+" scopes", grant.Scopes)...)
	problems = append(problems, validateAcceptedScopeSets(label+" acceptedScopeSets", grant.AcceptedScopeSets)...)
	if lifetime, err := time.ParseDuration(grant.Lifetime); err != nil || lifetime <= 0 {
		problems = append(problems, label+" lifetime must be a positive duration")
	}
	problems = append(problems, grantContext.validateCredentialMappings(label, grant.CredentialMappings)...)
	for _, credentialCopy := range grant.CredentialCopies {
		if !grantContext.isCredential(credentialCopy.Credential) || !grantContext.isCredential(credentialCopy.FromCredential) {
			problems = append(problems, label+" credentialCopies must name credential fields")
		}
		if slices.Contains(grantContext.hostKeptCredentials, credentialCopy.FromCredential) {
			problems = append(problems, label+" credentialCopies cannot copy host-kept field "+credentialCopy.FromCredential)
		}
	}
	problems = append(problems, grantContext.validateResponseRules(label, grant.TerminalErrorCodes, grant.MissingExpiry, nil)...)
	problems = append(problems, validateStoredMissingExpiry(label, grant.StoredMissingExpiry)...)
	return problems
}

func (grantContext tokenGrantContext) validateTokenRequestSettings(label string, authMethod *ValueSource, encoding string) []string {
	problems := grantContext.validateClientAuthentication(label+" tokenEndpointAuthMethod", authMethod)
	if encoding != "" && encoding != FormTokenRequestEncoding && encoding != JSONTokenRequestEncoding {
		problems = append(problems, label+" tokenRequestEncoding must be form or json")
	}
	return problems
}

// validateClientAuthentication requires every value the source can resolve to to be a supported method: a
// literal, the translated values, or the enum values of the field or setting it names.
func (grantContext tokenGrantContext) validateClientAuthentication(label string, source *ValueSource) []string {
	if source == nil {
		return nil
	}
	problems := grantContext.validateValueSource(label, *source, false)
	var values []string
	switch {
	case source.Template != "":
		return append(problems, label+" cannot be a template")
	case source.Value != "":
		values = []string{source.Value}
	case len(source.Values) > 0:
		for _, value := range source.Values {
			values = append(values, value)
		}
	default:
		field, found := grantContext.credentials[source.FromCredential]
		if source.FromSetting != "" {
			field, found = grantContext.settings[source.FromSetting]
		}
		if !found || field.Type != "enum" {
			return append(problems, label+" must name an enum field or translate its value with values")
		}
		values = field.Enum
	}
	for _, value := range values {
		if value != ClientSecretPostAuthMethod && value != ClientSecretBasicAuthMethod {
			return append(problems, label+" must be client_secret_post or client_secret_basic")
		}
	}
	return problems
}

func validateStoredMissingExpiry(label, policy string) []string {
	if policy != "" && policy != StoredMissingExpiryKeep && policy != StoredMissingExpiryRefresh {
		return []string{label + " storedMissingExpiry must be keep or refresh"}
	}
	return nil
}

func (grantContext tokenGrantContext) validateResponseRules(label string, terminalErrorCodes []string, missingExpiry *MissingExpiry, additionalParameters map[string]string) []string {
	var problems []string
	seenCodes := map[string]bool{}
	for _, code := range terminalErrorCodes {
		if strings.TrimSpace(code) == "" || seenCodes[code] {
			problems = append(problems, label+" terminalErrorCodes must be non-empty and unique")
		}
		seenCodes[code] = true
	}
	if missingExpiry != nil && missingExpiry.Lifetime != "" {
		if lifetime, err := time.ParseDuration(missingExpiry.Lifetime); err != nil || lifetime <= 0 {
			problems = append(problems, label+" missingExpiry lifetime must be a positive duration")
		}
	}
	names := make([]string, 0, len(additionalParameters))
	for name := range additionalParameters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(additionalParameters[name]) == "" || grantRequestParameters[name] {
			problems = append(problems, label+" additionalParameters must be non-empty and cannot replace "+name)
		}
	}
	return problems
}

func (grantContext tokenGrantContext) validateCredentialMappings(label string, mappings []OAuthCredentialMapping) []string {
	if len(mappings) == 0 {
		return []string{label + " requires credentialMappings"}
	}
	seen := map[string]bool{}
	for _, mapping := range mappings {
		if !grantContext.isCredential(mapping.Credential) || seen[mapping.Credential] || !isValidJSONPath(mapping.Source) {
			return []string{label + " credential mappings require unique credential fields and dotted JSON response paths"}
		}
		seen[mapping.Credential] = true
	}
	return nil
}

// validateEndpointSource also requires a literal or translated endpoint to be an absolute HTTPS URL.
func (grantContext tokenGrantContext) validateEndpointSource(label string, source ValueSource, hasKey bool) []string {
	problems := grantContext.validateValueSource(label, source, hasKey)
	if source.FromSetting != "" && len(source.Values) == 0 {
		problems = append(problems, validateSettingReference(label, source.FromSetting, "url", grantContext.settings)...)
	}
	if source.Value != "" && !isHTTPSURL(source.Value) {
		problems = append(problems, label+" must be an absolute HTTPS URL")
	}
	for _, value := range source.Values {
		if !isHTTPSURL(value) {
			problems = append(problems, label+" values must be absolute HTTPS URLs")
		}
	}
	if source.Template != "" && !hasFixedHTTPSOrigin(source.Template) {
		problems = append(problems, label+" template must start with a fixed HTTPS origin")
	}
	return problems
}

// hasFixedHTTPSOrigin reports whether template names its HTTPS scheme and host before its first placeholder.
func hasFixedHTTPSOrigin(template string) bool {
	prefix, _, _ := strings.Cut(template, "{")
	host, _, hasPath := strings.Cut(strings.TrimPrefix(prefix, "https://"), "/")
	return strings.HasPrefix(prefix, "https://") && hasPath && host != "" && isHTTPSURL(prefix)
}

func (grantContext tokenGrantContext) validateValueSource(label string, source ValueSource, hasKey bool) []string {
	if source.sourceCount() != 1 || len(source.List) > 0 {
		return []string{label + " requires exactly one of a literal, fromCredential, fromSetting, fromKey, or template"}
	}
	var problems []string
	switch {
	case source.FromCredential != "" && !grantContext.isCredential(source.FromCredential):
		problems = append(problems, label+" fromCredential must name a credential field of the method")
	case source.FromSetting != "" && grantContext.settings[source.FromSetting].Name == "":
		problems = append(problems, label+" fromSetting must name a setting of the method")
	case source.FromKey != "" && !hasKey:
		problems = append(problems, label+" fromKey requires keyCredential")
	case source.Template != "":
		placeholders := templatePlaceholderPattern.FindAllStringSubmatch(source.Template, -1)
		if len(placeholders) == 0 || strings.Count(source.Template, "{") != len(placeholders) || strings.Count(source.Template, "}") != len(placeholders) {
			problems = append(problems, label+" template requires {name} placeholders")
		}
		for _, placeholder := range placeholders {
			if !grantContext.isCredential(placeholder[1]) && grantContext.settings[placeholder[1]].Name == "" {
				problems = append(problems, label+" template placeholder "+placeholder[1]+" must name a credential field or setting of the method")
			}
		}
	}
	if len(source.Values) > 0 && source.FromCredential == "" && source.FromSetting == "" && source.FromKey == "" {
		problems = append(problems, label+" values translate only fromCredential, fromSetting, or fromKey")
	}
	for _, value := range source.Values {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, label+" values must be non-empty")
		}
	}
	return problems
}

func validateScopeList(label string, scopes []string) []string {
	seen := map[string]bool{}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" || seen[scope] {
			return []string{label + " must be non-empty and unique"}
		}
		seen[scope] = true
	}
	return nil
}

func validateAcceptedScopeSets(label string, scopeSets [][]string) []string {
	seen := map[string]bool{}
	for _, scopeSet := range scopeSets {
		if len(scopeSet) == 0 || len(validateScopeList(label, scopeSet)) > 0 {
			return []string{label + " must contain non-empty sets of unique scopes"}
		}
		canonical := strings.Join(scopeSet, "\x00")
		if seen[canonical] {
			return []string{label + " must not repeat a scope set"}
		}
		seen[canonical] = true
	}
	return nil
}

func validateAcceptedTokenTypes(label string, tokenTypes []string) []string {
	seen := map[string]bool{}
	for _, tokenType := range tokenTypes {
		canonical := strings.ToLower(tokenType)
		if strings.TrimSpace(tokenType) != tokenType || tokenType == "" || seen[canonical] {
			return []string{label + " acceptedTokenTypes must be non-empty and unique"}
		}
		seen[canonical] = true
	}
	return nil
}

func (grantContext tokenGrantContext) isCredential(name string) bool {
	return grantContext.credentials[name].Name != ""
}

func (grantContext tokenGrantContext) isCredentialOfType(name, fieldType string) bool {
	return grantContext.isCredential(name) && grantContext.credentials[name].Type == fieldType
}

func mapsSource(mappings []OAuthCredentialMapping, source string) bool {
	for _, mapping := range mappings {
		if mapping.Source == source {
			return true
		}
	}
	return false
}
