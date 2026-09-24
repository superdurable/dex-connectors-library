// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestGenerateIsDeterministicAndIncludesTypedOAuthCredentials(t *testing.T) {
	file, err := os.Open("../../schema/testdata/google-oauth.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	first, err := codegen.Generate(manifest)
	require.NoError(t, err)
	second, err := codegen.Generate(manifest)
	require.NoError(t, err)
	require.True(t, bytes.Equal(first, second))
	text := string(first)
	require.Contains(t, text, "type Config struct")
	require.Contains(t, text, "type Credentials struct")
	require.Contains(t, text, "AccessToken")
	require.Contains(t, text, "connector.SecretString")
	require.Contains(t, text, "AppendRowsBranchUncertain")
	require.Contains(t, text, "type AppendRowsStepConfig[IN any] struct")
	require.Contains(t, text, "func NewAppendRowsStep[IN any]")
	require.Contains(t, text, "connector.MutationFactoryConfigMarker")
	require.Contains(t, text, "`connector:\"connectorId=google-sheets-fixture\"`")
	require.Contains(t, text, "`connector:\"operationId=appendRows\"`")
	require.Contains(t, text, "type Environment string")
	require.Contains(t, text, `EnvironmentProduction Environment = "production"`)
	require.Contains(t, text, `[]string{"scope-b", "scope-a"}`)
	require.Contains(t, text, `map[string]string{"owner": "platform", "team": "onboarding"}`)
	require.NotContains(t, text, "access token default")
	require.NotContains(t, text, "ConnectorVersion")
	_, err = parser.ParseFile(token.NewFileSet(), "zz_generated_connector.go", strings.NewReader(text), parser.AllErrors)
	require.NoError(t, err)
}
