// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
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
	var timeout time.Duration
	var maxResponseBytes int
	for _, field := range manifest.Spec.Configuration.Fields {
		switch field.Name {
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
	values := struct {
		GoPackage, ConnectorLiteral, EndpointLiteral, ProviderLabel, Directory, KeyPrefixLiteral, ListTag, ModelTag string
		TimeoutNanos                                                                                                int64
		MaxResponseBytes                                                                                            int
	}{manifest.Spec.Codegen.Go.Package, literal(manifest.Metadata.Name), literal(protocol.Endpoint), protocol.ProviderLabel, strings.TrimPrefix(directory, "connectors/"), keyPrefix, listTag, modelTag, timeout.Nanoseconds(), maxResponseBytes}
	var output bytes.Buffer
	if err := modelCatalogTemplates.ExecuteTemplate(&output, templateName, values); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
