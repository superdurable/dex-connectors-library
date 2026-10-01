// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// EnvironmentDeclaration describes one application-owned string. MinLength counts Unicode code points; values cannot exceed 32768 UTF-8 bytes.
// Declarations contain no values or defaults. Secret fields have write-only browser input and exact versioned object references.
type EnvironmentDeclaration struct {
	// Name is a nonreserved uppercase environment name, at most 128 ASCII characters.
	Name string `json:"name"`
	// Required rejects missing values; empty strings still follow MinLength and Enum.
	Required bool `json:"required"`
	// Secret stores the value separately from the ordinary configuration document.
	Secret bool `json:"secret"`
	// MinLength is the minimum Unicode code point count, from zero through 32768.
	MinLength int `json:"minLength"`
	// Enum limits ordinary values when nonempty. Secret fields cannot publish allowed secret values.
	Enum []string `json:"enum"`
}

// ApplicationSecretRef pins one application secret in the same project scope. It never identifies a mutable head.
type ApplicationSecretRef struct {
	// Key is relative to the constructor-owned storage prefix and names app-secrets/<NAME>/<randomID>.
	Key string `json:"key"`
	// Version is the exact immutable object version.
	Version string `json:"version"`
	// Digest is sha256: followed by lowercase hexadecimal of the complete private object.
	Digest string `json:"digest"`
}

// EnvironmentValue contains exactly one ordinary value or an immutable secret reference.
type EnvironmentValue struct {
	// Value preserves an explicitly supplied empty ordinary value. Nil means no ordinary value.
	Value *string `json:"value,omitempty"`
	// SecretRef identifies private material; callers must never return the object or reference through a safe browser DTO.
	SecretRef *ApplicationSecretRef `json:"secretRef,omitempty"`
}

// ApplicationEnvironment contains resolved private startup values. It cannot be serialized or formatted with values.
// Keep it in application memory; never include it in Flow inputs, Attributes, Results, logs, or browser responses.
type ApplicationEnvironment struct{ values map[string]string }

// Lookup returns a declared startup value and whether it is present. The returned string may be secret.
func (environment ApplicationEnvironment) Lookup(name string) (string, bool) {
	value, found := environment.values[name]
	return value, found
}

// Apply sets only validated application-owned variables. Call once before reading app settings or creating application services, clients, or goroutines.
// It never sets platform, SDK, AWS, process-loader, or toolchain reserved variables.
func (environment ApplicationEnvironment) Apply() error {
	for name, value := range environment.values {
		if !IsApplicationEnvironmentName(name) || !validEnvironmentValue(value) {
			return errors.New("invalid application environment")
		}
	}
	for name, value := range environment.values {
		if err := os.Setenv(name, value); err != nil {
			return errors.New("application environment could not be applied")
		}
	}
	return nil
}

// String redacts all values.
func (environment ApplicationEnvironment) String() string {
	return fmt.Sprintf("projectconfig.ApplicationEnvironment{%d fields}", len(environment.values))
}

// GoString redacts all values in Go-syntax formatting.
func (environment ApplicationEnvironment) GoString() string { return environment.String() }

// MarshalJSON prevents transport of resolved secrets.
func (ApplicationEnvironment) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private application environment cannot be serialized")
}

// MarshalText prevents textual transport of resolved secrets.
func (ApplicationEnvironment) MarshalText() ([]byte, error) {
	return nil, errors.New("private application environment cannot be serialized")
}

// MarshalYAML prevents YAML transport of resolved secrets.
func (ApplicationEnvironment) MarshalYAML() (any, error) {
	return nil, errors.New("private application environment cannot be serialized")
}

// ValidateAndSortEnvironmentDeclarations validates declarations and returns independently sorted declarations and enums.
// Missing declarations are allowed. At most 128 unique names and 128 enum values per field are supported.
func ValidateAndSortEnvironmentDeclarations(declarations []EnvironmentDeclaration) ([]EnvironmentDeclaration, error) {
	if len(declarations) > 128 {
		return nil, errors.New("application environment exceeds field limit")
	}
	result := make([]EnvironmentDeclaration, len(declarations))
	names := map[string]bool{}
	for index, field := range declarations {
		if !IsApplicationEnvironmentName(field.Name) || names[field.Name] || field.MinLength < 0 || field.MinLength > 32768 || len(field.Enum) > 128 || (field.Secret && len(field.Enum) > 0) {
			return nil, errors.New("application environment declaration is invalid")
		}
		names[field.Name] = true
		field.Enum = append([]string{}, field.Enum...)
		sort.Strings(field.Enum)
		for optionIndex, value := range field.Enum {
			if !validEnvironmentValue(value) || utf8.RuneCountInString(value) < field.MinLength || (optionIndex > 0 && value == field.Enum[optionIndex-1]) {
				return nil, errors.New("application environment enum is invalid")
			}
		}
		result[index] = field
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// CanonicalEnvironmentDeclarations returns sorted plain maps for canonical JSON semantic digests shared with manifest readers.
// Every declaration includes enum, minLength, name, required and secret; empty enum is [] rather than null.
func CanonicalEnvironmentDeclarations(declarations []EnvironmentDeclaration) ([]map[string]any, error) {
	sorted, err := ValidateAndSortEnvironmentDeclarations(declarations)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(sorted))
	for _, field := range sorted {
		result = append(result, map[string]any{"name": field.Name, "required": field.Required, "secret": field.Secret, "minLength": field.MinLength, "enum": field.Enum})
	}
	return result, nil
}

// CreateApplicationSecret writes one immutable private object. Unknown writes reconcile the same random object key before returning.
// Successful candidates remain scope-owned even when a later configuration CAS loses; final scope deletion removes all versions.
// Callers validate declaration, manifest revision and value before writing; this method enforces storage and value bounds.
func (store *ConfigurationStore) CreateApplicationSecret(ctx context.Context, name, value string) (ApplicationSecretRef, error) {
	if !IsApplicationEnvironmentName(name) || !validEnvironmentValue(value) {
		return ApplicationSecretRef{}, errors.New("invalid application secret")
	}
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return ApplicationSecretRef{}, errors.New("application secret identity unavailable")
	}
	prefix, err := store.scope.Prefix()
	if err != nil {
		return ApplicationSecretRef{}, err
	}
	key := prefix + "/app-secrets/" + name + "/" + hex.EncodeToString(identifier)
	contents, err := json.Marshal(applicationSecret{Scope: store.scope, Name: name, Value: value})
	if err != nil {
		return ApplicationSecretRef{}, errors.New("application secret encoding failed")
	}
	object, err := store.objects.CreateObject(ctx, key, contents)
	if errors.Is(err, ErrOutcomeUnknown) || errors.Is(err, ErrConflict) {
		observed, readErr := store.objects.ReadObject(ctx, key, "")
		if readErr == nil && bytes.Equal(observed.Contents, contents) {
			object, err = observed, nil
		}
	}
	if err != nil {
		return ApplicationSecretRef{}, err
	}
	if object.Key != key || object.Version == "" || object.Version == "null" || !bytes.Equal(object.Contents, contents) {
		return ApplicationSecretRef{}, errors.New("application secret identity is invalid")
	}
	return ApplicationSecretRef{Key: key, Version: object.Version, Digest: "sha256:" + digestBytes(contents)}, nil
}

// ResolveApplicationEnvironment loads only the secret versions pinned by this accepted configuration, without refresh or writes.
// Live and Preview scopes cannot share references. Changes to current configuration or secret objects do not alter accepted versions.
func (store *ConfigurationStore) ResolveApplicationEnvironment(ctx context.Context, configuration Configuration) (ApplicationEnvironment, error) {
	if configuration.Scope != store.scope {
		return ApplicationEnvironment{}, errors.New("application environment scope differs")
	}
	if err := validateEnvironmentReferences(configuration); err != nil {
		return ApplicationEnvironment{}, err
	}
	values := make(map[string]string, len(configuration.Environment))
	for name, entry := range configuration.Environment {
		if entry.Value != nil {
			values[name] = *entry.Value
			continue
		}
		reference := entry.SecretRef
		object, err := store.objects.ReadObject(ctx, reference.Key, reference.Version)
		if err != nil {
			return ApplicationEnvironment{}, err
		}
		var secret applicationSecret
		if object.Key != reference.Key || object.Version != reference.Version || "sha256:"+digestBytes(object.Contents) != reference.Digest || strictJSON(object.Contents, &secret) != nil || secret.Scope != store.scope || secret.Name != name || !validEnvironmentValue(secret.Value) {
			return ApplicationEnvironment{}, errors.New("application secret integrity or scope differs")
		}
		values[name] = secret.Value
	}
	return ApplicationEnvironment{values: values}, nil
}

// ValidateApplicationEnvironment checks all configured fields against the exact source declarations and pinned private values.
// It rejects undeclared names, classification changes, missing required values, and string constraints without exposing values in errors.
func (store *ConfigurationStore) ValidateApplicationEnvironment(ctx context.Context, declarations []EnvironmentDeclaration, configuration Configuration) error {
	fields, err := ValidateAndSortEnvironmentDeclarations(declarations)
	if err != nil {
		return err
	}
	resolved, err := store.ResolveApplicationEnvironment(ctx, configuration)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(fields))
	for _, field := range fields {
		known[field.Name] = true
		entry, exists := configuration.Environment[field.Name]
		if !exists {
			if field.Required {
				return fmt.Errorf("application environment %s is required", field.Name)
			}
			continue
		}
		if (entry.SecretRef != nil) != field.Secret {
			return fmt.Errorf("application environment %s classification changed", field.Name)
		}
		value, _ := resolved.Lookup(field.Name)
		if err = ValidateEnvironmentValue(field, value); err != nil {
			return err
		}
	}
	for name := range configuration.Environment {
		if !known[name] {
			return errors.New("application environment contains undeclared fields")
		}
	}
	return nil
}

// ValidateEnvironmentValue checks one bounded string against a previously validated declaration. Errors contain the field name only.
func ValidateEnvironmentValue(field EnvironmentDeclaration, value string) error {
	if !validEnvironmentValue(value) || utf8.RuneCountInString(value) < field.MinLength {
		return fmt.Errorf("application environment %s has invalid length or encoding", field.Name)
	}
	if len(field.Enum) > 0 {
		for _, allowed := range field.Enum {
			if value == allowed {
				return nil
			}
		}
		return fmt.Errorf("application environment %s is not an allowed value", field.Name)
	}
	return nil
}

// IsApplicationEnvironmentName reports whether a name is safe for app-owned values, excluding deployment and process control variables.
func IsApplicationEnvironmentName(name string) bool {
	if !applicationEnvironmentName.MatchString(name) {
		return false
	}
	for _, prefix := range []string{"SUPERVERSE_", "DEX_", "AWS_", "LD_", "GO", "GIT_"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	switch name {
	case "PATH", "HOME", "PORT", "HOST", "NODE_OPTIONS", "SSL_CERT_FILE", "SSL_CERT_DIR", "PUBLIC_BASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
		return false
	}
	return true
}

var applicationEnvironmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
var applicationSecretID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var applicationSecretDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type applicationSecret struct {
	Scope Scope  `json:"scope"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func validEnvironmentValue(value string) bool {
	return len(value) <= 32768 && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}
func validateEnvironmentReferences(configuration Configuration) error {
	if len(configuration.Environment) > 128 {
		return errors.New("application environment exceeds field limit")
	}
	prefix, err := configuration.Scope.Prefix()
	if err != nil {
		return err
	}
	for name, entry := range configuration.Environment {
		if !IsApplicationEnvironmentName(name) || (entry.Value == nil) == (entry.SecretRef == nil) {
			return errors.New("application environment entry is invalid")
		}
		if entry.Value != nil {
			if !validEnvironmentValue(*entry.Value) {
				return errors.New("application environment value is invalid")
			}
			continue
		}
		reference := entry.SecretRef
		secretPrefix := prefix + "/app-secrets/" + name + "/"
		if !strings.HasPrefix(reference.Key, secretPrefix) || !applicationSecretID.MatchString(strings.TrimPrefix(reference.Key, secretPrefix)) || reference.Version == "" || reference.Version == "null" || strings.ContainsAny(reference.Version, "\x00\r\n") || !applicationSecretDigest.MatchString(reference.Digest) {
			return errors.New("application secret reference differs from scope")
		}
	}
	return nil
}
