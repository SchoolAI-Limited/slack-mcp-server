package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/korotovsky/slack-mcp-server/pkg/provider"
	"github.com/korotovsky/slack-mcp-server/pkg/provider/edge"
	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

type DraftsHandler struct {
	apiProvider *provider.ApiProvider
	logger      *zap.Logger
}

func NewDraftsHandler(apiProvider *provider.ApiProvider, logger *zap.Logger) *DraftsHandler {
	return &DraftsHandler{apiProvider: apiProvider, logger: logger}
}

func (h *DraftsHandler) DraftsListHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Debug("DraftsListHandler called", zap.Any("params", request.Params))

	limit := request.GetInt("limit", 100)
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("limit must be between 1 and 1000")
	}

	resp, err := h.apiProvider.Slack().DraftsList(ctx, limit)
	if err != nil {
		h.logger.Error("DraftsList failed", zap.Error(err))
		return nil, fmt.Errorf("failed to list drafts: %v", err)
	}
	return jsonToolResult(resp)
}

func (h *DraftsHandler) DraftsCreateHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Debug("DraftsCreateHandler called", zap.Any("params", request.Params))

	params, err := draftWriteParamsFromRequest(request, false)
	if err != nil {
		return nil, err
	}
	payload, err := draftPayloadFromParams(params)
	if err != nil {
		return nil, err
	}

	resp, err := h.apiProvider.Slack().DraftsCreate(ctx, payload)
	if err != nil {
		h.logger.Error("DraftsCreate failed",
			zap.String("channel_id", params.channelID),
			zap.String("thread_ts", params.threadTS),
			zap.Error(err))
		return nil, fmt.Errorf("failed to create draft: %v", err)
	}
	return jsonToolResult(resp)
}

func (h *DraftsHandler) DraftsUpdateHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Debug("DraftsUpdateHandler called", zap.Any("params", request.Params))

	params, err := draftWriteParamsFromRequest(request, true)
	if err != nil {
		return nil, err
	}
	payload, err := draftPayloadFromParams(params)
	if err != nil {
		return nil, err
	}

	resp, err := h.apiProvider.Slack().DraftsUpdate(ctx, params.draftID, params.clientLastUpdatedTS, payload)
	if err != nil {
		h.logger.Error("DraftsUpdate failed",
			zap.String("draft_id", params.draftID),
			zap.String("channel_id", params.channelID),
			zap.String("thread_ts", params.threadTS),
			zap.Error(err))
		return nil, fmt.Errorf("failed to update draft: %v", err)
	}
	return jsonToolResult(resp)
}

func (h *DraftsHandler) DraftsDeleteHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.logger.Debug("DraftsDeleteHandler called", zap.Any("params", request.Params))

	draftID := strings.TrimSpace(request.GetString("draft_id", ""))
	clientLastUpdatedTS := strings.TrimSpace(request.GetString("client_last_updated_ts", ""))
	if draftID == "" || clientLastUpdatedTS == "" {
		return nil, fmt.Errorf("draft_id and client_last_updated_ts are required parameters")
	}

	if err := h.apiProvider.Slack().DraftsDelete(ctx, draftID, padDraftTimestamp(clientLastUpdatedTS)); err != nil {
		h.logger.Error("DraftsDelete failed",
			zap.String("draft_id", draftID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to delete draft: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("Successfully deleted draft %s", draftID)), nil
}

type draftWriteParams struct {
	draftID             string
	clientLastUpdatedTS string
	channelID           string
	text                string
	threadTS            string
	broadcast           bool
}

func draftWriteParamsFromRequest(request mcp.CallToolRequest, requireConflictFields bool) (draftWriteParams, error) {
	params := draftWriteParams{
		draftID:             strings.TrimSpace(request.GetString("draft_id", "")),
		clientLastUpdatedTS: strings.TrimSpace(request.GetString("client_last_updated_ts", "")),
		channelID:           strings.TrimSpace(request.GetString("channel_id", "")),
		text:                request.GetString("text", ""),
		threadTS:            strings.TrimSpace(request.GetString("thread_ts", "")),
		broadcast:           request.GetBool("broadcast", false),
	}

	if requireConflictFields && (params.draftID == "" || params.clientLastUpdatedTS == "") {
		return draftWriteParams{}, fmt.Errorf("draft_id and client_last_updated_ts are required parameters")
	}
	if params.channelID == "" || params.text == "" {
		return draftWriteParams{}, fmt.Errorf("channel_id and text are required parameters")
	}
	if params.broadcast && params.threadTS == "" {
		return draftWriteParams{}, fmt.Errorf("broadcast requires thread_ts")
	}
	params.clientLastUpdatedTS = padDraftTimestamp(params.clientLastUpdatedTS)
	return params, nil
}

func draftPayloadFromParams(params draftWriteParams) (edge.DraftPayload, error) {
	clientMsgID, err := newClientMsgID()
	if err != nil {
		return edge.DraftPayload{}, err
	}
	return edge.NewDraftPayload(params.channelID, params.text, params.threadTS, params.broadcast, clientMsgID)
}

func padDraftTimestamp(ts string) string {
	parts := strings.SplitN(ts, ".", 2)
	if len(parts) != 2 {
		return ts
	}
	if len(parts[1]) >= 7 {
		return ts
	}
	return parts[0] + "." + parts[1] + strings.Repeat("0", 7-len(parts[1]))
}

func newClientMsgID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate draft client_msg_id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	), nil
}

func jsonToolResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Slack response: %v", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}
