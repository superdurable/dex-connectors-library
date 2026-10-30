// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const combinedPath = envelopePath + "/documents/combined"

// signedPDF stands in for a signed combined PDF.
var signedPDF = []byte("%PDF-1.7\n% signed agreement\n" + string(bytes.Repeat([]byte("stream-bytes "), 4096)) + "\n%%EOF\n")

// recordingDocumentStore keeps every stored document in memory.
type recordingDocumentStore struct {
	mu        sync.Mutex
	documents map[string][]byte
	stored    []docusign.CombinedDocumentReference
	fail      error
	readLimit int
}

func (store *recordingDocumentStore) StoreCombinedDocument(_ context.Context, document docusign.CombinedDocumentReference, content io.Reader) (string, error) {
	if store.readLimit > 0 {
		_, err := io.ReadFull(content, make([]byte, store.readLimit))
		return "partial.pdf", err
	}
	contents, err := io.ReadAll(content)
	if err != nil {
		return "", err
	}
	if store.fail != nil {
		return "", store.fail
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.documents == nil {
		store.documents = map[string][]byte{}
	}
	location := "signed/" + document.EnvelopeID + ".pdf"
	store.documents[location] = contents
	store.stored = append(store.stored, document)
	return location, nil
}

func pdfResponse(contents []byte) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/pdf")
		response.Header().Set("Content-Length", strconv.Itoa(len(contents)))
		_, _ = response.Write(contents) // A failed write is the interrupted download under test elsewhere.
	}
}

func downloadDocument(t *testing.T, fake *fakeDocuSign, config docusign.Config, options ...docusign.Option) (docusign.DownloadCombinedDocumentResult, error) {
	t.Helper()
	client := newTestClient(t, fake, staticCredentials(productionCredentials()), config, options...)
	return sdkgo.RunQuery(newTestDexContext("download"), client.DownloadCombinedDocument(), testConnection,
		docusign.DownloadCombinedDocumentInput{EnvelopeID: testEnvelopeID, IncludeCertificate: true})
}

func TestDownloadCombinedDocumentStoresThePDFAndRecordsOnlyItsDigest(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + combinedPath: pdfResponse(signedPDF)})
	store := &recordingDocumentStore{}
	result, err := downloadDocument(t, fake, docusign.Config{}, docusign.WithCombinedDocumentStore(store))
	require.NoError(t, err)
	require.Equal(t, docusign.DownloadCombinedDocumentBranchDownloaded, result.Branch)
	digest := sha256.Sum256(signedPDF)
	require.Equal(t, docusign.CombinedDocument{
		EnvelopeID: testEnvelopeID, ContentType: "application/pdf", ByteCount: int64(len(signedPDF)),
		SHA256: hex.EncodeToString(digest[:]), IncludesCertificate: true, StoredLocation: "signed/" + testEnvelopeID + ".pdf",
	}, result.Value)
	require.Equal(t, signedPDF, store.documents[result.Value.StoredLocation])
	require.Equal(t, []docusign.CombinedDocumentReference{{AccountID: testAccountID, EnvelopeID: testEnvelopeID, IncludesCertificate: true}}, store.stored)
	request := fake.requestsTo(http.MethodGet, combinedPath)[0]
	require.Equal(t, "true", request.query.Get("certificate"))
	require.Equal(t, "application/pdf", request.accept)
	require.NotContains(t, sdkgoJSON(t, result), "signed agreement", "the PDF never enters the Result")
}

func TestDownloadCombinedDocumentRecordsTheDigestWithoutAStore(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + combinedPath: pdfResponse(signedPDF)})
	result, err := downloadDocument(t, fake, docusign.Config{})
	require.NoError(t, err)
	require.Equal(t, docusign.DownloadCombinedDocumentBranchDownloaded, result.Branch)
	require.Equal(t, int64(len(signedPDF)), result.Value.ByteCount)
	require.Empty(t, result.Value.StoredLocation)
}

func TestDownloadCombinedDocumentRefusesOversizedAndNonPDFContent(t *testing.T) {
	chunked := func(contents []byte) http.HandlerFunc {
		return func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/pdf")
			response.(http.Flusher).Flush()
			_, _ = response.Write(contents) // The connector stops reading at the limit.
		}
	}
	for name, test := range map[string]struct {
		answer         http.HandlerFunc
		expectedBranch sdkgo.BranchID
	}{
		"declared length over the limit": {pdfResponse(signedPDF), docusign.DownloadCombinedDocumentBranchTooLarge},
		"streamed past the limit":        {chunked(signedPDF), docusign.DownloadCombinedDocumentBranchTooLarge},
		"HTML error page":                {respondJSON(http.StatusOK, `<html>oops</html>`), docusign.DownloadCombinedDocumentBranchInvalidResponse},
		"PDF media type without a PDF": {func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/pdf")
			_, _ = io.WriteString(response, "not a pdf")
		}, docusign.DownloadCombinedDocumentBranchInvalidResponse},
		"missing envelope": {respondJSON(http.StatusBadRequest, docusignError("ENVELOPE_DOES_NOT_EXIST")), docusign.DownloadCombinedDocumentBranchNotFound},
		"no access":        {respondJSON(http.StatusBadRequest, docusignError("ACCOUNT_LACKS_PERMISSIONS")), docusign.DownloadCombinedDocumentBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + combinedPath: test.answer})
			store := &recordingDocumentStore{}
			result, err := downloadDocument(t, fake, docusign.Config{MaxDocumentBytes: 1024}, docusign.WithCombinedDocumentStore(store))
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Empty(t, store.documents, "nothing is stored from a refused download")
			requireNoSecrets(t, result)
		})
	}
}

func TestDownloadCombinedDocumentReportsAStoreThatDidNotStoreTheWholePDF(t *testing.T) {
	for name, store := range map[string]*recordingDocumentStore{
		"store error":  {fail: errors.New("bucket unavailable")},
		"partial read": {readLimit: 16},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + combinedPath: pdfResponse(signedPDF)})
			result, err := downloadDocument(t, fake, docusign.Config{}, docusign.WithCombinedDocumentStore(store))
			require.NoError(t, err)
			require.Equal(t, docusign.DownloadCombinedDocumentBranchInvalidResponse, result.Branch)
			require.NotContains(t, result.Failure.Message, "bucket unavailable", "the store's error text stays local")
		})
	}
}

func TestDownloadCombinedDocumentRetriesAnOutage(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"GET " + combinedPath: respondJSON(http.StatusServiceUnavailable, `{}`)})
	_, err := downloadDocument(t, fake, docusign.Config{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
