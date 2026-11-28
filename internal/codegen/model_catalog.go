// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/superdurable/dex-connectors-library/schema"
)

//go:embed templates/modelcatalog/*.tmpl
var modelCatalogSources embed.FS

var modelCatalogTemplates = template.Must(template.New("catalog").Delims("[[", "]]").ParseFS(modelCatalogSources, "templates/modelcatalog/*.tmpl"))

// GenerateModelCatalogClient renders a native provider client for the explicitly selected model-list protocol.
func GenerateModelCatalogClient(manifest schema.Manifest, directory, language string) ([]byte, error) {
	protocol := manifest.Spec.Codegen.ModelCatalog
	var baseURL string
	var timeout time.Duration
	var maxResponseBytes int
	for _, field := range manifest.Spec.Configuration.Fields {
		switch field.Name {
		case "baseUrl":
			baseURL = strings.TrimRight(fmt.Sprint(field.Default), "/")
			if !strings.HasPrefix(protocol.Endpoint, baseURL+"/") || !strings.HasPrefix(baseURL, "https://") {
				return nil, fmt.Errorf("model catalog endpoint must be below its default baseUrl")
			}
		case "timeout":
			value, err := time.ParseDuration(fmt.Sprint(field.Default))
			if err != nil {
				return nil, fmt.Errorf("model catalog timeout: %w", err)
			}
			timeout = value
		case "maxResponseBytes":
			maxResponseBytes, _ = field.Default.(int)
		}
	}
	if timeout <= 0 || maxResponseBytes <= 0 || maxResponseBytes > 16<<20 {
		return nil, fmt.Errorf("model catalog requires a positive timeout and response limit of at most 16 MiB")
	}
	literal := rustLiteral
	templateName := "model_catalog.rs.tmpl"
	if language == "java" {
		templateName = "ModelCatalog.java.tmpl"
		literal = func(value string) string {
			encoded, _ := json.Marshal(value) // Every Go string has a JSON encoding.
			return string(encoded)
		}
	} else if language != "rust" {
		return nil, fmt.Errorf("unsupported model catalog language %q", language)
	}
	keyPrefix := ""
	if protocol.KeyPrefix != "" {
		keyPrefix = literal(protocol.KeyPrefix)
	}
	listTag, modelTag := "list", "model"
	switch protocol.ObjectTags {
	case "uppercase":
		listTag, modelTag = "LIST", "MODEL"
	case "absent":
		listTag, modelTag = "", ""
	}
	maxKeyBytes := protocol.MaxKeyBytes
	if maxKeyBytes == 0 {
		maxKeyBytes = 1024
	}
	if len(manifest.Spec.Auth.Fields) != 1 {
		return nil, fmt.Errorf("model catalog requires one bearer credential")
	}
	credentialField := manifest.Spec.Auth.Fields[0].Name
	if credentialField == "apiKey" {
		credentialField = "api_key"
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(credentialField) {
		return nil, fmt.Errorf("model catalog credential must have a snake_case field name")
	}
	successBranch, outputType := "", ""
	for _, operation := range manifest.Spec.Operations {
		if operation.Name != "listModels" {
			continue
		}
		outputType = operation.OutputType
		if !token.IsIdentifier(outputType) || !token.IsExported(outputType) {
			return nil, fmt.Errorf("model catalog requires a named public output type")
		}
		for _, branch := range operation.Branches {
			if !branch.Optional {
				if successBranch != "" {
					return nil, fmt.Errorf("model catalog requires exactly one listModels happy-path branch")
				}
				successBranch = branch.ID
			}
		}
	}
	if successBranch == "" {
		return nil, fmt.Errorf("model catalog requires a listModels happy-path branch")
	}
	values := struct {
		BaseURLLiteral       string
		PathLiteral          string
		OutputType           string
		CredentialField      string
		MaxKeyBytes          int
		ValidationStatuses   []int
		ReadRejectedBody     bool
		JSONContentType      bool
		SuccessBranchLiteral string
		GoPackage            string
		ConnectorLiteral     string
		EndpointLiteral      string
		ProviderLabel        string
		Directory            string
		KeyPrefixLiteral     string
		ListTag              string
		ModelTag             string
		ConnectorResponse    bool
		TimeoutNanos         int64
		MaxResponseBytes     int
	}{
		OutputType:           outputType,
		CredentialField:      credentialField,
		MaxKeyBytes:          maxKeyBytes,
		ValidationStatuses:   protocol.ValidationStatuses,
		ReadRejectedBody:     protocol.ReadRejectedBody,
		JSONContentType:      protocol.JSONContentType,
		SuccessBranchLiteral: strconv.Quote(successBranch),
		GoPackage:            manifest.Spec.Codegen.Go.Package,
		ConnectorLiteral:     literal(manifest.Metadata.Name),
		EndpointLiteral:      literal(protocol.Endpoint),
		ProviderLabel:        protocol.ProviderLabel,
		Directory:            strings.TrimPrefix(directory, "connectors/"),
		KeyPrefixLiteral:     keyPrefix,
		ListTag:              listTag,
		ModelTag:             modelTag,
		ConnectorResponse:    protocol.ConnectorResponse,
		TimeoutNanos:         timeout.Nanoseconds(),
		MaxResponseBytes:     maxResponseBytes,
	}
	if baseURL != "" {
		values.BaseURLLiteral = literal(baseURL)
		values.PathLiteral = literal(strings.TrimPrefix(protocol.Endpoint, baseURL))
	}
	var output bytes.Buffer
	if err := modelCatalogTemplates.ExecuteTemplate(&output, templateName, values); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
