// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gomodel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

func TestCatalogOperationModelsLoad(t *testing.T) {
	repository := filepath.Join("..", "..", "..")
	contents, err := os.ReadFile(filepath.Join(repository, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Directories []string `yaml:"directories"`
	}
	if err := yaml.Unmarshal(contents, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, directory := range catalog.Directories {
		t.Run(directory, func(t *testing.T) {
			t.Parallel()
			connectorDirectory := filepath.Join(repository, directory)
			manifestFile, err := os.Open(filepath.Join(connectorDirectory, "connector.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			defer manifestFile.Close()
			manifest, err := schema.Decode(manifestFile)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, operation := range manifest.Spec.Operations {
				names = append(names, operation.InputType, operation.OutputType)
			}
			if _, err := Load(filepath.Join(connectorDirectory, "go"), names); err != nil {
				t.Fatal(err)
			}
		})
	}
}
