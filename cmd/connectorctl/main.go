// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/superdurable/dex-connectors-library/internal/codegen"
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
		return errors.New("usage: connectorctl <validate|catalog|generate> [path ...]")
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
	case "generate":
		return generate(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func generate(args []string) error {
	check := false
	if len(args) > 0 && args[0] == "--check" {
		check = true
		args = args[1:]
	}
	if len(args) != 1 {
		return errors.New("usage: connectorctl generate [--check] <manifest>")
	}
	manifest, err := load(args[0])
	if err != nil {
		return err
	}
	generated, err := codegen.Generate(manifest)
	if err != nil {
		return err
	}
	output := filepath.Join(filepath.Dir(args[0]), codegen.OutputFile)
	if check {
		current, readErr := os.ReadFile(output)
		if readErr != nil {
			return fmt.Errorf("generated connector is missing: %s", output)
		}
		if !bytes.Equal(current, generated) {
			return fmt.Errorf("generated connector is stale: %s", output)
		}
		return nil
	}
	if err := os.WriteFile(output, generated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	return nil
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
