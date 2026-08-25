package edge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type draftRoundTripper struct {
	requests     []*http.Request
	bodies       []string
	responseBody string
}

func (rt *draftRoundTripper) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	rt.requests = append(rt.requests, req)
	rt.bodies = append(rt.bodies, string(body))
	responseBody := rt.responseBody
	if responseBody == "" {
		responseBody = `{"ok":true,"draft":{"id":"DRAFT123","last_updated_ts":"1772034406.5935090"}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(responseBody)),
		Header:     make(http.Header),
	}, nil
}

func newDraftTestClient(rt *draftRoundTripper) *Client {
	return &Client{
		cl:           rt,
		webclientAPI: "https://schoolai.slack.com/api/",
		token:        "xoxc-test",
		tape:         nopTape{},
	}
}

func decodeDraftRequest(t *testing.T, body string) map[string]any {
	t.Helper()
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	return got
}

func decodeStringJSON[T any](t *testing.T, raw any) T {
	t.Helper()
	s, ok := raw.(string)
	require.True(t, ok)
	var got T
	require.NoError(t, json.Unmarshal([]byte(s), &got))
	return got
}

func TestDraftPayloadShapeAndMultilinePreservation(t *testing.T) {
	payload, err := NewDraftPayload("C123", "first line\nsecond line", "1772034406.593509", false, "client-msg-1")
	require.NoError(t, err)

	blocks := decodeStringJSON[[]DraftRichTextBlock](t, payload.Blocks)
	require.Len(t, blocks, 1)
	assert.Equal(t, "rich_text", blocks[0].Type)
	assert.Equal(t, "rich_text_section", blocks[0].Elements[0].Type)
	assert.Equal(t, "first line\nsecond line", blocks[0].Elements[0].Elements[0].Text)

	destinations := decodeStringJSON[[]DraftDestination](t, payload.Destinations)
	require.Len(t, destinations, 1)
	assert.Equal(t, "C123", destinations[0].ChannelID)
	assert.Equal(t, "1772034406.593509", destinations[0].ThreadTS)
	assert.False(t, destinations[0].Broadcast)

	fileIDs := decodeStringJSON[[]string](t, payload.FileIDs)
	assert.Empty(t, fileIDs)
	assert.Equal(t, "client-msg-1", payload.ClientMsgID)
}

func TestDraftPayloadBroadcastDefaultsFalseWithoutThread(t *testing.T) {
	payload, err := NewDraftPayload("C123", "hello", "", false, "client-msg-1")
	require.NoError(t, err)

	destinations := decodeStringJSON[[]DraftDestination](t, payload.Destinations)
	require.Len(t, destinations, 1)
	assert.Equal(t, "C123", destinations[0].ChannelID)
	assert.Empty(t, destinations[0].ThreadTS)
	assert.False(t, destinations[0].Broadcast)
}

func TestDraftsCreateRequestShape(t *testing.T) {
	rt := &draftRoundTripper{}
	client := newDraftTestClient(rt)
	payload, err := NewDraftPayload("C123", "hello", "", false, "client-msg-1")
	require.NoError(t, err)

	resp, err := client.DraftsCreate(context.Background(), payload)
	require.NoError(t, err)
	assert.True(t, resp.Ok)
	require.Len(t, rt.requests, 1)
	assert.Equal(t, "https://schoolai.slack.com/api/drafts.create", rt.requests[0].URL.String())
	assert.Equal(t, "application/json", rt.requests[0].Header.Get("Content-Type"))

	got := decodeDraftRequest(t, rt.bodies[0])
	assert.Equal(t, "xoxc-test", got["token"])
	assert.Equal(t, "client-msg-1", got["client_msg_id"])
	assert.Equal(t, "true", got["is_from_composer"])
	assert.NotContains(t, got, "date_scheduled")
	assert.NotContains(t, got, "send")
	assert.NotContains(t, got, "post")

	fileIDs := decodeStringJSON[[]string](t, got["file_ids"])
	assert.Empty(t, fileIDs)
}

func TestDraftsUpdateRequestShape(t *testing.T) {
	rt := &draftRoundTripper{}
	client := newDraftTestClient(rt)
	payload, err := NewDraftPayload("C123", "updated", "1772034406.593509", true, "client-msg-2")
	require.NoError(t, err)

	resp, err := client.DraftsUpdate(context.Background(), "DRAFT123", "1772034406.5935090", payload)
	require.NoError(t, err)
	assert.True(t, resp.Ok)
	require.Len(t, rt.requests, 1)
	assert.Equal(t, "https://schoolai.slack.com/api/drafts.update", rt.requests[0].URL.String())

	got := decodeDraftRequest(t, rt.bodies[0])
	assert.Equal(t, "DRAFT123", got["draft_id"])
	assert.Equal(t, "1772034406.5935090", got["client_last_updated_ts"])
	assert.NotContains(t, got, "date_scheduled")
	assert.NotContains(t, got, "send")

	destinations := decodeStringJSON[[]DraftDestination](t, got["destinations"])
	require.Len(t, destinations, 1)
	assert.True(t, destinations[0].Broadcast)
}

func TestDraftsDeleteRequestShape(t *testing.T) {
	rt := &draftRoundTripper{}
	client := newDraftTestClient(rt)

	err := client.DraftsDelete(context.Background(), "DRAFT123", "1772034406.5935090")
	require.NoError(t, err)
	require.Len(t, rt.requests, 1)
	assert.Equal(t, "https://schoolai.slack.com/api/drafts.delete", rt.requests[0].URL.String())
	assert.Equal(t, "application/x-www-form-urlencoded", rt.requests[0].Header.Get("Content-Type"))

	assert.Contains(t, rt.bodies[0], "draft_id=DRAFT123")
	assert.Contains(t, rt.bodies[0], "client_last_updated_ts=1772034406.5935090")
	assert.NotContains(t, rt.bodies[0], "channel")
	assert.NotContains(t, rt.bodies[0], "date_scheduled")
}

func TestDraftsFailClosedOnSlackNotOK(t *testing.T) {
	rt := &draftRoundTripper{responseBody: `{"ok":false,"error":"draft_has_conflict"}`}
	client := newDraftTestClient(rt)
	payload, err := NewDraftPayload("C123", "updated", "", false, "client-msg-2")
	require.NoError(t, err)

	_, err = client.DraftsUpdate(context.Background(), "DRAFT123", "1772034406.5935090", payload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "draft_has_conflict")
}
