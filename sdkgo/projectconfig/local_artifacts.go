// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// LocalConnectorArtifact records reviewed unpublished source, never a published release identity.
// It is accepted only by explicitly configured local storage deployments.
type LocalConnectorArtifact struct {
	BaselineVersion  string `json:"baselineVersion"`
	SourceCommit     string `json:"sourceCommit"`
	SourceTreeDigest string `json:"sourceTreeDigest"`
	ArtifactDigest   string `json:"artifactDigest"`
}

// LocalConnectorAuthority is immutable operator-owned image metadata, not browser input.
type LocalConnectorAuthority struct {
	ConnectorID string `json:"connectorId"`
	ModulePath  string `json:"modulePath"`
	Directory   string `json:"directory"`
	LocalConnectorArtifact
}

// ReadLocalConnectorAuthorities validates an explicit image-owned descriptor and its exact source pins.
// Callers must independently enforce local-only admission before calling it.
func ReadLocalConnectorAuthorities(filename string) (map[string]LocalConnectorAuthority, error) {
	if !filepath.IsAbs(filename) {
		return nil, errors.New("local connector authority must be an absolute image-owned path")
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("local connector authority file is unavailable or invalid")
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var document struct {
		SchemaVersion string                    `json:"schemaVersion"`
		Artifacts     []LocalConnectorAuthority `json:"artifacts"`
	}
	if strictJSON(contents, &document) != nil || document.SchemaVersion != "connectors.dex.dev/local-artifacts/v1" || len(document.Artifacts) == 0 || len(document.Artifacts) > 32 {
		return nil, errors.New("local connector authority descriptor is invalid")
	}
	result := make(map[string]LocalConnectorAuthority, len(document.Artifacts))
	for _, authority := range document.Artifacts {
		if !regexpConnectorID(authority.ConnectorID) || !regexp.MustCompile(`^github\.com/superdurable/dex-connectors-library/connectors/[a-z0-9][a-z0-9/-]*$`).MatchString(authority.ModulePath) || !filepath.IsAbs(authority.Directory) || !authority.LocalConnectorArtifact.Valid() {
			return nil, errors.New("local connector source authority is invalid")
		}
		if _, exists := result[authority.ConnectorID]; exists {
			return nil, errors.New("local connector authority is duplicated")
		}
		result[authority.ConnectorID] = authority
	}
	return result, nil
}

// Valid checks non-secret exact source and artifact identities.
func (pin LocalConnectorArtifact) Valid() bool {
	return regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(pin.BaselineVersion) && regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(pin.SourceCommit) && regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(pin.SourceTreeDigest) && regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(pin.ArtifactDigest)
}

func validateLocalConnectorConfiguration(configuration Configuration, allowLocal bool, filename string) error {
	authorities := map[string]LocalConnectorAuthority{}
	if filename != "" {
		if !allowLocal {
			return errors.New("local connector artifacts are forbidden for hosted storage")
		}
		var err error
		authorities, err = ReadLocalConnectorAuthorities(filename)
		if err != nil {
			return err
		}
	}
	for _, connection := range configuration.Connections {
		authority, local := authorities[connection.ConnectorID]
		if connection.LocalArtifact == nil {
			if local {
				return errors.New("configuration is missing its local connector source pin")
			}
			continue
		}
		if !allowLocal || !local || authority.ModulePath != connection.ModulePath || authority.BaselineVersion != connection.ModuleVersion || authority.LocalConnectorArtifact != *connection.LocalArtifact {
			return errors.New("configuration local connector source pin does not match this application image")
		}
	}
	return nil
}
