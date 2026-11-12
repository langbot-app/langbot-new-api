package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func streamResponse(body string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "text/event-stream")
	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

// Some relays answer a streaming request with HTTP 200 + a bare JSON error.
// The SSE scanner skips non-"data:" lines, so without this check the client
// got an empty 200 stream and zero usage instead of the upstream error.
func TestUpstreamStreamErrorBodyConvertsJSONErrorDocument(t *testing.T) {
	resp := streamResponse(`{"error":{"message":"* tools[0].function_declarations[13].properties[search_after].items: missing field.","type":"upstream_error","param":"","code":400}}`)

	apiErr := UpstreamStreamErrorBody(context.Background(), resp)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "items: missing field")
}

func TestUpstreamStreamErrorBodyKeepsSSEBodyIntact(t *testing.T) {
	sse := "\n: ping\n\ndata: {\"candidates\":[]}\n\ndata: [DONE]\n"
	resp := streamResponse(sse)

	require.Nil(t, UpstreamStreamErrorBody(context.Background(), resp))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, sse, string(body))
}

func TestUpstreamStreamErrorBodyKeepsNonErrorJSON(t *testing.T) {
	doc := `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`
	resp := streamResponse(doc)

	require.Nil(t, UpstreamStreamErrorBody(context.Background(), resp))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, doc, string(body))
}

func TestUpstreamStreamErrorBodyDefaultsToBadGateway(t *testing.T) {
	resp := streamResponse(`{"error":{"message":"upstream exploded","code":"internal"}}`)

	apiErr := UpstreamStreamErrorBody(context.Background(), resp)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
}
