// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package s3_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go/internal/s3fake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestFolderLifecycleDeletesOnlyTheSelectedPrefix(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/2026/week.md", s3fake.Object{Body: []byte("week")})
	store.SetObject(testBucket, "reports/2025/old.md", s3fake.Object{Body: []byte("old")})
	client := newPathStyleClient(t, store)
	created, err := runMutation(t, newDexContext("folder-create"), client.CreateFolder(), s3.CreateFolderInput{Prefix: "reports/2026"})
	require.NoError(t, err)
	require.Equal(t, s3.CreateFolderBranchCreated, created.Branch)
	require.Equal(t, "reports/2026/", created.Value.Prefix)
	deleted, err := runMutation(t, newDexContext("folder-delete"), client.DeleteFolder(), s3.DeleteFolderInput{Prefix: "reports/2026"})
	require.NoError(t, err)
	require.Equal(t, s3.DeleteFolderBranchDeleted, deleted.Branch)
	_, exists := store.Object(testBucket, "reports/2026/week.md")
	require.False(t, exists)
	_, exists = store.Object(testBucket, "reports/2026/")
	require.False(t, exists)
	_, exists = store.Object(testBucket, "reports/2025/old.md")
	require.True(t, exists)
}

func TestFolderOperationsClassifyRejectionAndRetry(t *testing.T) {
	store := newFakeStore(t)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodPut {
			return &s3fake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}
		}
		return nil
	})
	client := newPathStyleClient(t, store)
	created, err := runMutation(t, newDexContext("folder-denied"), client.CreateFolder(), s3.CreateFolderInput{Prefix: "reports/2026"})
	require.NoError(t, err)
	require.Equal(t, s3.CreateFolderBranchProviderRejected, created.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, created.Failure.Kind)
	requireNoSecrets(t, created)
	store.Intercept(func(request s3fake.Request) *s3fake.Response {
		if request.Method == http.MethodGet {
			return &s3fake.Response{StatusCode: http.StatusServiceUnavailable}
		}
		return nil
	})
	_, err = runMutation(t, newDexContext("folder-retry"), client.DeleteFolder(), s3.DeleteFolderInput{Prefix: "reports/2026"})
	requireRetry(t, err, sdkgo.FailureAvailability)
}

func TestListFoldersReturnsOnlyImmediateChildPrefixes(t *testing.T) {
	store := newFakeStore(t)
	store.SetObject(testBucket, "reports/2026/week.md", s3fake.Object{Body: []byte("week")})
	store.SetObject(testBucket, "reports/2025/old.md", s3fake.Object{Body: []byte("old")})
	store.SetObject(testBucket, "reports/root.md", s3fake.Object{Body: []byte("root")})
	result, err := runQuery(t, "folders", newPathStyleClient(t, store).ListFolders(), s3.ListFoldersInput{ParentPrefix: "reports/"})
	require.NoError(t, err)
	require.Equal(t, s3.ListFoldersBranchListed, result.Branch)
	require.Equal(t, []string{"reports/2025/", "reports/2026/"}, result.Value.Folders)
}
