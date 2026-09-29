// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package schema loads and validates connector manifests.
package schema

import (
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const APIVersion = "connectors.dex.dev/v1alpha1"

type Manifest struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name        string `yaml:"name" json:"name"`
	DisplayName string `yaml:"displayName" json:"displayName"`
	Description string `yaml:"description" json:"description"`
	Company     string `yaml:"company" json:"company"`
	Version     string `yaml:"version" json:"version"`
}

type Spec struct {
	Provider      string        `yaml:"provider" json:"provider"`
	Codegen       Codegen       `yaml:"codegen" json:"codegen"`
	Configuration Configuration `yaml:"configuration" json:"configuration"`
	Auth          Auth          `yaml:"auth" json:"auth"`
	Studio        *Studio       `yaml:"studio,omitempty" json:"studio,omitempty"`
	Triggers      []Trigger     `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Operations    []Operation   `yaml:"operations" json:"operations"`
}

type Studio struct {
	Setup    StudioSetup     `yaml:"setup" json:"setup"`
	Commands []StudioCommand `yaml:"commands,omitempty" json:"commands,omitempty"`
	Units    []StudioUnit    `yaml:"units,omitempty" json:"units,omitempty"`
}

type StudioCommand struct {
	ID         string                   `yaml:"id" json:"id"`
	Capability string                   `yaml:"capability" json:"capability"`
	Request    StudioCommandHTTPRequest `yaml:"request" json:"request"`
}

type StudioCommandHTTPRequest struct {
	Method     string                  `yaml:"method" json:"method"`
	URL        string                  `yaml:"url" json:"url"`
	Credential StudioCommandCredential `yaml:"credential" json:"credential"`
	FixedQuery map[string]string       `yaml:"fixedQuery,omitempty" json:"fixedQuery,omitempty"`
	// FixedHeaders are non-secret header values, such as an API version, sent
	// with every request. Names compare case-insensitively, and "_" matches "-".
	FixedHeaders map[string]string        `yaml:"fixedHeaders,omitempty" json:"fixedHeaders,omitempty"`
	Parameters   []StudioCommandParameter `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

type StudioCommandCredential struct {
	Field string `yaml:"field" json:"field"`
	// Scheme is bearer, which sends Authorization: Bearer <secret>, or header,
	// which sends the raw secret in Header.
	Scheme string `yaml:"scheme" json:"scheme"`
	// Header names the request header that carries the raw secret. Only the
	// header scheme declares it.
	Header string `yaml:"header,omitempty" json:"header,omitempty"`
}

type StudioCommandParameter struct {
	Name     string `yaml:"name" json:"name"`
	Location string `yaml:"location" json:"location"`
	Target   string `yaml:"target" json:"target"`
}

type StudioSetup struct {
	Entrypoint          string   `yaml:"entrypoint" json:"entrypoint"`
	HostAPIRange        string   `yaml:"hostApiRange" json:"hostApiRange"`
	BackendCapabilities []string `yaml:"backendCapabilities" json:"backendCapabilities"`
	MockScenarios       []string `yaml:"mockScenarios" json:"mockScenarios"`
	Icon                string   `yaml:"icon" json:"icon"`
}

type StudioUnit struct {
	ID                  string           `yaml:"id" json:"id"`
	GoName              string           `yaml:"goName" json:"goName"`
	Description         string           `yaml:"description" json:"description"`
	BackendCapabilities []string         `yaml:"backendCapabilities,omitempty" json:"backendCapabilities,omitempty"`
	Inputs              []StudioUnitPort `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs             []StudioUnitPort `yaml:"outputs" json:"outputs"`
}

type StudioUnitPort struct {
	Name   string `yaml:"name" json:"name"`
	GoName string `yaml:"goName" json:"goName"`
	Type   string `yaml:"type" json:"type"`
}

type Codegen struct {
	Go GoCodegen `yaml:"go" json:"go"`
}

type GoCodegen struct {
	Package string `yaml:"package" json:"package"`
}

type Configuration struct {
	Fields []Field `yaml:"fields" json:"fields"`
}

type Auth struct {
	Type           string              `yaml:"type,omitempty" json:"type,omitempty"`
	ConnectionKind string              `yaml:"connectionKind,omitempty" json:"connectionKind,omitempty"`
	Fields         []Field             `yaml:"fields,omitempty" json:"fields,omitempty"`
	Guide          *AuthorizationGuide `yaml:"guide,omitempty" json:"guide,omitempty"`
	OAuth2         *OAuth2             `yaml:"oauth2,omitempty" json:"oauth2,omitempty"`
	DefaultMethod  string              `yaml:"defaultMethod,omitempty" json:"defaultMethod,omitempty"`
	Methods        []AuthMethod        `yaml:"methods,omitempty" json:"methods,omitempty"`
}

type AuthMethod struct {
	ID             string              `yaml:"id" json:"id"`
	DisplayName    string              `yaml:"displayName" json:"displayName"`
	Description    string              `yaml:"description" json:"description"`
	Recommended    bool                `yaml:"recommended,omitempty" json:"recommended,omitempty"`
	Type           string              `yaml:"type" json:"type"`
	ConnectionKind string              `yaml:"connectionKind" json:"connectionKind"`
	Fields         []Field             `yaml:"fields" json:"fields"`
	Guide          *AuthorizationGuide `yaml:"guide,omitempty" json:"guide,omitempty"`
	OAuth2         *OAuth2             `yaml:"oauth2,omitempty" json:"oauth2,omitempty"`
}

type AuthorizationGuide struct {
	StartURL string   `yaml:"startURL" json:"startURL"`
	Steps    []string `yaml:"steps" json:"steps"`
}

type OAuth2 struct {
	AuthorizationEndpoint   string                      `yaml:"authorizationEndpoint" json:"authorizationEndpoint"`
	TokenEndpoint           string                      `yaml:"tokenEndpoint" json:"tokenEndpoint"`
	AuthorizationParameters map[string]string           `yaml:"authorizationParameters,omitempty" json:"authorizationParameters,omitempty"`
	ClientIDCredential      string                      `yaml:"clientIDCredential,omitempty" json:"clientIDCredential,omitempty"`
	ClientSecretCredential  string                      `yaml:"clientSecretCredential,omitempty" json:"clientSecretCredential,omitempty"`
	Scopes                  []string                    `yaml:"scopes" json:"scopes"`
	UserScopes              []string                    `yaml:"userScopes,omitempty" json:"userScopes,omitempty"`
	CredentialMappings      []OAuthCredentialMapping    `yaml:"credentialMappings,omitempty" json:"credentialMappings,omitempty"`
	CredentialDerivations   []OAuthCredentialDerivation `yaml:"credentialDerivations,omitempty" json:"credentialDerivations,omitempty"`
	PKCE                    bool                        `yaml:"pkce" json:"pkce"`
	Protocol                string                      `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	OIDC                    *OIDC                       `yaml:"oidc,omitempty" json:"oidc,omitempty"`
}

type OAuthCredentialMapping struct {
	Credential string `yaml:"credential" json:"credential"`
	Source     string `yaml:"source" json:"source"`
}

type OAuthCredentialDerivation struct {
	Credential string `yaml:"credential" json:"credential"`
	Endpoint   string `yaml:"endpoint" json:"endpoint"`
	Source     string `yaml:"source" json:"source"`
	VerifiedBy string `yaml:"verifiedBy,omitempty" json:"verifiedBy,omitempty"`
}

type OIDC struct {
	Issuer            string `yaml:"issuer" json:"issuer"`
	DiscoveryEndpoint string `yaml:"discoveryEndpoint" json:"discoveryEndpoint"`
	UserInfoEndpoint  string `yaml:"userInfoEndpoint" json:"userInfoEndpoint"`
	NonceRequired     bool   `yaml:"nonceRequired" json:"nonceRequired"`
}

type Field struct {
	Name        string   `yaml:"name" json:"name"`
	GoName      string   `yaml:"goName" json:"goName"`
	Type        string   `yaml:"type" json:"type"`
	Description string   `yaml:"description" json:"description"`
	Required    bool     `yaml:"required" json:"required"`
	Default     any      `yaml:"default,omitempty" json:"default,omitempty"`
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`
}

type Operation struct {
	Name          string            `yaml:"name" json:"name"`
	GoName        string            `yaml:"goName" json:"goName"`
	InputType     string            `yaml:"inputType" json:"inputType"`
	OutputType    string            `yaml:"outputType" json:"outputType"`
	Kind          string            `yaml:"kind" json:"kind"`
	Description   string            `yaml:"description" json:"description"`
	Idempotency   string            `yaml:"idempotency" json:"idempotency"`
	Branches      []OperationBranch `yaml:"branches" json:"branches"`
	Progress      []string          `yaml:"progress,omitempty" json:"progress,omitempty"`
	Authorization string            `yaml:"authorization,omitempty" json:"authorization,omitempty"`
	Execution     Execution         `yaml:"execution" json:"execution"`
}

type Trigger struct {
	Name              string `yaml:"name" json:"name"`
	GoName            string `yaml:"goName" json:"goName"`
	EventType         string `yaml:"eventType" json:"eventType"`
	ConfigurationType string `yaml:"configurationType" json:"configurationType"`
	Description       string `yaml:"description" json:"description"`
}

type OperationBranch struct {
	ID          string `yaml:"id" json:"id"`
	GoName      string `yaml:"goName" json:"goName"`
	Description string `yaml:"description" json:"description"`
	Optional    bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type Execution struct {
	ExecuteMethodTimeout string `yaml:"executeMethodTimeout" json:"executeMethodTimeout"`
	HeartbeatTimeout     string `yaml:"heartbeatTimeout,omitempty" json:"heartbeatTimeout,omitempty"`
	Durability           string `yaml:"durability" json:"durability"`
	Retry                Retry  `yaml:"retry" json:"retry"`
}

type Retry struct {
	InitialInterval    string  `yaml:"initialInterval" json:"initialInterval"`
	BackoffCoefficient float64 `yaml:"backoffCoefficient" json:"backoffCoefficient"`
	MaximumInterval    string  `yaml:"maximumInterval" json:"maximumInterval"`
	MaximumAttempts    int32   `yaml:"maximumAttempts" json:"maximumAttempts"`
	TotalDuration      string  `yaml:"totalDuration" json:"totalDuration"`
}

var (
	namePattern         = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	operationPattern    = regexp.MustCompile(`^[a-z][A-Za-z0-9]+$`)
	authMethodIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	goNamePattern       = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	fieldNamePattern    = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)
	capabilityPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*)+$`)
	mockScenarioPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	versionPattern      = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	// httpHeaderTokenPattern is the RFC 7230 token grammar for header field names.
	httpHeaderTokenPattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
	// studioCommandHeaderValuePattern is printable ASCII without surrounding spaces.
	studioCommandHeaderValuePattern = regexp.MustCompile(`^[\x21-\x7E](?:[\x20-\x7E]*[\x21-\x7E])?$`)
)

const maximumStudioCommandFixedHeaderValueLength = 256

// studioCommandReservedHeaders are lowercase names the Dex Web broker, the
// transport, or routing intermediaries own.
var studioCommandReservedHeaders = map[string]bool{
	"accept": true, "connection": true, "content-length": true, "cookie": true,
	"forwarded": true, "host": true, "keep-alive": true, "origin": true,
	"referer": true, "set-cookie": true, "te": true, "trailer": true,
	"transfer-encoding": true, "upgrade": true,
	"x-http-method": true, "x-http-method-override": true, "x-method-override": true,
}

func Decode(reader io.Reader) (Manifest, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	manifest.setImplicitAuthDefaults()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	for index := range manifest.Spec.Operations {
		sort.Strings(manifest.Spec.Operations[index].Progress)
	}
	return manifest, nil
}

func (manifest *Manifest) setImplicitAuthDefaults() {
	if manifest.Spec.Auth.OAuth2 != nil && manifest.Spec.Auth.OAuth2.Protocol == "" {
		manifest.Spec.Auth.OAuth2.Protocol = "oauth2"
	}
	for index := range manifest.Spec.Auth.Methods {
		method := &manifest.Spec.Auth.Methods[index]
		if method.OAuth2 != nil && method.OAuth2.Protocol == "" {
			method.OAuth2.Protocol = "oauth2"
		}
	}
	if len(manifest.Spec.Auth.Methods) > 0 {
		manifest.Spec.Auth.Fields = manifest.Spec.Auth.allFields()
	}
	for index := range manifest.Spec.Operations {
		if manifest.Spec.Operations[index].Authorization != "" {
			continue
		}
		if !manifest.Spec.Auth.requiresAuthorization() {
			manifest.Spec.Operations[index].Authorization = "none"
		} else {
			manifest.Spec.Operations[index].Authorization = "required"
		}
	}
}

func (auth Auth) requiresAuthorization() bool {
	if len(auth.Methods) == 0 {
		return auth.Type != "none"
	}
	for _, method := range auth.Methods {
		if method.Type != "none" {
			return true
		}
	}
	return false
}

func (auth Auth) allFields() []Field {
	if len(auth.Methods) == 0 {
		return auth.Fields
	}
	fields := make([]Field, 0)
	seen := make(map[string]bool)
	for _, method := range auth.Methods {
		for _, field := range method.Fields {
			if seen[field.Name] {
				continue
			}
			field.Required = false
			fields = append(fields, field)
			seen[field.Name] = true
		}
	}
	return fields
}

func (manifest Manifest) Validate() error {
	var problems []string
	if manifest.APIVersion != APIVersion {
		problems = append(problems, "apiVersion must be "+APIVersion)
	}
	if manifest.Kind != "Connector" {
		problems = append(problems, "kind must be Connector")
	}
	if !namePattern.MatchString(manifest.Metadata.Name) {
		problems = append(problems, "metadata.name must be DNS-like and 2-63 characters")
	}
	if strings.TrimSpace(manifest.Metadata.DisplayName) == "" || strings.TrimSpace(manifest.Metadata.Description) == "" {
		problems = append(problems, "metadata.displayName and metadata.description are required")
	}
	if strings.TrimSpace(manifest.Metadata.Company) == "" {
		problems = append(problems, "metadata.company is required")
	}
	if !versionPattern.MatchString(manifest.Metadata.Version) {
		problems = append(problems, "metadata.version must be a stable semantic version with a v prefix")
	}
	if strings.TrimSpace(manifest.Spec.Provider) == "" {
		problems = append(problems, "spec.provider is required")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(manifest.Spec.Codegen.Go.Package) {
		problems = append(problems, "spec.codegen.go.package must be a Go package name")
	}
	problems = append(problems, validateFields("configuration", manifest.Spec.Configuration.Fields, false)...)
	if len(manifest.Spec.Auth.Methods) == 0 {
		problems = append(problems, validateAuthMethod("spec.auth", AuthMethod{
			Type: manifest.Spec.Auth.Type, ConnectionKind: manifest.Spec.Auth.ConnectionKind,
			Fields: manifest.Spec.Auth.Fields, Guide: manifest.Spec.Auth.Guide, OAuth2: manifest.Spec.Auth.OAuth2,
		})...)
	} else {
		if manifest.Spec.Auth.Type != "" || manifest.Spec.Auth.ConnectionKind != "" || manifest.Spec.Auth.Guide != nil || manifest.Spec.Auth.OAuth2 != nil {
			problems = append(problems, "spec.auth methods cannot be combined with legacy auth fields")
		}
		seenMethodIDs := make(map[string]bool, len(manifest.Spec.Auth.Methods))
		seenCredentialFields := make(map[string]Field)
		recommendedMethods := 0
		for _, method := range manifest.Spec.Auth.Methods {
			if !authMethodIDPattern.MatchString(method.ID) || seenMethodIDs[method.ID] {
				problems = append(problems, "spec.auth method IDs must be unique lowercase kebab-case values")
			}
			seenMethodIDs[method.ID] = true
			if strings.TrimSpace(method.DisplayName) == "" || strings.TrimSpace(method.Description) == "" {
				problems = append(problems, "spec.auth method "+method.ID+" requires displayName and description")
			}
			if method.Recommended {
				recommendedMethods++
			}
			problems = append(problems, validateAuthMethod("spec.auth method "+method.ID, method)...)
			for _, field := range method.Fields {
				if field.Name == "auth_method" || field.GoName == "AuthMethodID" {
					problems = append(problems, "auth_method and AuthMethodID are reserved for auth method selection")
				}
				if existing, found := seenCredentialFields[field.Name]; found && (existing.GoName != field.GoName || existing.Type != field.Type) {
					problems = append(problems, "credential fields shared by auth methods must use the same goName and type")
				}
				seenCredentialFields[field.Name] = field
			}
		}
		if manifest.Spec.Auth.DefaultMethod == "" || !seenMethodIDs[manifest.Spec.Auth.DefaultMethod] {
			problems = append(problems, "spec.auth.defaultMethod must identify a declared auth method")
		}
		if recommendedMethods > 1 {
			problems = append(problems, "spec.auth may recommend at most one auth method")
		}
	}
	if manifest.Spec.Studio != nil {
		setup := manifest.Spec.Studio.Setup
		if !isSafeStudioAssetPath(setup.Entrypoint, ".html") {
			problems = append(problems, "studio setup entrypoint must be a safe relative .html path")
		}
		if !isSafeStudioIconPath(setup.Icon) {
			problems = append(problems, "studio setup icon must be a safe relative .png or .svg path")
		}
		if strings.TrimSpace(setup.HostAPIRange) == "" {
			problems = append(problems, "studio setup hostApiRange is required")
		}
		if len(setup.BackendCapabilities) == 0 || !hasValidUniqueStrings(setup.BackendCapabilities, capabilityPattern) {
			problems = append(problems, "studio setup backendCapabilities must be non-empty, unique capability IDs")
		}
		if len(setup.MockScenarios) == 0 || !hasValidUniqueStrings(setup.MockScenarios, mockScenarioPattern) {
			problems = append(problems, "studio setup mockScenarios must be non-empty, unique scenario IDs")
		}
		setupCapabilities := make(map[string]bool, len(setup.BackendCapabilities))
		for _, capability := range setup.BackendCapabilities {
			setupCapabilities[capability] = true
		}
		authFields := make(map[string]Field, len(manifest.Spec.Auth.Fields))
		for _, field := range manifest.Spec.Auth.Fields {
			authFields[field.Name] = field
		}
		seenCommandIDs := map[string]bool{}
		for _, command := range manifest.Spec.Studio.Commands {
			if !operationPattern.MatchString(command.ID) || seenCommandIDs[command.ID] {
				problems = append(problems, "studio command IDs must be lower camel case and unique")
			}
			seenCommandIDs[command.ID] = true
			if !capabilityPattern.MatchString(command.Capability) || !setupCapabilities[command.Capability] {
				problems = append(problems, "studio command "+command.ID+": capability must be declared by studio setup")
			}
			problems = append(problems, validateStudioCommand(command, authFields)...)
		}
		seenUnitIDs := map[string]bool{}
		seenUnitGoNames := map[string]bool{}
		for _, unit := range manifest.Spec.Studio.Units {
			if !operationPattern.MatchString(unit.ID) || seenUnitIDs[unit.ID] {
				problems = append(problems, "studio unit IDs must be lower camel case and unique")
			}
			seenUnitIDs[unit.ID] = true
			if !goNamePattern.MatchString(unit.GoName) || seenUnitGoNames[unit.GoName] {
				problems = append(problems, "studio units require unique exported goName values")
			}
			seenUnitGoNames[unit.GoName] = true
			if strings.TrimSpace(unit.Description) == "" {
				problems = append(problems, "studio unit "+unit.ID+": description is required")
			}
			if !hasValidUniqueStrings(unit.BackendCapabilities, capabilityPattern) {
				problems = append(problems, "studio unit "+unit.ID+": backendCapabilities must be unique capability IDs")
			}
			for _, capability := range unit.BackendCapabilities {
				if !setupCapabilities[capability] {
					problems = append(problems, "studio unit "+unit.ID+": backendCapabilities must be declared by studio setup")
				}
			}
			if len(unit.Outputs) == 0 {
				problems = append(problems, "studio unit "+unit.ID+": at least one output port is required")
			}
			problems = append(problems, validateStudioUnitPorts(unit.ID, unit.Inputs, unit.Outputs)...)
		}
	}
	if len(manifest.Spec.Operations) == 0 {
		problems = append(problems, "spec.operations must contain at least one operation")
	}
	seenTriggers := map[string]bool{}
	seenTriggerGoNames := map[string]bool{}
	for _, trigger := range manifest.Spec.Triggers {
		if !operationPattern.MatchString(trigger.Name) || seenTriggers[trigger.Name] {
			problems = append(problems, "trigger names must be lower camel case and unique")
		}
		seenTriggers[trigger.Name] = true
		if !goNamePattern.MatchString(trigger.GoName) || seenTriggerGoNames[trigger.GoName] {
			problems = append(problems, trigger.Name+": goName must be exported and unique")
		}
		seenTriggerGoNames[trigger.GoName] = true
		if !goNamePattern.MatchString(trigger.EventType) || !goNamePattern.MatchString(trigger.ConfigurationType) {
			problems = append(problems, trigger.Name+": eventType and configurationType must name exported local Go types")
		}
		if strings.TrimSpace(trigger.Description) == "" {
			problems = append(problems, trigger.Name+": description is required")
		}
	}
	seenOperations := map[string]bool{}
	seenOperationGoNames := map[string]bool{}
	for _, operation := range manifest.Spec.Operations {
		if !operationPattern.MatchString(operation.Name) || seenOperations[operation.Name] {
			problems = append(problems, "operation names must be lower camel case and unique")
		}
		seenOperations[operation.Name] = true
		if !goNamePattern.MatchString(operation.GoName) || seenOperationGoNames[operation.GoName] {
			problems = append(problems, operation.Name+": goName must be exported and unique")
		}
		seenOperationGoNames[operation.GoName] = true
		if !goNamePattern.MatchString(operation.InputType) || !goNamePattern.MatchString(operation.OutputType) {
			problems = append(problems, operation.Name+": inputType and outputType must name exported local Go types")
		}
		if operation.Kind != "query" && operation.Kind != "mutation" {
			problems = append(problems, operation.Name+": kind must be query or mutation")
		}
		if strings.TrimSpace(operation.Description) == "" {
			problems = append(problems, operation.Name+": description is required")
		}
		if operation.Idempotency != "none" && operation.Idempotency != "required" {
			problems = append(problems, operation.Name+": invalid idempotency")
		}
		if operation.Kind == "mutation" && operation.Idempotency == "none" {
			problems = append(problems, operation.Name+": mutations must declare required idempotency")
		}
		if operation.Kind == "query" && operation.Idempotency != "none" {
			problems = append(problems, operation.Name+": queries must declare no idempotency")
		}
		branches := map[string]bool{}
		branchGoNames := map[string]bool{}
		for _, branch := range operation.Branches {
			if !operationPattern.MatchString(branch.ID) || branches[branch.ID] || !goNamePattern.MatchString(branch.GoName) || branchGoNames[branch.GoName] || strings.TrimSpace(branch.Description) == "" {
				problems = append(problems, operation.Name+": branches require unique lower camel IDs, goName, and description")
			}
			branches[branch.ID] = true
			branchGoNames[branch.GoName] = true
		}
		if len(branches) == 0 || !branches["defect"] {
			problems = append(problems, operation.Name+": branches must declare defect")
		}
		if operation.Kind == "query" && branches["uncertain"] {
			problems = append(problems, operation.Name+": query cannot declare uncertain")
		}
		if operation.Authorization != "none" && operation.Authorization != "required" {
			problems = append(problems, operation.Name+": authorization must be none or required")
		}
		if operation.Authorization == "required" && !manifest.Spec.Auth.requiresAuthorization() {
			problems = append(problems, operation.Name+": required authorization needs connector auth")
		}
		seenProgress := map[string]bool{}
		for _, capability := range operation.Progress {
			if capability != "structured" && capability != "text" {
				problems = append(problems, operation.Name+": progress must contain only structured or text")
			}
			if seenProgress[capability] {
				problems = append(problems, operation.Name+": progress capabilities must be unique")
			}
			seenProgress[capability] = true
		}
		if _, err := time.ParseDuration(operation.Execution.ExecuteMethodTimeout); err != nil {
			problems = append(problems, operation.Name+": executeMethodTimeout must be a duration")
		}
		if operation.Execution.HeartbeatTimeout != "" {
			if _, err := time.ParseDuration(operation.Execution.HeartbeatTimeout); err != nil {
				problems = append(problems, operation.Name+": heartbeatTimeout must be a duration")
			}
		}
		if operation.Execution.Durability != "sync" && operation.Execution.Durability != "async" {
			problems = append(problems, operation.Name+": execution durability must be sync or async")
		}
		if _, err := time.ParseDuration(operation.Execution.Retry.InitialInterval); err != nil {
			problems = append(problems, operation.Name+": retry initialInterval must be a duration")
		}
		if _, err := time.ParseDuration(operation.Execution.Retry.MaximumInterval); err != nil {
			problems = append(problems, operation.Name+": retry maximumInterval must be a duration")
		}
		if _, err := time.ParseDuration(operation.Execution.Retry.TotalDuration); err != nil {
			problems = append(problems, operation.Name+": retry totalDuration must be a duration")
		}
		if operation.Execution.Retry.MaximumAttempts < 1 || operation.Execution.Retry.BackoffCoefficient < 1 {
			problems = append(problems, operation.Name+": retry attempts and backoff must be positive")
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("invalid connector manifest: %s", strings.Join(problems, "; "))
	}
	return nil
}

func validateStudioUnitPorts(unitID string, inputPorts []StudioUnitPort, outputPorts []StudioUnitPort) []string {
	var problems []string
	seenNames := map[string]bool{}
	seenGoNames := map[string]bool{}
	allowedTypes := map[string]bool{
		"string": true, "stringList": true, "integer": true, "number": true, "boolean": true,
	}
	for _, port := range append(append([]StudioUnitPort(nil), inputPorts...), outputPorts...) {
		if !operationPattern.MatchString(port.Name) || seenNames[port.Name] {
			problems = append(problems, "studio unit "+unitID+": port names must be lower camel case and unique")
		}
		seenNames[port.Name] = true
		if !goNamePattern.MatchString(port.GoName) || seenGoNames[port.GoName] || !allowedTypes[port.Type] {
			problems = append(problems, "studio unit "+unitID+": ports require unique exported goName values and supported types")
		}
		seenGoNames[port.GoName] = true
	}
	return problems
}

func validateStudioCommand(command StudioCommand, authFields map[string]Field) []string {
	request := command.Request
	prefix := "studio command " + command.ID + ": "
	problems := []string{}
	if request.Method != "GET" {
		problems = append(problems, prefix+"request method must be GET")
	}
	target, err := url.Parse(request.URL)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		problems = append(problems, prefix+"request URL must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	credential, exists := authFields[request.Credential.Field]
	if !exists || credential.Type != "secretString" || (request.Credential.Scheme != "bearer" && request.Credential.Scheme != "header") {
		problems = append(problems, prefix+"credential must name a secretString auth field with bearer or header scheme")
	}
	switch {
	case request.Credential.Scheme == "header" && request.Credential.Header == "":
		problems = append(problems, prefix+"header credential scheme requires a header name")
	case request.Credential.Scheme == "header":
		if reason := describeStudioCommandHeaderNameProblem(request.Credential.Header); reason != "" {
			problems = append(problems, prefix+"credential header "+strconv.QuoteToASCII(request.Credential.Header)+" "+reason)
		}
	case request.Credential.Header != "":
		problems = append(problems, prefix+"credential header is allowed only with the header scheme")
	}
	problems = append(problems, validateStudioCommandFixedHeaders(prefix, request)...)
	seenParameters := map[string]bool{}
	seenTargets := map[string]bool{}
	for _, parameter := range request.Parameters {
		if !fieldNamePattern.MatchString(parameter.Name) || seenParameters[parameter.Name] {
			problems = append(problems, prefix+"parameter names must be lower camel case and unique")
		}
		seenParameters[parameter.Name] = true
		if strings.TrimSpace(parameter.Target) == "" || seenTargets[parameter.Location+"\x00"+parameter.Target] {
			problems = append(problems, prefix+"parameter targets must be non-empty and unique per location")
		}
		seenTargets[parameter.Location+"\x00"+parameter.Target] = true
		switch parameter.Location {
		case "query":
			if _, fixed := request.FixedQuery[parameter.Target]; fixed {
				problems = append(problems, prefix+"query parameter targets cannot replace fixed query values")
			}
		case "path":
			placeholder := "{" + parameter.Target + "}"
			if strings.Count(request.URL, placeholder) != 1 {
				problems = append(problems, prefix+"path parameter target must have exactly one URL placeholder")
			}
		default:
			problems = append(problems, prefix+"parameter location must be query or path")
		}
	}
	for name := range request.FixedQuery {
		if strings.TrimSpace(name) == "" {
			problems = append(problems, prefix+"fixed query names must be non-empty")
		}
	}
	if target != nil {
		for _, match := range regexp.MustCompile(`\{([^{}]+)\}`).FindAllStringSubmatch(target.Path, -1) {
			if !seenTargets["path\x00"+match[1]] {
				problems = append(problems, prefix+"URL contains an undeclared path placeholder")
			}
		}
	}
	return problems
}

func validateStudioCommandFixedHeaders(prefix string, request StudioCommandHTTPRequest) []string {
	problems := []string{}
	headerNames := make([]string, 0, len(request.FixedHeaders))
	for name := range request.FixedHeaders {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	seenFoldedNames := map[string]bool{}
	for _, name := range headerNames {
		foldedName := foldStudioCommandHeaderName(name)
		if reason := describeStudioCommandHeaderNameProblem(name); reason != "" {
			problems = append(problems, prefix+"fixed header "+strconv.QuoteToASCII(name)+" "+reason)
		}
		if seenFoldedNames[foldedName] {
			problems = append(problems, prefix+"fixed header names must be unique ignoring case and treating _ as -")
		}
		seenFoldedNames[foldedName] = true
		if request.Credential.Scheme == "header" && foldedName == foldStudioCommandHeaderName(request.Credential.Header) {
			problems = append(problems, prefix+"fixed headers cannot repeat the credential header")
		}
		value := request.FixedHeaders[name]
		if len(value) > maximumStudioCommandFixedHeaderValueLength || !studioCommandHeaderValuePattern.MatchString(value) {
			problems = append(problems, fmt.Sprintf("%sfixed header %s value must be 1-%d printable ASCII characters without surrounding spaces", prefix, strconv.QuoteToASCII(name), maximumStudioCommandFixedHeaderValueLength))
		}
	}
	return problems
}

// describeStudioCommandHeaderNameProblem returns why a manifest cannot declare a
// header name, or an empty string when the name is allowed.
func describeStudioCommandHeaderNameProblem(name string) string {
	foldedName := foldStudioCommandHeaderName(name)
	switch {
	case !httpHeaderTokenPattern.MatchString(name):
		return "must be an RFC 7230 token"
	case foldedName == "authorization":
		return "cannot be Authorization; use the bearer credential scheme"
	case studioCommandReservedHeaders[foldedName], strings.HasPrefix(foldedName, "proxy-"), strings.HasPrefix(foldedName, "x-forwarded-"):
		return "is reserved for Dex Web, the transport, or intermediaries"
	}
	return ""
}

// foldStudioCommandHeaderName lowercases a name and treats "_" as "-", because
// CGI-style servers merge them.
func foldStudioCommandHeaderName(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

func isValidJSONPath(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !fieldNamePattern.MatchString(part) {
			return false
		}
	}
	return true
}

func validateAuthMethod(prefix string, method AuthMethod) []string {
	var problems []string
	if method.Type != "none" && method.Type != "apiKey" && method.Type != "oauth2" && method.Type != "serviceAccount" {
		problems = append(problems, prefix+" type must be none, apiKey, oauth2, or serviceAccount")
	}
	problems = append(problems, validateFields(prefix, method.Fields, true)...)
	if method.Type == "none" && len(method.Fields) != 0 {
		problems = append(problems, prefix+" none auth cannot declare credential fields")
	}
	if method.Type != "none" && len(method.Fields) == 0 {
		problems = append(problems, prefix+" credential auth requires at least one field")
	}
	if method.Type != "none" && method.ConnectionKind == "" {
		problems = append(problems, prefix+" credential auth requires connectionKind")
	}
	if method.Type != "none" {
		if method.Guide == nil || !isHTTPSURL(method.Guide.StartURL) || len(method.Guide.Steps) == 0 {
			problems = append(problems, prefix+" credential auth requires an HTTPS authorization guide with steps")
		} else {
			for _, step := range method.Guide.Steps {
				if strings.TrimSpace(step) == "" {
					problems = append(problems, prefix+" authorization guide steps must be non-empty")
				}
			}
		}
	} else if method.Guide != nil {
		problems = append(problems, prefix+" authorization guide requires credential auth")
	}
	if method.Type != "oauth2" {
		if method.OAuth2 != nil {
			problems = append(problems, prefix+" oauth2 metadata requires oauth2 auth")
		}
		return problems
	}
	if method.OAuth2 == nil {
		return append(problems, prefix+" oauth2 auth requires oauth2 metadata")
	}
	oauth := method.OAuth2
	if method.ConnectionKind == "" || !isHTTPSURL(oauth.AuthorizationEndpoint) || !isHTTPSURL(oauth.TokenEndpoint) || len(oauth.Scopes) == 0 {
		return append(problems, prefix+" oauth2 auth requires connectionKind, endpoints, and scopes")
	}
	if oauth.Protocol != "oauth2" && oauth.Protocol != "oidc" {
		problems = append(problems, prefix+" oauth2 protocol must be oauth2 or oidc")
	}
	reservedAuthorizationParameters := map[string]bool{
		"client_id": true, "redirect_uri": true, "response_type": true, "scope": true,
		"state": true, "code_challenge": true, "code_challenge_method": true,
	}
	for name, value := range oauth.AuthorizationParameters {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" || reservedAuthorizationParameters[name] {
			problems = append(problems, prefix+" oauth2 authorizationParameters must be non-empty and cannot override protocol parameters")
		}
	}
	if oauth.Protocol == "oidc" {
		oidc := oauth.OIDC
		if oidc == nil || !isHTTPSURL(oidc.Issuer) || !isHTTPSURL(oidc.DiscoveryEndpoint) || !isHTTPSURL(oidc.UserInfoEndpoint) || !oidc.NonceRequired {
			problems = append(problems, prefix+" oidc auth requires HTTPS issuer, discovery, UserInfo, and nonce")
		}
	} else if oauth.OIDC != nil {
		problems = append(problems, prefix+" oidc metadata requires oidc protocol")
	}
	for scopeKind, scopes := range map[string][]string{"scopes": oauth.Scopes, "userScopes": oauth.UserScopes} {
		seenScopes := map[string]bool{}
		for _, scope := range scopes {
			if strings.TrimSpace(scope) == "" || seenScopes[scope] {
				problems = append(problems, prefix+" oauth2 "+scopeKind+" must be non-empty and unique")
			}
			seenScopes[scope] = true
		}
	}
	credentialFields := make(map[string]bool, len(method.Fields))
	credentialFieldTypes := make(map[string]string, len(method.Fields))
	for _, field := range method.Fields {
		credentialFields[field.Name] = true
		credentialFieldTypes[field.Name] = field.Type
	}
	if (oauth.ClientIDCredential == "") != (oauth.ClientSecretCredential == "") ||
		(oauth.ClientIDCredential != "" && (credentialFieldTypes[oauth.ClientIDCredential] == "" || credentialFieldTypes[oauth.ClientIDCredential] == "secretString" || credentialFieldTypes[oauth.ClientSecretCredential] != "secretString")) {
		problems = append(problems, prefix+" oauth2 client credential mappings require a non-secret client ID field and secret client secret field")
	}
	seenMappings := map[string]bool{}
	for _, mapping := range oauth.CredentialMappings {
		if !credentialFields[mapping.Credential] || seenMappings[mapping.Credential] || !isValidJSONPath(mapping.Source) {
			problems = append(problems, prefix+" oauth2 credential mappings require unique credential fields and dotted JSON response paths")
		}
		seenMappings[mapping.Credential] = true
	}
	for _, derivation := range oauth.CredentialDerivations {
		if !credentialFields[derivation.Credential] || seenMappings[derivation.Credential] || !isHTTPSURL(derivation.Endpoint) || !isValidJSONPath(derivation.Source) || (derivation.VerifiedBy != "" && !isValidJSONPath(derivation.VerifiedBy)) {
			problems = append(problems, prefix+" oauth2 credential derivations require unique credential fields, HTTPS endpoints, and dotted JSON response paths")
		}
		seenMappings[derivation.Credential] = true
	}
	return problems
}

func validateFields(prefix string, fields []Field, allowSecret bool) []string {
	var problems []string
	seen := map[string]bool{}
	seenGoNames := map[string]bool{}
	allowed := map[string]bool{
		"string": true, "url": true, "integer": true, "boolean": true, "duration": true,
		"stringList": true, "stringMap": true, "enum": true, "secretString": allowSecret,
	}
	for _, field := range fields {
		if !fieldNamePattern.MatchString(field.Name) || seen[field.Name] {
			problems = append(problems, prefix+" field names must be non-empty and unique")
		}
		seen[field.Name] = true
		if !goNamePattern.MatchString(field.GoName) || seenGoNames[field.GoName] || !allowed[field.Type] || strings.TrimSpace(field.Description) == "" {
			problems = append(problems, prefix+" fields require goName, supported type, and description")
		}
		seenGoNames[field.GoName] = true
		if field.Type == "enum" && len(field.Enum) == 0 {
			problems = append(problems, prefix+" enum fields require values")
		}
		if field.Default != nil && !isValidDefault(field) {
			problems = append(problems, prefix+" field "+field.Name+" has an invalid default")
		}
	}
	return problems
}

func isValidDefault(field Field) bool {
	switch field.Type {
	case "string", "url", "enum":
		value, ok := field.Default.(string)
		if !ok {
			return false
		}
		if field.Type == "url" {
			return isAbsoluteURL(value)
		}
		if field.Type == "enum" {
			for _, allowed := range field.Enum {
				if value == allowed {
					return true
				}
			}
			return false
		}
		return true
	case "duration":
		value, ok := field.Default.(string)
		if !ok {
			return false
		}
		duration, err := time.ParseDuration(value)
		return err == nil && duration >= 0
	case "integer":
		_, ok := field.Default.(int)
		return ok
	case "boolean":
		_, ok := field.Default.(bool)
		return ok
	case "stringList":
		values, ok := field.Default.([]any)
		if !ok {
			return false
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	case "stringMap":
		values, ok := field.Default.(map[string]any)
		if !ok {
			return false
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isAbsoluteURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Hostname() != ""
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != ""
}

func isSafeStudioAssetPath(value string, suffix string) bool {
	cleaned := filepath.Clean(value)
	return value != "" && value == filepath.ToSlash(value) && cleaned == value &&
		!filepath.IsAbs(value) && value != "." && !strings.HasPrefix(value, "../") &&
		strings.HasSuffix(strings.ToLower(value), suffix)
}

func isSafeStudioIconPath(value string) bool {
	return isSafeStudioAssetPath(value, ".png") || isSafeStudioAssetPath(value, ".svg")
}

func hasValidUniqueStrings(values []string, pattern *regexp.Regexp) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if !pattern.MatchString(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}
