// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplicationEnvironmentPinsSecretsAndReconcilesUnknownWrites(t *testing.T) {
	ctx := context.Background()
	objects := newMemoryObjects()
	uncertain := &faultObjects{ObjectStore: objects, isAccepted: true, failWrite: func(string, []byte) bool { return true }}
	store, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: uncertain, Scope: testScope})
	require.NoError(t, err)
	secret, err := store.CreateApplicationSecret(ctx, "SIGNING_SECRET", strings.Repeat("private-one-", 4))
	require.NoError(t, err)
	mode := "production"
	config, object, err := store.WriteConfiguration(ctx, 0, Configuration{Environment: map[string]EnvironmentValue{"APP_ENV": {Value: &mode}, "SIGNING_SECRET": {SecretRef: &secret}}})
	require.NoError(t, err)
	require.NotContains(t, string(object.Contents), "private-one-")
	pinned, err := store.FreezeConfiguration(ctx, config.Revision)
	require.NoError(t, err)
	replacement, err := store.CreateApplicationSecret(ctx, "SIGNING_SECRET", strings.Repeat("private-two-", 4))
	require.NoError(t, err)
	config.Environment["SIGNING_SECRET"] = EnvironmentValue{SecretRef: &replacement}
	_, _, err = store.WriteConfiguration(ctx, 1, config)
	require.NoError(t, err)
	_, _, err = store.WriteConfiguration(ctx, 1, config)
	require.ErrorIs(t, err, ErrConflict)
	restarted, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: objects, Scope: testScope})
	require.NoError(t, err)
	accepted, err := restarted.ReadSnapshot(ctx, pinned)
	require.NoError(t, err)
	values, err := restarted.ResolveApplicationEnvironment(ctx, accepted)
	require.NoError(t, err)
	actual, found := values.Lookup("SIGNING_SECRET")
	require.True(t, found)
	require.Equal(t, strings.Repeat("private-one-", 4), actual)
	require.NotContains(t, fmt.Sprintf("%+v %#v", values, values), "private-one-")
	_, err = json.Marshal(values)
	require.Error(t, err)
	fields := []EnvironmentDeclaration{{Name: "APP_ENV", Required: true, Enum: []string{"production"}}, {Name: "SIGNING_SECRET", Secret: true, Required: true, MinLength: 32}}
	require.NoError(t, restarted.ValidateApplicationEnvironment(ctx, fields, accepted))
	fields[1].MinLength = 100
	require.Error(t, restarted.ValidateApplicationEnvironment(ctx, fields, accepted))
}

func TestApplicationEnvironmentRejectsScopeIdentityAndPartialApply(t *testing.T) {
	ctx := context.Background()
	objects := newMemoryObjects()
	store, err := NewConfigurationStore(&ConfigurationStoreConfig{Objects: objects, Scope: testScope})
	require.NoError(t, err)
	secret, err := store.CreateApplicationSecret(ctx, "SIGNING_SECRET", "private")
	require.NoError(t, err)
	config := Configuration{Scope: testScope, Environment: map[string]EnvironmentValue{"SIGNING_SECRET": {SecretRef: &secret}}}
	for _, mutation := range []func(*ApplicationSecretRef){func(r *ApplicationSecretRef) { r.Version = "" }, func(r *ApplicationSecretRef) { r.Version = "null" }, func(r *ApplicationSecretRef) { r.Key = strings.Replace(r.Key, "/live/", "/preview/session/", 1) }, func(r *ApplicationSecretRef) { r.Key = strings.Replace(r.Key, "SIGNING_SECRET", "OTHER_SECRET", 1) }, func(r *ApplicationSecretRef) { r.Digest = "sha256:" + strings.Repeat("0", 64) }} {
		modified := secret
		mutation(&modified)
		config.Environment["SIGNING_SECRET"] = EnvironmentValue{SecretRef: &modified}
		_, err = store.ResolveApplicationEnvironment(ctx, config)
		require.Error(t, err)
	}
	empty := ""
	config.Environment["SIGNING_SECRET"] = EnvironmentValue{Value: &empty, SecretRef: &secret}
	_, err = store.ResolveApplicationEnvironment(ctx, config)
	require.Error(t, err)
	// A valid earlier field is not installed when a later secret cannot resolve.
	config.Environment = map[string]EnvironmentValue{"APP_ENV": {Value: &empty}, "SIGNING_SECRET": {SecretRef: &ApplicationSecretRef{Key: secret.Key, Version: "missing", Digest: secret.Digest}}}
	t.Setenv("APP_ENV", "unchanged")
	values, err := store.ResolveApplicationEnvironment(ctx, config)
	require.Error(t, err)
	_, found := values.Lookup("APP_ENV")
	require.False(t, found)
	require.Equal(t, "unchanged", os.Getenv("APP_ENV"))
}

func TestApplicationEnvironmentDeclarationsCanonicalAndReserved(t *testing.T) {
	fields := []EnvironmentDeclaration{{Name: "Z", Enum: []string{"b", "a"}}, {Name: "A", Secret: true, MinLength: 2}}
	canonical, err := CanonicalEnvironmentDeclarations(fields)
	require.NoError(t, err)
	data, err := json.Marshal(canonical)
	require.NoError(t, err)
	require.JSONEq(t, `[{"enum":[],"minLength":2,"name":"A","required":false,"secret":true},{"enum":["a","b"],"minLength":0,"name":"Z","required":false,"secret":false}]`, string(data))
	require.Equal(t, "b", fields[0].Enum[0])
	for _, name := range []string{"DEX_SERVER_ADDRESS", "AWS_REGION", "PATH", "HOME", "GOFLAGS", "GIT_CONFIG", "LD_PRELOAD", "NODE_OPTIONS", "SSL_CERT_FILE", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "PUBLIC_BASE_URL", "lowercase"} {
		require.False(t, IsApplicationEnvironmentName(name), name)
	}
	require.NoError(t, ValidateEnvironmentValue(EnvironmentDeclaration{Name: "TEXT", MinLength: 2}, "你好"))
	require.Error(t, ValidateEnvironmentValue(EnvironmentDeclaration{Name: "TEXT", MinLength: 2}, "好"))
	_, err = ValidateAndSortEnvironmentDeclarations([]EnvironmentDeclaration{{Name: "TOKEN", Secret: true, Enum: []string{"private"}}})
	require.Error(t, err)
	_, err = ValidateAndSortEnvironmentDeclarations([]EnvironmentDeclaration{{Name: "A"}, {Name: "A"}})
	require.Error(t, err)
}
