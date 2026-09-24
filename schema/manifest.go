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
}

type Spec struct {
	Provider      string        `yaml:"provider" json:"provider"`
	Codegen       Codegen       `yaml:"codegen" json:"codegen"`
	Configuration Configuration `yaml:"configuration" json:"configuration"`
	Auth          Auth          `yaml:"auth" json:"auth"`
	Studio        *Studio       `yaml:"studio,omitempty" json:"studio,omitempty"`
	Operations    []Operation   `yaml:"operations" json:"operations"`
}

type Studio struct {
	Setup StudioSetup `yaml:"setup" json:"setup"`
}

type StudioSetup struct {
	Entrypoint          string   `yaml:"entrypoint" json:"entrypoint"`
	HostAPIRange        string   `yaml:"hostApiRange" json:"hostApiRange"`
	BackendCapabilities []string `yaml:"backendCapabilities" json:"backendCapabilities"`
	MockScenarios       []string `yaml:"mockScenarios" json:"mockScenarios"`
	Icon                string   `yaml:"icon" json:"icon"`
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
	Type           string  `yaml:"type" json:"type"`
	ConnectionKind string  `yaml:"connectionKind" json:"connectionKind"`
	Fields         []Field `yaml:"fields" json:"fields"`
	OAuth2         *OAuth2 `yaml:"oauth2,omitempty" json:"oauth2,omitempty"`
}

type OAuth2 struct {
	AuthorizationEndpoint string   `yaml:"authorizationEndpoint" json:"authorizationEndpoint"`
	TokenEndpoint         string   `yaml:"tokenEndpoint" json:"tokenEndpoint"`
	Scopes                []string `yaml:"scopes" json:"scopes"`
	PKCE                  bool     `yaml:"pkce" json:"pkce"`
	Protocol              string   `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	OIDC                  *OIDC    `yaml:"oidc,omitempty" json:"oidc,omitempty"`
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
	Name            string            `yaml:"name" json:"name"`
	GoName          string            `yaml:"goName" json:"goName"`
	InputType       string            `yaml:"inputType" json:"inputType"`
	OutputType      string            `yaml:"outputType" json:"outputType"`
	Kind            string            `yaml:"kind" json:"kind"`
	Description     string            `yaml:"description" json:"description"`
	Idempotency     string            `yaml:"idempotency" json:"idempotency"`
	Branches        []OperationBranch `yaml:"branches" json:"branches"`
	DefectBranch    string            `yaml:"defectBranch" json:"defectBranch"`
	UncertainBranch string            `yaml:"uncertainBranch,omitempty" json:"uncertainBranch,omitempty"`
	ResultAttribute string            `yaml:"resultAttribute" json:"resultAttribute"`
	Progress        []string          `yaml:"progress,omitempty" json:"progress,omitempty"`
	Authorization   string            `yaml:"authorization,omitempty" json:"authorization,omitempty"`
	Execution       Execution         `yaml:"execution" json:"execution"`
}

type OperationBranch struct {
	ID          string `yaml:"id" json:"id"`
	GoName      string `yaml:"goName" json:"goName"`
	Description string `yaml:"description" json:"description"`
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
	goNamePattern       = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	fieldNamePattern    = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)
	capabilityPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*)+$`)
	mockScenarioPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

func Decode(reader io.Reader) (Manifest, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	manifest.normalize()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	for index := range manifest.Spec.Operations {
		sort.Strings(manifest.Spec.Operations[index].Progress)
	}
	return manifest, nil
}

func (manifest *Manifest) normalize() {
	if manifest.Spec.Auth.OAuth2 != nil && manifest.Spec.Auth.OAuth2.Protocol == "" {
		manifest.Spec.Auth.OAuth2.Protocol = "oauth2"
	}
	for index := range manifest.Spec.Operations {
		if manifest.Spec.Operations[index].Authorization != "" {
			continue
		}
		if manifest.Spec.Auth.Type == "none" {
			manifest.Spec.Operations[index].Authorization = "none"
		} else {
			manifest.Spec.Operations[index].Authorization = "required"
		}
	}
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
	if strings.TrimSpace(manifest.Spec.Provider) == "" {
		problems = append(problems, "spec.provider is required")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(manifest.Spec.Codegen.Go.Package) {
		problems = append(problems, "spec.codegen.go.package must be a Go package name")
	}
	problems = append(problems, validateFields("configuration", manifest.Spec.Configuration.Fields, false)...)
	if manifest.Spec.Auth.Type != "none" && manifest.Spec.Auth.Type != "apiKey" && manifest.Spec.Auth.Type != "oauth2" {
		problems = append(problems, "spec.auth.type must be none, apiKey, or oauth2")
	}
	problems = append(problems, validateFields("auth", manifest.Spec.Auth.Fields, true)...)
	if manifest.Spec.Auth.Type == "none" && len(manifest.Spec.Auth.Fields) != 0 {
		problems = append(problems, "none auth cannot declare credential fields")
	}
	if manifest.Spec.Auth.Type != "none" && len(manifest.Spec.Auth.Fields) == 0 {
		problems = append(problems, "credential auth requires at least one field")
	}
	if manifest.Spec.Auth.Type != "none" && manifest.Spec.Auth.ConnectionKind == "" {
		problems = append(problems, "credential auth requires connectionKind")
	}
	if manifest.Spec.Auth.Type == "oauth2" {
		if manifest.Spec.Auth.OAuth2 == nil {
			problems = append(problems, "oauth2 auth requires oauth2 metadata")
		} else if manifest.Spec.Auth.ConnectionKind == "" || !httpsURL(manifest.Spec.Auth.OAuth2.AuthorizationEndpoint) || !httpsURL(manifest.Spec.Auth.OAuth2.TokenEndpoint) || len(manifest.Spec.Auth.OAuth2.Scopes) == 0 {
			problems = append(problems, "oauth2 auth requires connectionKind, endpoints, and scopes")
		} else {
			if manifest.Spec.Auth.OAuth2.Protocol != "oauth2" && manifest.Spec.Auth.OAuth2.Protocol != "oidc" {
				problems = append(problems, "oauth2 protocol must be oauth2 or oidc")
			}
			if manifest.Spec.Auth.OAuth2.Protocol == "oidc" {
				oidc := manifest.Spec.Auth.OAuth2.OIDC
				if oidc == nil || !httpsURL(oidc.Issuer) || !httpsURL(oidc.DiscoveryEndpoint) || !httpsURL(oidc.UserInfoEndpoint) || !oidc.NonceRequired {
					problems = append(problems, "oidc auth requires HTTPS issuer, discovery, UserInfo, and nonce")
				}
			} else if manifest.Spec.Auth.OAuth2.OIDC != nil {
				problems = append(problems, "oidc metadata requires oidc protocol")
			}
			seenScopes := map[string]bool{}
			for _, scope := range manifest.Spec.Auth.OAuth2.Scopes {
				if strings.TrimSpace(scope) == "" || seenScopes[scope] {
					problems = append(problems, "oauth2 scopes must be non-empty and unique")
				}
				seenScopes[scope] = true
			}
		}
	} else if manifest.Spec.Auth.OAuth2 != nil {
		problems = append(problems, "oauth2 metadata requires oauth2 auth")
	}
	if manifest.Spec.Studio != nil {
		setup := manifest.Spec.Studio.Setup
		if !safeStudioAssetPath(setup.Entrypoint, ".html") {
			problems = append(problems, "studio setup entrypoint must be a safe relative .html path")
		}
		if !safeStudioIconPath(setup.Icon) {
			problems = append(problems, "studio setup icon must be a safe relative .png or .svg path")
		}
		if strings.TrimSpace(setup.HostAPIRange) == "" {
			problems = append(problems, "studio setup hostApiRange is required")
		}
		if len(setup.BackendCapabilities) == 0 || !validUniqueStrings(setup.BackendCapabilities, capabilityPattern) {
			problems = append(problems, "studio setup backendCapabilities must be non-empty, unique capability IDs")
		}
		if len(setup.MockScenarios) == 0 || !validUniqueStrings(setup.MockScenarios, mockScenarioPattern) {
			problems = append(problems, "studio setup mockScenarios must be non-empty, unique scenario IDs")
		}
	}
	if len(manifest.Spec.Operations) == 0 {
		problems = append(problems, "spec.operations must contain at least one operation")
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
		if len(branches) == 0 || !branches[operation.DefectBranch] {
			problems = append(problems, operation.Name+": defectBranch must name a declared branch")
		}
		if operation.Kind == "mutation" && !branches[operation.UncertainBranch] {
			problems = append(problems, operation.Name+": uncertainBranch must name a declared branch")
		}
		if operation.Kind == "query" && operation.UncertainBranch != "" {
			problems = append(problems, operation.Name+": query cannot declare uncertainBranch")
		}
		if operation.ResultAttribute != "none" && operation.ResultAttribute != "optional" && operation.ResultAttribute != "required" {
			problems = append(problems, operation.Name+": resultAttribute must be none, optional, or required")
		}
		if operation.Authorization != "none" && operation.Authorization != "required" {
			problems = append(problems, operation.Name+": authorization must be none or required")
		}
		if operation.Authorization == "required" && manifest.Spec.Auth.Type == "none" {
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
		if field.Default != nil && !validDefault(field) {
			problems = append(problems, prefix+" field "+field.Name+" has an invalid default")
		}
	}
	return problems
}

func validDefault(field Field) bool {
	switch field.Type {
	case "string", "url", "enum":
		value, ok := field.Default.(string)
		if !ok {
			return false
		}
		if field.Type == "url" {
			return absoluteURL(value)
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

func absoluteURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Hostname() != ""
}

func httpsURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != ""
}

func safeStudioAssetPath(value string, suffix string) bool {
	cleaned := filepath.Clean(value)
	return value != "" && value == filepath.ToSlash(value) && cleaned == value &&
		!filepath.IsAbs(value) && value != "." && !strings.HasPrefix(value, "../") &&
		strings.HasSuffix(strings.ToLower(value), suffix)
}

func safeStudioIconPath(value string) bool {
	return safeStudioAssetPath(value, ".png") || safeStudioAssetPath(value, ".svg")
}

func validUniqueStrings(values []string, pattern *regexp.Regexp) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if !pattern.MatchString(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}
