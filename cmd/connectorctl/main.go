// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/superdurable/dex-connectors-library/schema"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: connectorctl <validate|catalog> [path ...]")
	}
	switch args[0] {
	case "validate":
		if len(args) < 2 {
			return errors.New("usage: connectorctl validate <manifest ...>")
		}
		for _, path := range args[1:] {
			manifest, err := load(path)
			if err != nil {
				return err
			}
			fmt.Printf("valid %s %s\n", manifest.Metadata.Name, manifest.Metadata.Version)
		}
		return nil
	case "catalog":
		root := "connectors"
		if len(args) > 1 {
			root = args[1]
		}
		manifests, err := catalog(root)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(manifests)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func load(path string) (schema.Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return schema.Manifest{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	manifest, err := schema.Decode(file)
	if err != nil {
		return schema.Manifest{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return manifest, nil
}

func catalog(root string) ([]schema.Manifest, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "connector.yaml" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan catalog: %w", err)
	}
	sort.Strings(paths)
	manifests := make([]schema.Manifest, 0, len(paths))
	for _, path := range paths {
		manifest, loadErr := load(path)
		if loadErr != nil {
			return nil, loadErr
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}
