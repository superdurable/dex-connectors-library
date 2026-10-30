// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/connectors/docusign"
)

// directoryDocumentStore writes each signed combined PDF to one file per envelope.
type directoryDocumentStore struct {
	directory string
}

// newDirectoryDocumentStore creates directory, readable only by this user, when it is missing.
func newDirectoryDocumentStore(directory string) (*directoryDocumentStore, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create signed document directory: %w", err)
	}
	return &directoryDocumentStore{directory: directory}, nil
}

// StoreCombinedDocument renames a complete temporary file into place, so repeats replace it whole.
func (store *directoryDocumentStore) StoreCombinedDocument(
	_ context.Context, document docusign.CombinedDocumentReference, content io.Reader,
) (string, error) {
	name := document.AccountID + "-" + document.EnvelopeID + ".pdf"
	if document.IncludesCertificate {
		name = document.AccountID + "-" + document.EnvelopeID + "-with-certificate.pdf"
	}
	temporary, err := os.CreateTemp(store.directory, ".incoming-*.pdf")
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(temporary, content)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		// The partial file is useless; a failed removal leaves only an ignored .incoming file.
		_ = os.Remove(temporary.Name())
		return "", err
	}
	if err := os.Rename(temporary.Name(), filepath.Join(store.directory, name)); err != nil {
		_ = os.Remove(temporary.Name()) // As above, an .incoming file is never read.
		return "", err
	}
	return name, nil
}
