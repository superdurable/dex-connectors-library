// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
)

// connectorGoPackage loads once per command run for all its model graphs; not concurrency-safe.
type connectorGoPackage struct {
	goDirectory string
	loaded      *gomodel.Package
}

func newConnectorGoPackage(manifestPath string) *connectorGoPackage {
	return &connectorGoPackage{goDirectory: filepath.Join(filepath.Dir(manifestPath), "go")}
}

// load returns the type-checked package; a failed load is retried by the next call.
func (goPackage *connectorGoPackage) load() (*gomodel.Package, error) {
	if goPackage.loaded == nil {
		loaded, err := gomodel.LoadPackage(goPackage.goDirectory)
		if err != nil {
			return nil, err
		}
		goPackage.loaded = loaded
	}
	return goPackage.loaded, nil
}

// graph builds an independent model graph for rootNames from the shared package.
func (goPackage *connectorGoPackage) graph(rootNames []string, publishedNames ...string) (gomodel.Graph, error) {
	loaded, err := goPackage.load()
	if err != nil {
		return gomodel.Graph{}, err
	}
	return loaded.Graph(rootNames, publishedNames...)
}
