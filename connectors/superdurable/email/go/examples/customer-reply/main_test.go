// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/internal/mailtest"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("EMAIL_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("EMAIL_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("EMAIL_EXAMPLE_MISSING", "fallback"))
}

func TestConnectionOptionsLoadACustomRootCAOnlyWhenSet(t *testing.T) {
	t.Setenv(rootCAFileEnvironmentVariable, "")
	options, err := connectionOptions()
	require.NoError(t, err)
	require.Empty(t, options)

	path := filepath.Join(t.TempDir(), "root-ca.pem")
	require.NoError(t, os.WriteFile(path, mailtest.NewCertificates(t).AuthorityPEM, 0o600))
	t.Setenv(rootCAFileEnvironmentVariable, path)
	options, err = connectionOptions()
	require.NoError(t, err)
	require.Len(t, options, 1)

	require.NoError(t, os.WriteFile(path, []byte("not a certificate"), 0o600))
	_, err = connectionOptions()
	require.ErrorContains(t, err, "holds no PEM certificate")
}
