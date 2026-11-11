// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"net/http"
	"time"
)

// LoadProjectForTest loads a project from the given object store like LoadFromEnvironment does, with a test clock
// and, when httpClient is not nil, a test HTTP client for the credential broker.
func LoadProjectForTest(ctx context.Context, objects ObjectStore, scope Scope, reference SnapshotRef, httpClient *http.Client, now func() time.Time) (*LoadedProject, error) {
	return loadProject(ctx, objects, scope, reference, brokerClientOptions{httpClient: httpClient, now: now})
}

// ReplaceDexWebRenameForTest makes the local connections file store of project replace files with rename.
func ReplaceDexWebRenameForTest(project *LoadedProject, rename func(oldPath, newPath string) error) {
	project.objects.(*dexWebStore).renameFile = rename
}
