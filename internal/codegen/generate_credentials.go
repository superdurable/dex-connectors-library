// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"slices"
	"strconv"

	"github.com/superdurable/dex-connectors-library/schema"
)

// HasHostKeptCredentials reports whether a host keeps some credential field of manifest from applications, so the
// generated credential type decodes shared credentials with UnmarshalSharedCredential.
func HasHostKeptCredentials(manifest schema.Manifest) bool {
	for _, method := range manifest.Spec.Auth.AuthMethods() {
		if len(schema.HostKeptCredentials(method)) > 0 {
			return true
		}
	}
	return false
}

// writeCredentialDecoding writes decodeCredentials, which requires every field. When a host keeps credentials, it
// also writes UnmarshalSharedCredential, and both decode with decodeCredentialFields.
func writeCredentialDecoding(generation *generator, manifest schema.Manifest) {
	if !HasHostKeptCredentials(manifest) {
		writeCredentialFieldDecoding(generation, manifest, "decodeCredentials", "credentials, credentials.Validate()")
		return
	}
	generation.mustWrite("func decodeCredentials(contents json.RawMessage) (Credentials, error) {\n")
	generation.mustWrite("\tcredentials, err := decodeCredentialFields(contents)\n")
	generation.mustWrite("\tif err != nil { return Credentials{}, err }\n")
	generation.mustWrite("\treturn credentials, credentials.Validate()\n}\n\n")
	generation.mustWrite("// UnmarshalSharedCredential decodes the credential of a shared connection, which the credential broker serves\n")
	generation.mustWrite("// without the fields that its host keeps to refresh or mint it. projectconfig decodes shared entries with it.\n")
	generation.mustWrite("func (credentials *Credentials) UnmarshalSharedCredential(contents []byte) error {\n")
	generation.mustWrite("\tdecoded, err := decodeCredentialFields(contents)\n")
	generation.mustWrite("\tif err != nil { return err }\n")
	generation.mustWrite("\tif err := decoded.validateSharedCredential(); err != nil { return err }\n")
	generation.mustWrite("\t*credentials = decoded\n\treturn nil\n}\n\n")
	writeCredentialFieldDecoding(generation, manifest, "decodeCredentialFields", "credentials, nil")
}

// writeCredentialFieldDecoding writes name, which strictly decodes every credential field and returns results.
func writeCredentialFieldDecoding(generation *generator, manifest schema.Manifest, name, results string) {
	generation.mustWrite("func %s(contents json.RawMessage) (Credentials, error) {\n", name)
	generation.mustWrite("\tvar fields struct {\n")
	if selection := credentialSelection(manifest.Spec.Auth); selection != nil {
		generation.mustWrite("\t\t%s %s `json:\"%s\"`\n", selection.goName, selection.goType, selection.wireName)
	}
	for _, field := range manifest.Spec.Auth.Fields {
		fieldType := goType(field)
		if field.Type == "secretString" {
			fieldType = "string"
		}
		generation.mustWrite("\t\t%s %s `json:\"%s\"`\n", field.GoName, fieldType, field.Name)
	}
	generation.mustWrite("\t}\n")
	generation.mustWrite("\tif err := projectconfig.DecodeCredentials(contents, &fields); err != nil { return Credentials{}, err }\n")
	if selection := credentialSelection(manifest.Spec.Auth); selection != nil {
		generation.mustWrite("\t// Credentials saved before the connector declared auth methods carry no %s; they use the default method.\n", selection.wireName)
		generation.mustWrite("\tif fields.%s == \"\" { fields.%s = %s }\n", selection.goName, selection.goName, strconv.Quote(manifest.Spec.Auth.DefaultMethod))
	}
	generation.mustWrite("\tcredentials := Credentials{\n")
	if selection := credentialSelection(manifest.Spec.Auth); selection != nil {
		generation.mustWrite("\t\t%s: fields.%s,\n", selection.goName, selection.goName)
	}
	for _, field := range manifest.Spec.Auth.Fields {
		value := "fields." + field.GoName
		if field.Type == "secretString" {
			value = "sdkgo.NewSecretString(" + value + ")"
		}
		generation.mustWrite("\t\t%s: %s,\n", field.GoName, value)
	}
	generation.mustWrite("\t}\n")
	generation.mustWrite("\treturn %s\n}\n\n", results)
}

// writeCredentialValidation writes Validate and, when a host keeps credentials, validateSharedCredential.
func writeCredentialValidation(generation *generator, manifest schema.Manifest) {
	writeCredentialValidator(generation, manifest, "func (credentials Credentials) Validate() error {\n", func(_ schema.AuthMethod, field schema.Field) bool {
		return field.Required
	})
	if !HasHostKeptCredentials(manifest) {
		return
	}
	generation.mustWrite("// validateSharedCredential is Validate without the host-kept fields; it requires the access token a host mints.\n")
	writeCredentialValidator(generation, manifest, "func (credentials Credentials) validateSharedCredential() error {\n", isRequiredInSharedCredential)
}

// isRequiredInSharedCredential reports whether a shared credential of method must carry field: every required field
// except the host-kept ones, and the access token that a clientCredentials or jwtBearer grant mints, because an
// application of a shared connection cannot mint it.
func isRequiredInSharedCredential(method schema.AuthMethod, field schema.Field) bool {
	if slices.Contains(schema.HostKeptCredentials(method), field.Name) {
		return false
	}
	return field.Required || slices.Contains(mintedAccessTokenCredentials(method), field.Name)
}

// SharedRequiredCredentialFields returns the fields an application must receive from a shared connection broker.
func SharedRequiredCredentialFields(method schema.AuthMethod) []string {
	var required []string
	for _, field := range method.Fields {
		if isRequiredInSharedCredential(method, field) {
			required = append(required, field.Name)
		}
	}
	return required
}

func writeCredentialValidator(generation *generator, manifest schema.Manifest, signature string, isRequired func(schema.AuthMethod, schema.Field) bool) {
	generation.mustWrite("%s", signature)
	if len(manifest.Spec.Auth.Methods) == 0 {
		writeMethodValidation(generation, manifest.Spec.Auth.AuthMethods()[0], isRequired)
	} else {
		generation.mustWrite("\tswitch credentials.AuthMethodID {\n")
		for _, method := range manifest.Spec.Auth.Methods {
			generation.mustWrite("\tcase %s:\n", strconv.Quote(method.ID))
			writeMethodValidation(generation, method, isRequired)
		}
		generation.mustWrite("\tdefault:\n\t\treturn fmt.Errorf(%q)\n\t}\n", "credential auth_method is invalid")
	}
	generation.mustWrite("\treturn nil\n}\n\n")
}

func writeMethodValidation(generation *generator, method schema.AuthMethod, isRequired func(schema.AuthMethod, schema.Field) bool) {
	for _, field := range method.Fields {
		field.Required = isRequired(method, field)
		writeFieldValidation(generation, "credentials."+field.GoName, field, "credential")
	}
}

// mintedAccessTokenCredentials are the fields that a clientCredentials or jwtBearer grant of method fills from the
// token response's access_token.
func mintedAccessTokenCredentials(method schema.AuthMethod) []string {
	var mappings []schema.OAuthCredentialMapping
	if method.ClientCredentials != nil {
		mappings = append(mappings, method.ClientCredentials.CredentialMappings...)
	}
	if method.ServiceAccount != nil && method.ServiceAccount.JWTBearer != nil {
		mappings = append(mappings, method.ServiceAccount.JWTBearer.CredentialMappings...)
	}
	var fields []string
	for _, mapping := range mappings {
		if mapping.Source == "access_token" {
			fields = append(fields, mapping.Credential)
		}
	}
	return fields
}
