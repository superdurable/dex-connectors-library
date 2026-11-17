// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisputeFileEvidenceChecksWorkerFile(t *testing.T) {
	directory := t.TempDir()
	document := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF\n")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "sample.pdf"), document, 0600))
	digest := sha256.Sum256(document)
	input := CreateDisputeFileEvidenceInput{FileName: "sample.pdf", ExpectedSHA256: hex.EncodeToString(digest[:])}
	client := &Client{evidenceDirectory: directory}
	actual, contentType, err := client.readDisputeEvidenceFile(input)
	require.NoError(t, err)
	require.Equal(t, document, actual)
	require.Equal(t, "application/pdf", contentType)

	input.ExpectedSHA256 = hex.EncodeToString(make([]byte, sha256.Size))
	_, _, err = client.readDisputeEvidenceFile(input)
	require.Error(t, err)
	oversized := bytes.Repeat([]byte("A"), maximumDisputeEvidenceBytes+1)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "oversized.pdf"), oversized, 0600))
	oversizedDigest := sha256.Sum256(oversized)
	input.FileName = "oversized.pdf"
	input.ExpectedSHA256 = hex.EncodeToString(oversizedDigest[:])
	_, _, err = client.readDisputeEvidenceFile(input)
	require.Error(t, err)

	outsideDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDirectory, "outside.pdf"), document, 0600))
	require.NoError(t, os.Symlink(filepath.Join(outsideDirectory, "outside.pdf"), filepath.Join(directory, "link.pdf")))
	input.FileName = "link.pdf"
	input.ExpectedSHA256 = hex.EncodeToString(digest[:])
	_, _, err = client.readDisputeEvidenceFile(input)
	require.Error(t, err)

	require.False(t, validEvidenceFileName("../outside.pdf"))
	require.False(t, validSHA256Hex("not-a-digest"))
}
