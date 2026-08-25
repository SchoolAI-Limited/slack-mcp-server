package handler

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnitPadDraftTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "already seven decimals", input: "1772034406.5935090", expected: "1772034406.5935090"},
		{name: "six decimals pads one zero", input: "1772034406.593509", expected: "1772034406.5935090"},
		{name: "short fraction pads to seven", input: "1772034406.5", expected: "1772034406.5000000"},
		{name: "no decimal unchanged", input: "1772034406", expected: "1772034406"},
		{name: "long fraction unchanged", input: "1772034406.59350901", expected: "1772034406.59350901"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, padDraftTimestamp(tt.input))
		})
	}
}

func TestUnitDraftWriteParamValidation(t *testing.T) {
	t.Run("create requires channel_id and text", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"channel_id": "C123"}

		_, err := draftWriteParamsFromRequest(req, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "channel_id and text")
	})

	t.Run("update requires conflict fields", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"channel_id": "C123",
			"text":       "hello",
		}

		_, err := draftWriteParamsFromRequest(req, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "draft_id and client_last_updated_ts")
	})

	t.Run("broadcast requires thread_ts", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"channel_id": "C123",
			"text":       "hello",
			"broadcast":  true,
		}

		_, err := draftWriteParamsFromRequest(req, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "broadcast requires thread_ts")
	})

	t.Run("update pads client_last_updated_ts", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"draft_id":               "DRAFT123",
			"client_last_updated_ts": "1772034406.593509",
			"channel_id":             "C123",
			"text":                   "hello",
		}

		params, err := draftWriteParamsFromRequest(req, true)
		require.NoError(t, err)
		assert.Equal(t, "1772034406.5935090", params.clientLastUpdatedTS)
	})
}
