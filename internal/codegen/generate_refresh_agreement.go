// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"go/format"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/schema"
)

//go:embed templates/python_refresh_agreement.py
var pythonRefreshAgreementTest string

//go:embed templates/typescript_refresh_agreement.ts
var typeScriptRefreshAgreementTest string

// GeneratePythonRefreshAgreementTest emits a replay of generated cases through the manifest driver.
func GeneratePythonRefreshAgreementTest(packageName string) []byte {
	return []byte(strings.ReplaceAll(pythonRefreshAgreementTest, "{{PACKAGE}}", packageName))
}

// GenerateTypeScriptRefreshAgreementTest emits a replay of generated cases through the manifest driver.
func GenerateTypeScriptRefreshAgreementTest() []byte {
	return []byte(typeScriptRefreshAgreementTest)
}

const (
	// RefreshAgreementTestOutputFile is the generated test that runs a connector's hand-written credential refresh
	// driver against a stub token endpoint and checks it against the manifest's refresh and minting declarations.
	RefreshAgreementTestOutputFile = "zz_generated_credential_refresh_test.go"
	// RefreshAgreementSamplesFile, in the connector's testdata directory, overrides sample values that a driver
	// validates beyond the manifest, such as a provider-owned API domain.
	RefreshAgreementSamplesFile = "credential-refresh-samples.json"

	sampleClientID        = "manifest-client-id"
	sampleClientSecret    = "manifest-client-secret"
	sampleRefreshToken    = "manifest-refresh-token"
	sampleAccessToken     = "manifest-access-token"
	sampleSubject         = "manifest-user@example.com"
	sampleTemplateValue   = "manifest.example.com"
	sampleTokenEndpoint   = "https://manifest.example.com/token"
	sampleExpiresInSecond = 3600
	offlineAccessScope    = "offline_access"
)

// RefreshAgreementSample overrides sample values for every grant ("*"), the grants of one method (its ID, or
// spec.auth), or one grant, such as "spec.auth refresh bot_refresh_token".
type RefreshAgreementSample struct {
	// Credentials are stored credential values by field name.
	Credentials map[string]string `json:"credentials,omitempty"`
	// Settings are connection settings by field name.
	Settings map[string]string `json:"settings,omitempty"`
	// Response are string members of a successful token response, such as token_type or a mapped api_domain.
	Response map[string]string `json:"response,omitempty"`
}

// RefreshAgreementCase is one language-neutral token exchange derived from the Go agreement generator's samples.
type RefreshAgreementCase struct {
	Name                 string                     `json:"name"`
	ExpectedError        string                     `json:"expectedError,omitempty"`
	MethodID             string                     `json:"methodId,omitempty"`
	Credentials          map[string]string          `json:"credentials"`
	Settings             map[string]string          `json:"settings,omitempty"`
	Endpoint             string                     `json:"endpoint"`
	Encoding             string                     `json:"encoding"`
	ClientAuth           string                     `json:"clientAuth"`
	ClientID             string                     `json:"clientId,omitempty"`
	ClientSecret         string                     `json:"clientSecret,omitempty"`
	Parameters           map[string]string          `json:"parameters"`
	Response             map[string]any             `json:"response"`
	Updates              map[string]string          `json:"updates"`
	TerminalErrorCodes   []string                   `json:"terminalErrorCodes"`
	KeyCredential        string                     `json:"keyCredential,omitempty"`
	PrivateKeyCredential string                     `json:"privateKeyCredential,omitempty"`
	PrivateKeyMember     string                     `json:"privateKeyMember,omitempty"`
	KeyMembers           map[string]string          `json:"keyMembers,omitempty"`
	Assertion            *RefreshAgreementAssertion `json:"assertion,omitempty"`
	MayRefuseWithoutCall bool                       `json:"mayRefuseWithoutCall,omitempty"`
}

// RefreshAgreementAssertion records the JWT claims and lifetime checked by the Go agreement harness.
type RefreshAgreementAssertion struct {
	Issuer          string `json:"issuer"`
	Subject         string `json:"subject,omitempty"`
	Audience        string `json:"audience"`
	Scope           string `json:"scope,omitempty"`
	LifetimeSeconds int64  `json:"lifetimeSeconds"`
}

// GenerateRefreshAgreementCases returns the Go agreement's sample cases for Python and TypeScript replay.
func GenerateRefreshAgreementCases(manifest schema.Manifest, samples map[string]RefreshAgreementSample) ([]byte, error) {
	agreement := &refreshAgreementGenerator{manifest: manifest, samples: samples}
	var cases []RefreshAgreementCase
	for _, grant := range declaredGrants(manifest) {
		for _, variant := range agreement.variants(grant) {
			isPublicClients := []bool{false}
			if grant.kind == "refresh" && !agreement.isRequiredCredential(grant.method, grant.clientSecret) {
				isPublicClients = append(isPublicClients, true)
			}
			for _, isPublicClient := range isPublicClients {
				name := grant.name
				if variant.field != "" {
					name += fmt.Sprintf(" with %s %q", variant.field, variant.value)
				}
				if isPublicClient {
					name += " without a client secret"
				}
				sample := agreement.sample(grant)
				credentials := agreement.sampleCredentials(grant, variant, sample)
				settings := agreement.sampleSettings(grant, variant, sample)
				if isPublicClient {
					credentials[grant.clientSecret] = ""
				}
				keyMembers := agreement.sampleKeyMembers(grant, variant)
				endpoint, err := resolveSampleSource(grant.endpoint, credentials, settings, keyMembers)
				if err != nil {
					return nil, fmt.Errorf("%s tokenEndpoint: %w", name, err)
				}
				clientAuth := "none"
				if grant.kind != "jwtBearer" {
					clientAuth, err = resolveSampleSource(grant.authMethod, credentials, settings, keyMembers)
					if err != nil {
						return nil, fmt.Errorf("%s tokenEndpointAuthMethod: %w", name, err)
					}
				}
				parameters := map[string]string{}
				for key, value := range grant.additional {
					parameters[key] = value
				}
				switch grant.kind {
				case "refresh":
					parameters["grant_type"] = "refresh_token"
					parameters["refresh_token"] = credentials[grant.refreshToken]
					if isPublicClient {
						clientAuth = "public"
					}
				case "clientCredentials":
					parameters["grant_type"] = "client_credentials"
					if len(grant.requestScopes) > 0 {
						parameters["scope"] = strings.Join(grant.requestScopes, " ")
					}
				case "jwtBearer":
					parameters["grant_type"] = "urn:ietf:params:oauth:grant-type:jwt-bearer"
				}
				clientID, clientSecret := credentials[grant.clientID], credentials[grant.clientSecret]
				switch clientAuth {
				case "client_secret_post":
					parameters["client_id"], parameters["client_secret"] = clientID, clientSecret
				case "public":
					parameters["client_id"] = clientID
				}
				response, updates := agreement.sampleResponse(grant, credentials, sample)
				result := RefreshAgreementCase{Name: name, MethodID: grant.method.ID, Credentials: credentials,
					Settings: settings, Endpoint: endpoint, Encoding: grant.encoding, ClientAuth: clientAuth, ClientID: clientID, ClientSecret: clientSecret,
					Parameters: parameters, Response: response, Updates: updates, TerminalErrorCodes: grant.terminalCodes,
					MayRefuseWithoutCall: isPublicClient}
				if grant.jwtBearer != nil {
					result.KeyCredential = grant.jwtBearer.KeyCredential
					result.PrivateKeyCredential = grant.jwtBearer.PrivateKey.FromCredential
					result.PrivateKeyMember = grant.jwtBearer.PrivateKey.FromKey
					result.KeyMembers = keyMembers
					assertion, err := agreement.sampleAssertion(grant, credentials, settings, keyMembers)
					if err != nil {
						return nil, fmt.Errorf("%s assertion: %w", name, err)
					}
					result.Assertion = assertion
				}
				cases = append(cases, result)
				for _, variation := range agreement.responseVariations(result, grant) {
					cases = append(cases, variation)
				}
			}
		}
	}
	encoded, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode refresh agreement cases: %w", err)
	}
	return append(encoded, '\n'), nil
}

func (agreement *refreshAgreementGenerator) sampleAssertion(grant declaredGrant, credentials, settings, keyMembers map[string]string) (*RefreshAgreementAssertion, error) {
	jwt := grant.jwtBearer
	resolve := func(source schema.ValueSource) (string, error) {
		return resolveSampleSource(source, credentials, settings, keyMembers)
	}
	issuer, err := resolve(jwt.Issuer)
	if err != nil {
		return nil, err
	}
	audience, err := resolve(jwt.Audience)
	if err != nil {
		return nil, err
	}
	subject := ""
	if jwt.Subject != nil {
		subject, err = resolve(*jwt.Subject)
		if err != nil {
			return nil, err
		}
	}
	lifetime, err := time.ParseDuration(jwt.Lifetime)
	if err != nil {
		return nil, err
	}
	return &RefreshAgreementAssertion{Issuer: issuer, Subject: subject, Audience: audience,
		Scope: strings.Join(jwt.Scopes, " "), LifetimeSeconds: int64(lifetime / time.Second)}, nil
}

func (agreement *refreshAgreementGenerator) responseVariations(base RefreshAgreementCase, grant declaredGrant) []RefreshAgreementCase {
	clone := func(suffix, expectedError string) RefreshAgreementCase {
		variation := base
		variation.Name += suffix
		variation.ExpectedError = expectedError
		variation.Response = make(map[string]any, len(base.Response))
		for name, value := range base.Response {
			variation.Response[name] = value
		}
		variation.TerminalErrorCodes = nil
		return variation
	}
	var variations []RefreshAgreementCase
	wrongType := clone(" with unaccepted token type", "retryable")
	wrongType.Response["token_type"] = "manifest-unaccepted-token-type"
	variations = append(variations, wrongType)
	missingType := clone(" without token type", "retryable")
	delete(missingType.Response, "token_type")
	if grant.acceptsMissingTokenType {
		missingType.ExpectedError = ""
	}
	variations = append(variations, missingType)
	if len(grant.responseScopes) > 0 {
		partial := clone(" with partial scopes", "reauthorization")
		partial.Response["scope"] = "manifest-ungranted-scope"
		variations = append(variations, partial)
		if grant.exactScopes {
			extra := clone(" with extra scopes", "reauthorization")
			extra.Response["scope"] = strings.Join(grant.responseScopes, " ") + " manifest-extra-scope"
			variations = append(variations, extra)
		}
	}
	if grant.missingExpiry == nil {
		missingExpiry := clone(" without expires_in", "retryable")
		delete(missingExpiry.Response, "expires_in")
		variations = append(variations, missingExpiry)
	}
	return variations
}

// declaredGrant is one token request that a method declares.
type declaredGrant struct {
	name         string
	methodLabel  string
	method       schema.AuthMethod
	kind         string
	endpoint     schema.ValueSource
	authMethod   schema.ValueSource
	encoding     string
	clientID     string
	clientSecret string
	refreshToken string
	mappings     []schema.OAuthCredentialMapping
	copies       []schema.CredentialCopy
	// requestScopes are sent as the scope parameter or claim; responseScopes are what a successful response reports.
	requestScopes           []string
	responseScopes          []string
	terminalCodes           []string
	acceptedTokenTypes      []string
	acceptsMissingTokenType bool
	exactScopes             bool
	missingExpiry           *schema.MissingExpiry
	additional              map[string]string
	jwtBearer               *schema.JWTBearerGrant
}

// HasDeclaredTokenGrants reports whether any method of manifest refreshes or mints a token.
func HasDeclaredTokenGrants(manifest schema.Manifest) bool {
	return len(declaredGrants(manifest)) > 0
}

// GenerateRefreshAgreementTest returns the credential refresh agreement test of a manifest that declares token
// grants. connectorImportPath is the Go import path of the connector package; samples may be nil.
func GenerateRefreshAgreementTest(manifest schema.Manifest, connectorImportPath string, samples map[string]RefreshAgreementSample) ([]byte, error) {
	grants := declaredGrants(manifest)
	if len(grants) == 0 {
		return nil, fmt.Errorf("manifest %s declares no token grants", manifest.Metadata.Name)
	}
	connector := manifest.Spec.Codegen.Go.Package
	agreement := &refreshAgreementGenerator{manifest: manifest, connector: connector, samples: samples}
	generation := &generator{}
	generation.mustWrite("// Code generated by connectorctl generate; DO NOT EDIT.\n")
	generation.mustWrite("// Copyright (c) 2026 Super Durable\n// SPDX-License-Identifier: MIT\n\n")
	generation.mustWrite("package %s_test\n\n", connector)
	generation.mustWrite("import (\n")
	for _, standardImport := range []string{"context", "crypto", "crypto/rand", "crypto/rsa", "crypto/sha256", "crypto/x509", "encoding/base64", "encoding/json", "encoding/pem", "io", "net/http", "net/url", "strings", "testing", "time"} {
		generation.mustWrite("\t%q\n", standardImport)
	}
	generation.mustWrite("\n\t\"github.com/stretchr/testify/require\"\n\t%q\n", connectorImportPath)
	generation.mustWrite("\t\"github.com/superdurable/dex-connectors-library/sdkgo\"\n)\n")
	generation.mustWrite("%s\n", strings.ReplaceAll(refreshAgreementHarness, "CONNECTOR", connector))

	if agreement.readsSettings(grants) {
		generation.mustWrite("// newManifestRefreshDriver passes the settings that connector.yaml's declarations read to the driver.\n")
		generation.mustWrite("func newManifestRefreshDriver(httpClient *http.Client, settings map[string]string) *%s.CredentialRefreshDriver {\n", connector)
		generation.mustWrite("\treturn newCredentialRefreshDriverWithSettings(httpClient, settings)\n}\n\n")
	} else {
		generation.mustWrite("func newManifestRefreshDriver(httpClient *http.Client, _ map[string]string) *%s.CredentialRefreshDriver {\n", connector)
		generation.mustWrite("\treturn %s.NewCredentialRefreshDriver(httpClient)\n}\n\n", connector)
	}
	agreement.writeCredentialReaders(generation)
	generation.mustWrite("// manifestDeclaredErrorCodes are every terminal error code that connector.yaml declares.\n")
	generation.mustWrite("var manifestDeclaredErrorCodes = %s\n\n", goStringSlice(declaredErrorCodes(grants)))
	generation.mustWrite("func manifestGrants() []manifestGrant {\n\treturn []manifestGrant{\n")
	for _, grant := range grants {
		for _, variant := range agreement.variants(grant) {
			if err := agreement.writeGrantCase(generation, grant, variant, false); err != nil {
				return nil, err
			}
			if grant.kind == "refresh" && !agreement.isRequiredCredential(grant.method, grant.clientSecret) {
				if err := agreement.writeGrantCase(generation, grant, variant, true); err != nil {
					return nil, err
				}
			}
		}
	}
	generation.mustWrite("\t}\n}\n\n")
	agreement.writeStaticMethods(generation, grants)
	formatted, err := format.Source(generation.output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated credential refresh agreement test: %w", err)
	}
	return formatted, nil
}

type refreshAgreementGenerator struct {
	manifest  schema.Manifest
	connector string
	samples   map[string]RefreshAgreementSample
}

// grantVariant fixes the value of one credential field or setting that a values map translates.
type grantVariant struct {
	field string
	value string
}

// declaredGrants lists every declared grant in manifest order. An oauth2 method that maps a refresh token without
// a refresh block has the default grant.
func declaredGrants(manifest schema.Manifest) []declaredGrant {
	var grants []declaredGrant
	methods := manifest.Spec.Auth.Methods
	labels := make([]string, 0, len(methods))
	for _, method := range methods {
		labels = append(labels, method.ID)
	}
	if len(methods) == 0 {
		auth := manifest.Spec.Auth
		methods = []schema.AuthMethod{{Type: auth.Type, Fields: auth.Fields, OAuth2: auth.OAuth2, ClientCredentials: auth.ClientCredentials, ServiceAccount: auth.ServiceAccount}}
		labels = []string{"spec.auth"}
	}
	for index, method := range methods {
		label := labels[index]
		if oauth := method.OAuth2; oauth != nil {
			refresh := oauth.Refresh
			if refresh == nil {
				refresh = &schema.OAuthRefresh{}
			}
			grantList := refresh.Grants
			if len(grantList) == 0 {
				for _, mapping := range oauth.CredentialMappings {
					if mapping.Source == "refresh_token" {
						grantList = []schema.OAuthRefreshGrant{{RefreshTokenCredential: mapping.Credential, CredentialMappings: oauth.CredentialMappings}}
					}
				}
			}
			for _, refreshGrant := range grantList {
				name := label + " refresh"
				if len(refresh.Grants) > 0 {
					name += " " + refreshGrant.RefreshTokenCredential
				}
				responseScopes := oauth.Scopes.List
				if len(refresh.RequiredScopes) > 0 {
					responseScopes = refresh.RequiredScopes
				}
				if refreshGrant.GrantedScopes == schema.UserScopes {
					responseScopes = oauth.UserScopes
				}
				acceptedTokenTypes := refreshGrant.AcceptedTokenTypes
				if len(acceptedTokenTypes) == 0 {
					acceptedTokenTypes = refresh.AcceptedTokenTypes
				}
				acceptedTokenTypes = defaultStrings(acceptedTokenTypes, "Bearer")
				acceptsMissingTokenType := refresh.AcceptsMissingTokenType
				if refreshGrant.AcceptsMissingTokenType != nil {
					acceptsMissingTokenType = *refreshGrant.AcceptsMissingTokenType
				}
				exactScopes := refresh.ExactScopes
				if refreshGrant.ExactScopes != nil {
					exactScopes = *refreshGrant.ExactScopes
				}
				grants = append(grants, declaredGrant{
					name: name, methodLabel: label, method: method, kind: "refresh", endpoint: oauth.TokenEndpoint,
					authMethod: clientAuthentication(oauth.TokenEndpointAuthMethod), encoding: defaultString(oauth.TokenRequestEncoding, schema.FormTokenRequestEncoding),
					clientID: oauth.ClientIDCredential, clientSecret: oauth.ClientSecretCredential, refreshToken: refreshGrant.RefreshTokenCredential,
					mappings: refreshGrant.CredentialMappings, responseScopes: withoutString(responseScopes, offlineAccessScope),
					terminalCodes: defaultStrings(refresh.TerminalErrorCodes, "invalid_grant"), missingExpiry: refresh.MissingExpiry, additional: refresh.AdditionalParameters,
					acceptedTokenTypes: acceptedTokenTypes, acceptsMissingTokenType: acceptsMissingTokenType, exactScopes: exactScopes,
				})
			}
		}
		if clientCredentials := method.ClientCredentials; clientCredentials != nil {
			grants = append(grants, declaredGrant{
				name: label + " clientCredentials", methodLabel: label, method: method, kind: "clientCredentials", endpoint: clientCredentials.TokenEndpoint,
				authMethod: clientAuthentication(clientCredentials.TokenEndpointAuthMethod), encoding: defaultString(clientCredentials.TokenRequestEncoding, schema.FormTokenRequestEncoding),
				clientID: clientCredentials.ClientIDCredential, clientSecret: clientCredentials.ClientSecretCredential, mappings: clientCredentials.CredentialMappings,
				requestScopes: clientCredentials.Scopes, responseScopes: clientCredentials.Scopes,
				terminalCodes: defaultStrings(clientCredentials.TerminalErrorCodes, "invalid_client"), missingExpiry: clientCredentials.MissingExpiry, additional: clientCredentials.AdditionalParameters,
				acceptedTokenTypes: defaultStrings(clientCredentials.AcceptedTokenTypes, "Bearer"), acceptsMissingTokenType: clientCredentials.AcceptsMissingTokenType,
			})
		}
		if method.ServiceAccount != nil && method.ServiceAccount.JWTBearer != nil {
			jwtBearer := method.ServiceAccount.JWTBearer
			grants = append(grants, declaredGrant{
				name: label + " jwtBearer", methodLabel: label, method: method, kind: "jwtBearer", endpoint: jwtBearer.TokenEndpoint,
				encoding: schema.FormTokenRequestEncoding, mappings: jwtBearer.CredentialMappings, copies: jwtBearer.CredentialCopies,
				requestScopes: jwtBearer.Scopes, responseScopes: jwtBearer.Scopes, terminalCodes: defaultStrings(jwtBearer.TerminalErrorCodes, "invalid_grant"),
				missingExpiry: jwtBearer.MissingExpiry, jwtBearer: jwtBearer,
				acceptedTokenTypes: defaultStrings(jwtBearer.AcceptedTokenTypes, "Bearer"), acceptsMissingTokenType: jwtBearer.AcceptsMissingTokenType,
			})
		}
	}
	return grants
}

// variants returns one variant for each value that a values map translates, or one variant without a fixed value.
func (agreement *refreshAgreementGenerator) variants(grant declaredGrant) []grantVariant {
	var translated []schema.ValueSource
	translated = append(translated, grant.endpoint, grant.authMethod)
	if grant.jwtBearer != nil {
		translated = append(translated, grant.jwtBearer.Issuer, grant.jwtBearer.Audience)
		if grant.jwtBearer.Subject != nil {
			translated = append(translated, *grant.jwtBearer.Subject)
		}
	}
	keys := map[string]bool{}
	field := ""
	for _, source := range translated {
		if len(source.Values) == 0 {
			continue
		}
		field = sourceField(source)
		for key := range source.Values {
			keys[key] = true
		}
	}
	if field == "" && len(grant.authMethod.Values) == 0 {
		if enumField, isEnum := agreement.enumField(grant.method, sourceField(grant.authMethod)); isEnum {
			field = enumField.Name
			for _, value := range enumField.Enum {
				keys[value] = true
			}
		}
	}
	if field == "" {
		return []grantVariant{{}}
	}
	variants := make([]grantVariant, 0, len(keys))
	for key := range keys {
		variants = append(variants, grantVariant{field: field, value: key})
	}
	sort.Slice(variants, func(left, right int) bool { return variants[left].value < variants[right].value })
	return variants
}

func (agreement *refreshAgreementGenerator) writeGrantCase(generation *generator, grant declaredGrant, variant grantVariant, isPublicClient bool) error {
	name := grant.name
	if variant.field != "" {
		name += fmt.Sprintf(" with %s %q", variant.field, variant.value)
	}
	if isPublicClient {
		name += " without a client secret"
	}
	sample := agreement.sample(grant)
	credentials := agreement.sampleCredentials(grant, variant, sample)
	settings := agreement.sampleSettings(grant, variant, sample)
	if isPublicClient {
		credentials[grant.clientSecret] = ""
	}
	resolve := func(source schema.ValueSource) (string, error) {
		return resolveSampleSource(source, credentials, settings, agreement.sampleKeyMembers(grant, variant))
	}
	endpoint, err := resolve(grant.endpoint)
	if err != nil {
		return fmt.Errorf("%s tokenEndpoint: %w", name, err)
	}
	parameters := map[string]string{}
	for parameter, value := range grant.additional {
		parameters[parameter] = value
	}
	clientAuth := "none"
	if grant.kind != "jwtBearer" {
		if clientAuth, err = resolve(grant.authMethod); err != nil {
			return fmt.Errorf("%s tokenEndpointAuthMethod: %w", name, err)
		}
	}
	switch grant.kind {
	case "refresh":
		parameters["grant_type"] = "refresh_token"
		parameters["refresh_token"] = credentials[grant.refreshToken]
		if isPublicClient {
			clientAuth = "public"
		}
	case "clientCredentials":
		parameters["grant_type"] = "client_credentials"
		if len(grant.requestScopes) > 0 {
			parameters["scope"] = strings.Join(grant.requestScopes, " ")
		}
	case "jwtBearer":
		parameters["grant_type"] = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	}
	response, updates := agreement.sampleResponse(grant, credentials, sample)

	generation.mustWrite("\t\t{\n\t\t\tname: %q,\n", name)
	if len(agreement.manifest.Spec.Auth.Methods) > 0 {
		generation.mustWrite("\t\t\tmethodID: %q,\n", grant.method.ID)
	}
	generation.mustWrite("\t\t\tcredentials: func(privateKeyPEM string) %s.Credentials {\n\t\t\t\treturn %s.Credentials{\n", agreement.connector, agreement.connector)
	if len(agreement.manifest.Spec.Auth.Methods) > 0 {
		generation.mustWrite("\t\t\t\t\tAuthMethodID: %q,\n", grant.method.ID)
	}
	for _, field := range agreement.manifest.Spec.Auth.Fields {
		value, isSampled := credentials[field.Name]
		if !isSampled || !isStringLikeField(field) {
			continue
		}
		expression := strconv.Quote(value)
		if grant.jwtBearer != nil && field.Name == grant.jwtBearer.KeyCredential {
			expression = fmt.Sprintf("manifestJSONKey(privateKeyPEM, %q, %s)", grant.jwtBearer.PrivateKey.FromKey, goStringMap(agreement.sampleKeyMembers(grant, variant)))
		} else if grant.jwtBearer != nil && field.Name == grant.jwtBearer.PrivateKey.FromCredential {
			expression = "privateKeyPEM"
		}
		generation.mustWrite("\t\t\t\t\t%s: %s,\n", field.GoName, typedFieldExpression(agreement.connector, field, expression))
	}
	generation.mustWrite("\t\t\t\t}\n\t\t\t},\n")
	if len(settings) > 0 {
		generation.mustWrite("\t\t\tsettings: %s,\n", goStringMap(settings))
	}
	generation.mustWrite("\t\t\tendpoint: %q,\n\t\t\tencoding: %q,\n\t\t\tclientAuth: %q,\n", endpoint, grant.encoding, clientAuth)
	if grant.clientID != "" {
		generation.mustWrite("\t\t\tclientID: %q,\n\t\t\tsecret: %q,\n", credentials[grant.clientID], credentials[grant.clientSecret])
	}
	generation.mustWrite("\t\t\tparameters: %s,\n", goStringMap(parameters))
	if grant.jwtBearer != nil {
		if err := agreement.writeAssertion(generation, grant, resolve); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	generation.mustWrite("\t\t\tresponse: %s,\n", goAnyMap(response))
	generation.mustWrite("\t\t\tupdates: %s,\n", goStringMap(updates))
	if grant.refreshToken != "" {
		generation.mustWrite("\t\t\trefreshToken: %q,\n", grant.refreshToken)
	}
	generation.mustWrite("\t\t\tterminalErrorCodes: %s,\n", goStringSlice(grant.terminalCodes))
	if grant.missingExpiry != nil && grant.missingExpiry.Lifetime != "" {
		lifetime, err := time.ParseDuration(grant.missingExpiry.Lifetime)
		if err != nil {
			return fmt.Errorf("%s missingExpiry: %w", name, err)
		}
		generation.mustWrite("\t\t\tmissingExpiry: time.Duration(%d),\n", lifetime)
	}
	if isPublicClient {
		generation.mustWrite("\t\t\tmayRefuseWithoutRequest: true,\n")
	}
	generation.mustWrite("\t\t},\n")
	return nil
}

func (agreement *refreshAgreementGenerator) writeAssertion(generation *generator, grant declaredGrant, resolve func(schema.ValueSource) (string, error)) error {
	jwtBearer := grant.jwtBearer
	issuer, err := resolve(jwtBearer.Issuer)
	if err != nil {
		return fmt.Errorf("issuer: %w", err)
	}
	audience, err := resolve(jwtBearer.Audience)
	if err != nil {
		return fmt.Errorf("audience: %w", err)
	}
	subject := ""
	if jwtBearer.Subject != nil {
		if subject, err = resolve(*jwtBearer.Subject); err != nil {
			return fmt.Errorf("subject: %w", err)
		}
	}
	lifetime, err := time.ParseDuration(jwtBearer.Lifetime)
	if err != nil {
		return fmt.Errorf("lifetime: %w", err)
	}
	generation.mustWrite("\t\t\tassertion: &manifestAssertion{issuer: %q, subject: %q, audience: %q, scope: %q, lifetime: time.Duration(%d)},\n",
		issuer, subject, audience, strings.Join(jwtBearer.Scopes, " "), lifetime)
	return nil
}

// sample merges the overrides that apply to grant, the most specific last.
func (agreement *refreshAgreementGenerator) sample(grant declaredGrant) RefreshAgreementSample {
	merged := RefreshAgreementSample{Credentials: map[string]string{}, Settings: map[string]string{}, Response: map[string]string{}}
	for _, key := range []string{"*", grant.methodLabel, grant.name} {
		override, found := agreement.samples[key]
		if !found {
			continue
		}
		for name, value := range override.Credentials {
			merged.Credentials[name] = value
		}
		for name, value := range override.Settings {
			merged.Settings[name] = value
		}
		for name, value := range override.Response {
			merged.Response[name] = value
		}
	}
	return merged
}

// sampleCredentials gives every string credential field of the method a value that its role in the grant needs.
func (agreement *refreshAgreementGenerator) sampleCredentials(grant declaredGrant, variant grantVariant, sample RefreshAgreementSample) map[string]string {
	credentials := map[string]string{}
	for _, field := range grant.method.Fields {
		value := "manifest-" + field.Name
		if field.Type == "enum" && len(field.Enum) > 0 {
			value = field.Enum[0]
		}
		if field.Type == "url" {
			value = "https://manifest.example.com/" + field.Name
		}
		credentials[field.Name] = value
	}
	for _, mapping := range grant.mappings {
		switch mapping.Source {
		case "access_token":
			credentials[mapping.Credential] = sampleAccessToken
		case "refresh_token":
			credentials[mapping.Credential] = sampleRefreshToken
		}
	}
	for _, placeholder := range agreement.templatePlaceholders(grant) {
		if _, isCredential := credentials[placeholder]; isCredential {
			credentials[placeholder] = sampleTemplateValue
		}
	}
	if grant.clientID != "" {
		credentials[grant.clientID] = sampleClientID
		credentials[grant.clientSecret] = sampleClientSecret
	}
	if grant.refreshToken != "" {
		credentials[grant.refreshToken] = sampleRefreshToken
	}
	if grant.jwtBearer != nil {
		if subject := grant.jwtBearer.Subject; subject != nil && subject.FromCredential != "" {
			credentials[subject.FromCredential] = sampleSubject
		}
		for _, source := range []schema.ValueSource{grant.jwtBearer.TokenEndpoint, grant.jwtBearer.Audience} {
			if source.FromCredential != "" && len(source.Values) == 0 {
				credentials[source.FromCredential] = sampleTokenEndpoint
			}
		}
		if grant.jwtBearer.KeyCredential != "" {
			credentials[grant.jwtBearer.KeyCredential] = ""
		}
		if grant.jwtBearer.PrivateKey.FromCredential != "" {
			credentials[grant.jwtBearer.PrivateKey.FromCredential] = ""
		}
	}
	for _, otherGrant := range declaredGrants(agreement.manifest) {
		if otherGrant.method.ID == grant.method.ID && otherGrant.refreshToken != "" && otherGrant.refreshToken != grant.refreshToken {
			credentials[otherGrant.refreshToken] = ""
		}
	}
	if variant.field != "" {
		if _, isCredential := credentials[variant.field]; isCredential {
			credentials[variant.field] = variant.value
		}
	}
	for name, value := range sample.Credentials {
		credentials[name] = value
	}
	return credentials
}

func (agreement *refreshAgreementGenerator) sampleSettings(grant declaredGrant, variant grantVariant, sample RefreshAgreementSample) map[string]string {
	settings := map[string]string{}
	for _, source := range agreement.sources(grant) {
		if source.FromSetting == "" {
			continue
		}
		settings[source.FromSetting] = "manifest-" + source.FromSetting
		if enumField, isEnum := agreement.enumField(grant.method, source.FromSetting); isEnum {
			settings[source.FromSetting] = enumField.Enum[0]
		}
		if agreement.isURLSetting(grant.method, source.FromSetting) {
			settings[source.FromSetting] = "https://manifest.example.com/" + source.FromSetting
		}
	}
	for _, placeholder := range agreement.templatePlaceholders(grant) {
		if !agreement.isMethodCredential(grant.method, placeholder) {
			settings[placeholder] = sampleTemplateValue
		}
	}
	if variant.field != "" && !agreement.isMethodCredential(grant.method, variant.field) {
		settings[variant.field] = variant.value
	}
	for name, value := range sample.Settings {
		settings[name] = value
	}
	return settings
}

// sampleKeyMembers gives every JSON key member that a source reads a value; the private key member is added at run time.
func (agreement *refreshAgreementGenerator) sampleKeyMembers(grant declaredGrant, variant grantVariant) map[string]string {
	members := map[string]string{}
	if grant.jwtBearer == nil {
		return members
	}
	jwtBearer := grant.jwtBearer
	for _, source := range agreement.sources(grant) {
		if source.FromKey != "" && source.FromKey != jwtBearer.PrivateKey.FromKey {
			members[source.FromKey] = "manifest-" + source.FromKey
		}
	}
	for _, source := range []schema.ValueSource{jwtBearer.TokenEndpoint, jwtBearer.Audience} {
		if source.FromKey != "" && len(source.Values) == 0 {
			members[source.FromKey] = sampleTokenEndpoint
		}
	}
	if jwtBearer.Issuer.FromKey != "" && len(jwtBearer.Issuer.Values) == 0 && members[jwtBearer.Issuer.FromKey] != sampleTokenEndpoint {
		members[jwtBearer.Issuer.FromKey] = "manifest-issuer@example.com"
	}
	if variant.field != "" {
		if _, isMember := members[variant.field]; isMember {
			members[variant.field] = variant.value
		}
	}
	return members
}

// sampleResponse returns a successful token response and the credential values it must leave behind.
func (agreement *refreshAgreementGenerator) sampleResponse(grant declaredGrant, credentials map[string]string, sample RefreshAgreementSample) (map[string]any, map[string]string) {
	response := map[string]any{"token_type": grant.acceptedTokenTypes[0], "expires_in": sampleExpiresInSecond}
	if len(grant.responseScopes) > 0 {
		response["scope"] = strings.Join(grant.responseScopes, " ")
	}
	updates := map[string]string{}
	for _, mapping := range grant.mappings {
		value := "https://manifest-" + strings.ReplaceAll(strings.ReplaceAll(mapping.Source, "_", "-"), ".", "-") + ".example.com"
		switch mapping.Source {
		case "access_token":
			value = sampleAccessToken + "-2"
		case "refresh_token":
			value = sampleRefreshToken + "-2"
		}
		if override, found := sample.Response[mapping.Source]; found {
			value = override
		}
		setResponsePath(response, mapping.Source, value)
		updates[mapping.Credential] = value
	}
	for _, credentialCopy := range grant.copies {
		updates[credentialCopy.Credential] = credentials[credentialCopy.FromCredential]
	}
	for name, value := range sample.Response {
		setResponsePath(response, name, value)
	}
	return response, updates
}

func (agreement *refreshAgreementGenerator) sources(grant declaredGrant) []schema.ValueSource {
	sources := []schema.ValueSource{grant.endpoint, grant.authMethod}
	if jwtBearer := grant.jwtBearer; jwtBearer != nil {
		sources = append(sources, jwtBearer.PrivateKey, jwtBearer.Issuer, jwtBearer.Audience, jwtBearer.TokenEndpoint)
		if jwtBearer.Subject != nil {
			sources = append(sources, *jwtBearer.Subject)
		}
	}
	return sources
}

func (agreement *refreshAgreementGenerator) templatePlaceholders(grant declaredGrant) []string {
	var placeholders []string
	for _, source := range agreement.sources(grant) {
		for _, placeholder := range templatePlaceholders(source.Template) {
			placeholders = append(placeholders, placeholder)
		}
	}
	return placeholders
}

// readsSettings reports whether any declaration reads a connection setting, which the driver then takes from its
// test-only constructor.
func (agreement *refreshAgreementGenerator) readsSettings(grants []declaredGrant) bool {
	for _, grant := range grants {
		if len(agreement.sampleSettings(grant, grantVariant{}, RefreshAgreementSample{})) > 0 {
			return true
		}
	}
	return false
}

// enumField returns the enum credential field or setting of method that name names.
func (agreement *refreshAgreementGenerator) enumField(method schema.AuthMethod, name string) (schema.Field, bool) {
	fields := append(append([]schema.Field{}, method.Fields...), agreement.manifest.Spec.Configuration.Fields...)
	if method.Configuration != nil {
		fields = append(fields, method.Configuration.Fields...)
	}
	for _, field := range fields {
		if field.Name == name && field.Type == "enum" && len(field.Enum) > 0 {
			return field, true
		}
	}
	return schema.Field{}, false
}

// isURLSetting reports whether name names a url setting of method.
func (agreement *refreshAgreementGenerator) isURLSetting(method schema.AuthMethod, name string) bool {
	fields := append([]schema.Field{}, agreement.manifest.Spec.Configuration.Fields...)
	if method.Configuration != nil {
		fields = append(fields, method.Configuration.Fields...)
	}
	for _, field := range fields {
		if field.Name == name {
			return field.Type == "url"
		}
	}
	return false
}

func (agreement *refreshAgreementGenerator) isMethodCredential(method schema.AuthMethod, name string) bool {
	for _, field := range method.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func (agreement *refreshAgreementGenerator) isRequiredCredential(method schema.AuthMethod, name string) bool {
	for _, field := range method.Fields {
		if field.Name == name {
			return field.Required
		}
	}
	return true
}

// writeStaticMethods lists the credential methods that declare no grant, with sample credentials.
func (agreement *refreshAgreementGenerator) writeStaticMethods(generation *generator, grants []declaredGrant) {
	hasGrant := map[string]bool{}
	for _, grant := range grants {
		hasGrant[grant.methodLabel] = true
	}
	generation.mustWrite("func manifestStaticMethods() []manifestStaticMethod {\n\treturn []manifestStaticMethod{\n")
	for _, method := range agreement.manifest.Spec.Auth.Methods {
		if hasGrant[method.ID] || method.Type == "none" {
			continue
		}
		generation.mustWrite("\t\t{name: %q, credentials: %s.Credentials{\n\t\t\tAuthMethodID: %q,\n", method.ID, agreement.connector, method.ID)
		for _, field := range method.Fields {
			if !isStringLikeField(field) {
				continue
			}
			value := "manifest-" + field.Name
			if field.Type == "enum" && len(field.Enum) > 0 {
				value = field.Enum[0]
			}
			generation.mustWrite("\t\t\t%s: %s,\n", field.GoName, typedFieldExpression(agreement.connector, field, strconv.Quote(value)))
		}
		generation.mustWrite("\t\t}},\n")
	}
	generation.mustWrite("\t}\n}\n")
}

// writeCredentialReaders writes the functions that read string credential values and the selected method.
func (agreement *refreshAgreementGenerator) writeCredentialReaders(generation *generator) {
	generation.mustWrite("func manifestCredentialValues(credentials %s.Credentials) map[string]string {\n\treturn map[string]string{\n", agreement.connector)
	for _, field := range agreement.manifest.Spec.Auth.Fields {
		if !isStringLikeField(field) {
			continue
		}
		value := "credentials." + field.GoName
		switch field.Type {
		case "secretString":
			value += ".Reveal()"
		case "enum":
			value = "string(" + value + ")"
		}
		generation.mustWrite("\t\t%q: %s,\n", field.Name, value)
	}
	generation.mustWrite("\t}\n}\n\n")
	generation.mustWrite("func manifestAuthMethodID(credentials %s.Credentials) string {\n", agreement.connector)
	if len(agreement.manifest.Spec.Auth.Methods) > 0 {
		generation.mustWrite("\treturn credentials.AuthMethodID\n}\n\n")
	} else {
		generation.mustWrite("\treturn \"\"\n}\n\n")
	}
}

// resolveSampleSource returns the value a host resolves for source from the sample connection.
func resolveSampleSource(source schema.ValueSource, credentials, settings, keyMembers map[string]string) (string, error) {
	var value string
	switch {
	case source.Value != "":
		return source.Value, nil
	case source.FromCredential != "":
		value = credentials[source.FromCredential]
	case source.FromSetting != "":
		value = settings[source.FromSetting]
	case source.FromKey != "":
		value = keyMembers[source.FromKey]
	case source.Template != "":
		resolved := source.Template
		for _, placeholder := range templatePlaceholders(source.Template) {
			placeholderValue, isCredential := credentials[placeholder]
			if !isCredential {
				placeholderValue = settings[placeholder]
			}
			resolved = strings.ReplaceAll(resolved, "{"+placeholder+"}", url.PathEscape(placeholderValue))
		}
		return resolved, nil
	default:
		return "", fmt.Errorf("value source is empty")
	}
	if len(source.Values) == 0 {
		return value, nil
	}
	translated, found := source.Values[value]
	if !found {
		return "", fmt.Errorf("values do not translate %q", value)
	}
	return translated, nil
}

func templatePlaceholders(template string) []string {
	var placeholders []string
	for _, segment := range strings.Split(template, "{")[1:] {
		placeholder, _, _ := strings.Cut(segment, "}")
		placeholders = append(placeholders, placeholder)
	}
	return placeholders
}

func sourceField(source schema.ValueSource) string {
	switch {
	case source.FromCredential != "":
		return source.FromCredential
	case source.FromSetting != "":
		return source.FromSetting
	default:
		return source.FromKey
	}
}

// setResponsePath sets a dotted JSON path of a token response.
func setResponsePath(response map[string]any, path, value string) {
	segments := strings.Split(path, ".")
	current := response
	for _, segment := range segments[:len(segments)-1] {
		next, isObject := current[segment].(map[string]any)
		if !isObject {
			next = map[string]any{}
			current[segment] = next
		}
		current = next
	}
	current[segments[len(segments)-1]] = value
}

func declaredErrorCodes(grants []declaredGrant) []string {
	seen := map[string]bool{}
	var codes []string
	for _, grant := range grants {
		for _, code := range grant.terminalCodes {
			if !seen[code] {
				seen[code] = true
				codes = append(codes, code)
			}
		}
	}
	sort.Strings(codes)
	return codes
}

func isStringLikeField(field schema.Field) bool {
	switch field.Type {
	case "string", "url", "secretString", "enum":
		return true
	default:
		return false
	}
}

func typedFieldExpression(connector string, field schema.Field, expression string) string {
	switch field.Type {
	case "secretString":
		return "sdkgo.NewSecretString(" + expression + ")"
	case "enum":
		return connector + "." + field.GoName + "(" + expression + ")"
	default:
		return expression
	}
}

// clientAuthentication returns the declared token endpoint client authentication, client_secret_post by default.
func clientAuthentication(source *schema.ValueSource) schema.ValueSource {
	if source == nil {
		return schema.ValueSource{Value: schema.ClientSecretPostAuthMethod}
	}
	return *source
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultStrings(values []string, fallback string) []string {
	if len(values) == 0 {
		return []string{fallback}
	}
	return values
}

func withoutString(values []string, removed string) []string {
	var kept []string
	for _, value := range values {
		if value != removed {
			kept = append(kept, value)
		}
	}
	return kept
}

func goStringSlice(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}

func goStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, strconv.Quote(key)+": "+strconv.Quote(values[key]))
	}
	return "map[string]string{" + strings.Join(entries, ", ") + "}"
}

func goAnyMap(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		var expression string
		switch value := values[key].(type) {
		case string:
			expression = strconv.Quote(value)
		case int:
			expression = strconv.Itoa(value)
		case map[string]any:
			expression = goAnyMap(value)
		default:
			panic(fmt.Sprintf("unsupported token response value %T", value))
		}
		entries = append(entries, strconv.Quote(key)+": "+expression)
	}
	return "map[string]any{" + strings.Join(entries, ", ") + "}"
}
