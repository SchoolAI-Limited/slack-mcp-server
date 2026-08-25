package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/trace"
)

// drafts.* API — internal Slack APIs for native unsent drafts.
// Only accessible with browser session tokens (xoxc/xoxd).

type DraftDestination struct {
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"thread_ts,omitempty"`
	Broadcast bool   `json:"broadcast"`
}

type DraftRichTextBlock struct {
	Type     string                 `json:"type"`
	Elements []DraftRichTextElement `json:"elements"`
}

type DraftRichTextElement struct {
	Type     string          `json:"type"`
	Elements []DraftTextPart `json:"elements"`
}

type DraftTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type DraftPayload struct {
	Blocks       string `json:"blocks"`
	Destinations string `json:"destinations"`
	ClientMsgID  string `json:"client_msg_id"`
	FileIDs      string `json:"file_ids"`
}

type draftsListRequest struct {
	BaseRequest
	IsActive bool `json:"is_active"`
	Limit    int  `json:"limit,omitempty"`
}

type DraftsListResponse struct {
	baseResponse
	Drafts []map[string]any `json:"drafts,omitempty"`
}

type draftsCreateRequest struct {
	BaseRequest
	DraftPayload
	IsFromComposer string `json:"is_from_composer"`
}

type DraftsMutateResponse struct {
	baseResponse
	Draft map[string]any `json:"draft,omitempty"`
}

type draftsUpdateRequest struct {
	BaseRequest
	DraftPayload
	DraftID             string `json:"draft_id"`
	ClientLastUpdatedTS string `json:"client_last_updated_ts"`
}

type draftsDeleteForm struct {
	BaseRequest
	DraftID             string `json:"draft_id"`
	ClientLastUpdatedTS string `json:"client_last_updated_ts"`
}

func NewDraftPayload(channelID, text, threadTS string, broadcast bool, clientMsgID string) (DraftPayload, error) {
	blocks := []DraftRichTextBlock{
		{
			Type: "rich_text",
			Elements: []DraftRichTextElement{
				{
					Type: "rich_text_section",
					Elements: []DraftTextPart{
						{Type: "text", Text: text},
					},
				},
			},
		},
	}

	destination := DraftDestination{ChannelID: channelID, Broadcast: broadcast}
	if threadTS != "" {
		destination.ThreadTS = threadTS
	}
	destinations := []DraftDestination{destination}

	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return DraftPayload{}, fmt.Errorf("marshal draft blocks: %w", err)
	}
	destinationsJSON, err := json.Marshal(destinations)
	if err != nil {
		return DraftPayload{}, fmt.Errorf("marshal draft destinations: %w", err)
	}
	fileIDsJSON, err := json.Marshal([]string{})
	if err != nil {
		return DraftPayload{}, fmt.Errorf("marshal draft file_ids: %w", err)
	}

	return DraftPayload{
		Blocks:       string(blocksJSON),
		Destinations: string(destinationsJSON),
		ClientMsgID:  clientMsgID,
		FileIDs:      string(fileIDsJSON),
	}, nil
}

func (cl *Client) PostWebJSON(ctx context.Context, path string, req PostRequest) (*http.Response, error) {
	if !req.IsTokenSet() {
		req.SetToken(cl.token)
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	tape := cl.recorder(bytes.NewReader(data))
	defer cl.record([]byte("\n\n"))
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, cl.webclientAPI+path, tape)
	if err != nil {
		return nil, err
	}
	r.Header.Set(hdrContentType, "application/json")
	return do(ctx, cl.cl, r)
}

func (cl *Client) DraftsList(ctx context.Context, limit int) (DraftsListResponse, error) {
	ctx, task := trace.NewTask(ctx, "DraftsList")
	defer task.End()

	req := draftsListRequest{
		BaseRequest: BaseRequest{Token: cl.token},
		IsActive:    true,
		Limit:       limit,
	}
	resp, err := cl.PostWebJSON(ctx, "drafts.list", &req)
	if err != nil {
		return DraftsListResponse{}, err
	}
	r := DraftsListResponse{}
	if err := cl.ParseResponse(&r, resp); err != nil {
		return DraftsListResponse{}, err
	}
	if err := r.validate("drafts.list"); err != nil {
		return DraftsListResponse{}, err
	}
	return r, nil
}

func (cl *Client) DraftsCreate(ctx context.Context, payload DraftPayload) (DraftsMutateResponse, error) {
	ctx, task := trace.NewTask(ctx, "DraftsCreate")
	defer task.End()

	req := draftsCreateRequest{
		BaseRequest:    BaseRequest{Token: cl.token},
		DraftPayload:   payload,
		IsFromComposer: "true",
	}
	resp, err := cl.PostWebJSON(ctx, "drafts.create", &req)
	if err != nil {
		return DraftsMutateResponse{}, err
	}
	r := DraftsMutateResponse{}
	if err := cl.ParseResponse(&r, resp); err != nil {
		return DraftsMutateResponse{}, err
	}
	if err := r.validate("drafts.create"); err != nil {
		return DraftsMutateResponse{}, err
	}
	return r, nil
}

func (cl *Client) DraftsUpdate(ctx context.Context, draftID, clientLastUpdatedTS string, payload DraftPayload) (DraftsMutateResponse, error) {
	ctx, task := trace.NewTask(ctx, "DraftsUpdate")
	defer task.End()

	req := draftsUpdateRequest{
		BaseRequest:         BaseRequest{Token: cl.token},
		DraftPayload:        payload,
		DraftID:             draftID,
		ClientLastUpdatedTS: clientLastUpdatedTS,
	}
	resp, err := cl.PostWebJSON(ctx, "drafts.update", &req)
	if err != nil {
		return DraftsMutateResponse{}, err
	}
	r := DraftsMutateResponse{}
	if err := cl.ParseResponse(&r, resp); err != nil {
		return DraftsMutateResponse{}, err
	}
	if err := r.validate("drafts.update"); err != nil {
		return DraftsMutateResponse{}, err
	}
	return r, nil
}

func (cl *Client) DraftsDelete(ctx context.Context, draftID, clientLastUpdatedTS string) error {
	ctx, task := trace.NewTask(ctx, "DraftsDelete")
	defer task.End()

	form := draftsDeleteForm{
		BaseRequest:         BaseRequest{Token: cl.token},
		DraftID:             draftID,
		ClientLastUpdatedTS: clientLastUpdatedTS,
	}
	resp, err := cl.PostForm(ctx, "drafts.delete", values(form, true))
	if err != nil {
		return err
	}
	r := baseResponse{}
	if err := cl.ParseResponse(&r, resp); err != nil {
		return err
	}
	if err := r.validate("drafts.delete"); err != nil {
		return err
	}
	return nil
}
