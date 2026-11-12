// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"

	"gopkg.in/yaml.v3"
)

// conformanceIrrelevantMetadataFields are display-only metadata keys that no conformance runner reads.
var conformanceIrrelevantMetadataFields = []string{"categories", "description", "displayName"}

// conformanceManifestChangesCommand prints which manifests changed between revisions outside display-only metadata.
func conformanceManifestChangesCommand(args []string, output io.Writer) error {
	if len(args) < 3 {
		return errors.New("usage: connectorctl conformance-manifest-changes <base-revision> <head-revision> <manifest ...>")
	}
	baseRevision, headRevision := args[0], args[1]
	for _, revision := range []string{baseRevision, headRevision} {
		if err := exec.Command("git", "rev-parse", "--quiet", "--verify", revision+"^{commit}").Run(); err != nil {
			return fmt.Errorf("resolve revision %s: %w", revision, err)
		}
	}
	changedManifests := make(map[string]bool, len(args)-2)
	for _, manifestPath := range args[2:] {
		baseContents, isPresentAtBase, err := gitFileAtRevision(baseRevision, manifestPath)
		if err != nil {
			return err
		}
		headContents, isPresentAtHead, err := gitFileAtRevision(headRevision, manifestPath)
		if err != nil {
			return err
		}
		if !isPresentAtBase || !isPresentAtHead {
			changedManifests[manifestPath] = true
			continue
		}
		isChanged, err := hasConformanceRelevantManifestChange(baseContents, headContents)
		if err != nil {
			return fmt.Errorf("compare %s between %s and %s: %w", manifestPath, baseRevision, headRevision, err)
		}
		changedManifests[manifestPath] = isChanged
	}
	return json.NewEncoder(output).Encode(changedManifests)
}

// hasConformanceRelevantManifestChange compares decoded YAML, so comments, formatting, and key order never count.
// An undecodable base counts as changed; an undecodable head is an error.
func hasConformanceRelevantManifestChange(baseContents, headContents []byte) (bool, error) {
	headDocuments, err := decodeConformanceRelevantManifest(headContents)
	if err != nil {
		return false, fmt.Errorf("decode head manifest: %w", err)
	}
	baseDocuments, err := decodeConformanceRelevantManifest(baseContents)
	if err != nil {
		return true, nil
	}
	return !reflect.DeepEqual(baseDocuments, headDocuments), nil
}

func decodeConformanceRelevantManifest(contents []byte) ([]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	documents := []any{}
	for {
		var document any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	if len(documents) == 0 {
		return documents, nil
	}
	if root, isMapping := documents[0].(map[string]any); isMapping {
		if metadata, isMetadataMapping := root["metadata"].(map[string]any); isMetadataMapping {
			for _, field := range conformanceIrrelevantMetadataFields {
				delete(metadata, field)
			}
		}
	}
	return documents, nil
}

// gitFileAtRevision reads a path at a verified revision and reports false when it is absent there.
func gitFileAtRevision(revision, path string) ([]byte, bool, error) {
	object := revision + ":" + path
	if exec.Command("git", "cat-file", "-e", object).Run() != nil {
		return nil, false, nil
	}
	contents, err := exec.Command("git", "show", object).Output()
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", object, err)
	}
	return contents, true, nil
}
