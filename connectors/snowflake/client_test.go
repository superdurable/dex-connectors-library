// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestResolveAccountHostAcceptsOnlyDocumentedIdentifierForms(t *testing.T) {
	for identifier, want := range map[string][2]string{
		"myorg-analytics":                   {"myorg-analytics.snowflakecomputing.com", "MYORG-ANALYTICS"},
		"MyOrg-Analytics_Prod":              {"myorg-analytics_prod.snowflakecomputing.com", "MYORG-ANALYTICS_PROD"},
		"myorg-analytics.privatelink":       {"myorg-analytics.privatelink.snowflakecomputing.com", "MYORG-ANALYTICS"},
		"xy12345":                           {"xy12345.snowflakecomputing.com", "XY12345"},
		"xy12345.us-east-2.aws":             {"xy12345.us-east-2.aws.snowflakecomputing.com", "XY12345"},
		"xy12345.east-us-2.azure":           {"xy12345.east-us-2.azure.snowflakecomputing.com", "XY12345"},
		"xy12345.us-east-2.aws.privatelink": {"xy12345.us-east-2.aws.privatelink.snowflakecomputing.com", "XY12345"},
	} {
		host, jwtAccount, err := resolveAccountHost(identifier)
		require.NoError(t, err, identifier)
		require.Equal(t, want[0], host, identifier)
		require.Equal(t, want[1], jwtAccount, identifier)
	}
	for identifier, message := range map[string]string{
		"":                 "must not be blank",
		" myorg-analytics": "surrounding whitespace",
		"https://myorg-analytics.snowflakecomputing.com": "without https://",
		"myorg-analytics.snowflakecomputing.com":         "without https://",
		"myorg-analytics-prod":                           "keep the account name's underscores",
		"myorg-analytics.us-east-2":                      "must be orgname-account_name",
		"evil.example.com/path":                          "without https://",
		"my_org-analytics":                               "must be orgname-account_name",
		"xy12345.us-east-2.aws.extra":                    "must be orgname-account_name",
	} {
		_, _, err := resolveAccountHost(identifier)
		require.ErrorContains(t, err, message, identifier)
	}
}

func TestNewValidatesConfigurationBeforeContactingSnowflake(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	valid := Config{AccountIdentifier: "myorg-analytics"}
	client, err := New(valid, credentials)
	require.NoError(t, err)
	require.Equal(t, "https://myorg-analytics.snowflakecomputing.com", client.baseURL)
	require.Equal(t, int64(3600), client.statementTimeoutSeconds)
	require.Equal(t, 1000, client.maxRows)

	for name, mutate := range map[string]func(*Config){
		"negative timeout": func(config *Config) { config.StatementTimeoutSeconds = -1 },
		"timeout too long": func(config *Config) { config.StatementTimeoutSeconds = 604801 },
		"quoted warehouse": func(config *Config) { config.Warehouse = `"REPORTING_WH"` },
		"control in role":  func(config *Config) { config.Role = "DEX\nROLE" },
		"too many rows":    func(config *Config) { config.MaxRows = 100001 },
		"too many bytes":   func(config *Config) { config.MaxResponseBytes = 16<<20 + 1 },
	} {
		config := valid
		mutate(&config)
		_, err := New(config, credentials)
		require.Error(t, err, name)
	}
	_, err = New(valid, nil)
	require.Error(t, err)
	_, err = New(valid, credentials, WithLocalProviderURL("http://example.com"))
	require.ErrorContains(t, err, "loopback")
	_, err = New(valid, credentials, WithLocalProviderURL("http://127.0.0.1:9/api"))
	require.ErrorContains(t, err, "path")
}

func TestSignKeyPairJWTUsesSnowflakesDocumentedClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	privateKeyPEM := sdkgo.NewSecretString(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})))
	issuedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	token, err := signKeyPairJWT("XY12345", "dex.service@example.com", privateKeyPEM, issuedAt)
	require.NoError(t, err)
	parts := strings.Split(token.Reveal(), ".")
	require.Len(t, parts, 3)
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	require.JSONEq(t, `{"alg":"RS256","typ":"JWT"}`, string(header))
	claimsText, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(claimsText, &claims))
	fingerprint, err := publicKeyFingerprint(&key.PublicKey)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(fingerprint, "SHA256:"))
	require.Equal(t, map[string]any{
		"iss": "XY12345.DEX.SERVICE@EXAMPLE.COM." + fingerprint, "sub": "XY12345.DEX.SERVICE@EXAMPLE.COM",
		"iat": float64(issuedAt.Unix()), "exp": float64(issuedAt.Add(59 * time.Minute).Unix()),
	}, claims, "no aud claim, upper-case account and user, and a lifetime under one hour")

	encryptedBlockType := "ENCRYPTED " + "PRIVATE KEY"
	encrypted := sdkgo.NewSecretString("-----BEGIN " + encryptedBlockType + "-----\nMIIB\n-----END " + encryptedBlockType + "-----\n")
	_, err = signKeyPairJWT("XY12345", "DEX", encrypted, issuedAt)
	require.ErrorContains(t, err, "encrypted")
	_, err = signKeyPairJWT("XY12345", "DEX", sdkgo.NewSecretString("not a key"), issuedAt)
	require.EqualError(t, err, "the Snowflake private key is not an unencrypted PEM RSA private key")
	_, err = signKeyPairJWT("XY12345", " ", privateKeyPEM, issuedAt)
	require.Error(t, err)
}
