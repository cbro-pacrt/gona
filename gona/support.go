package gona

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// TicketListOptions contains optional filters for ticket list endpoints.
type TicketListOptions struct {
	Open         *string
	IncludeStats *string
}

// Ticket is a support ticket.
type Ticket struct {
	ID         string          `json:"id,omitempty"`
	Subject    string          `json:"subject,omitempty"`
	Status     string          `json:"status,omitempty"`
	Department string          `json:"department,omitempty"`
	Urgency    string          `json:"urgency,omitempty"`
	CreatedAt  string          `json:"created_at,omitempty"`
	UpdatedAt  string          `json:"updated_at,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// UnmarshalJSON records the complete ticket payload while exposing common fields.
func (t *Ticket) UnmarshalJSON(data []byte) error {
	type Alias Ticket
	aux := (*Alias)(t)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	t.Raw = append(t.Raw[:0], data...)
	return nil
}

// TicketReply is a support ticket reply.
type TicketReply struct {
	ID        string          `json:"id,omitempty"`
	Message   string          `json:"message,omitempty"`
	CreatedAt string          `json:"created_at,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// UnmarshalJSON records the complete ticket reply payload while exposing common fields.
func (r *TicketReply) UnmarshalJSON(data []byte) error {
	type Alias TicketReply
	aux := (*Alias)(r)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.Raw = append(r.Raw[:0], data...)
	return nil
}

// TicketDepartment is a support ticket department.
type TicketDepartment struct {
	ID   int             `json:"id,omitempty"`
	Name string          `json:"name,omitempty"`
	Raw  json.RawMessage `json:"-"`
}

// UnmarshalJSON records the complete ticket department payload while exposing common fields.
func (d *TicketDepartment) UnmarshalJSON(data []byte) error {
	type Alias TicketDepartment
	aux := (*Alias)(d)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	d.Raw = append(d.Raw[:0], data...)
	return nil
}

// TicketAttachment is support ticket attachment metadata.
type TicketAttachment struct {
	Name        string          `json:"name,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Size        int             `json:"size,omitempty"`
	Data        string          `json:"data,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON records the complete attachment metadata payload while exposing common fields.
func (a *TicketAttachment) UnmarshalJSON(data []byte) error {
	type Alias TicketAttachment
	aux := (*Alias)(a)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	a.Raw = append(a.Raw[:0], data...)
	return nil
}

// CreateTicketRequest is the POST /support/tickets body.
type CreateTicketRequest struct {
	Subject    string   `json:"subject"`
	Message    string   `json:"message"`
	Department int      `json:"department"`
	Urgency    string   `json:"urgency,omitempty"`
	Files      []string `json:"files,omitempty"`
}

// ReplyTicketRequest is the ticket reply body.
type ReplyTicketRequest struct {
	Message string   `json:"message"`
	Files   []string `json:"files,omitempty"`
}

// GetLegacyTickets returns archived legacy tickets.
func (c *Client) GetLegacyTickets() ([]Ticket, error) {
	var tickets []Ticket
	if err := c.get(context.Background(), "support/legacy-tickets", &tickets); err != nil {
		return nil, fmt.Errorf("get archived legacy tickets: %w", err)
	}
	return tickets, nil
}

// GetTickets returns support tickets with optional filters.
func (c *Client) GetTickets(opts TicketListOptions) ([]Ticket, error) {
	var tickets []Ticket
	if err := c.get(context.Background(), ticketListPath("support/tickets", opts), &tickets); err != nil {
		return nil, fmt.Errorf("get support tickets: %w", err)
	}
	return tickets, nil
}

// CreateTicket creates a support ticket.
func (c *Client) CreateTicket(req *CreateTicketRequest) (*Ticket, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("create support ticket request: %w", err)
	}
	var ticket Ticket
	if err := c.postJSON(context.Background(), "support/tickets", body, &ticket); err != nil {
		return nil, fmt.Errorf("create support ticket: %w", err)
	}
	return &ticket, nil
}

// GetOldTickets returns legacy support tickets with optional filters.
func (c *Client) GetOldTickets(opts TicketListOptions) ([]Ticket, error) {
	var tickets []Ticket
	if err := c.get(context.Background(), ticketListPath("support/tickets-old", opts), &tickets); err != nil {
		return nil, fmt.Errorf("get legacy support tickets: %w", err)
	}
	return tickets, nil
}

// GetOldTicket returns a legacy support ticket by id.
func (c *Client) GetOldTicket(id string) (*Ticket, error) {
	var ticket Ticket
	path := "support/tickets-old/" + url.PathEscape(id)
	if err := c.get(context.Background(), path, &ticket); err != nil {
		return nil, fmt.Errorf("get legacy support ticket %s: %w", id, err)
	}
	return &ticket, nil
}

// GetTicketDepartments returns support ticket departments.
func (c *Client) GetTicketDepartments() ([]TicketDepartment, error) {
	var departments []TicketDepartment
	if err := c.get(context.Background(), "support/tickets/departments", &departments); err != nil {
		return nil, fmt.Errorf("get support ticket departments: %w", err)
	}
	return departments, nil
}

// GetTicket returns a support ticket by id.
func (c *Client) GetTicket(id string) (*Ticket, error) {
	var ticket Ticket
	path := "support/tickets/" + url.PathEscape(id)
	if err := c.get(context.Background(), path, &ticket); err != nil {
		return nil, fmt.Errorf("get support ticket %s: %w", id, err)
	}
	return &ticket, nil
}

// GetTicketReplies returns replies for a support ticket.
func (c *Client) GetTicketReplies(id string) ([]TicketReply, error) {
	var replies []TicketReply
	path := "support/tickets/" + url.PathEscape(id) + "/replies"
	if err := c.get(context.Background(), path, &replies); err != nil {
		return nil, fmt.Errorf("get support ticket %s replies: %w", id, err)
	}
	return replies, nil
}

// GetTicketAttachment returns attachment metadata for a ticket or reply attachment.
func (c *Client) GetTicketAttachment(id, attachmentType, relID string, index int, withoutData *int) (*TicketAttachment, error) {
	path := ticketAttachmentPath(id, attachmentType, relID, index)
	if withoutData != nil {
		values := url.Values{}
		values.Set("without_data", strconv.Itoa(*withoutData))
		path += "?" + values.Encode()
	}

	var attachment TicketAttachment
	if err := c.get(context.Background(), path, &attachment); err != nil {
		return nil, fmt.Errorf("get support ticket %s attachment: %w", id, err)
	}
	return &attachment, nil
}

// DownloadTicketAttachment returns the binary content for a ticket or reply attachment.
func (c *Client) DownloadTicketAttachment(id, attachmentType, relID string, index int) ([]byte, error) {
	path := ticketAttachmentPath(id, attachmentType, relID, index) + "/download"
	body, err := c.getRaw(context.Background(), path)
	if err != nil {
		return nil, fmt.Errorf("download support ticket %s attachment: %w", id, err)
	}
	return body, nil
}

// PreviewTicketAttachment returns inline content for a ticket or reply attachment.
func (c *Client) PreviewTicketAttachment(id, attachmentType, relID string, index int) ([]byte, error) {
	path := ticketAttachmentPath(id, attachmentType, relID, index) + "/preview"
	body, err := c.getRaw(context.Background(), path)
	if err != nil {
		return nil, fmt.Errorf("preview support ticket %s attachment: %w", id, err)
	}
	return body, nil
}

// CloseTicket closes a support ticket.
func (c *Client) CloseTicket(id string) error {
	path := "support/tickets/" + url.PathEscape(id) + "/close"
	if err := c.postJSON(context.Background(), path, nil, nil); err != nil {
		return fmt.Errorf("close support ticket %s: %w", id, err)
	}
	return nil
}

// CloseTicketAlias closes a support ticket through the alternate close path.
func (c *Client) CloseTicketAlias(id string) error {
	path := "support/tickets/close/" + url.PathEscape(id)
	if err := c.postJSON(context.Background(), path, nil, nil); err != nil {
		return fmt.Errorf("close support ticket %s: %w", id, err)
	}
	return nil
}

// ReplyToTicket replies to a support ticket.
func (c *Client) ReplyToTicket(id string, req *ReplyTicketRequest) (*TicketReply, error) {
	return c.replyToTicket("support/tickets/"+url.PathEscape(id)+"/reply", id, req)
}

// ReplyToTicketAlias replies to a support ticket through the alternate reply path.
func (c *Client) ReplyToTicketAlias(id string, req *ReplyTicketRequest) (*TicketReply, error) {
	return c.replyToTicket("support/tickets/reply/"+url.PathEscape(id), id, req)
}

func (c *Client) replyToTicket(path, id string, req *ReplyTicketRequest) (*TicketReply, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("reply to support ticket request: %w", err)
	}
	var reply TicketReply
	if err := c.postJSON(context.Background(), path, body, &reply); err != nil {
		return nil, fmt.Errorf("reply to support ticket %s: %w", id, err)
	}
	return &reply, nil
}

func ticketListPath(path string, opts TicketListOptions) string {
	values := url.Values{}
	if opts.Open != nil {
		values.Set("open", *opts.Open)
	}
	if opts.IncludeStats != nil {
		values.Set("include_stats", *opts.IncludeStats)
	}
	if encoded := values.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}

func ticketAttachmentPath(id, attachmentType, relID string, index int) string {
	return fmt.Sprintf("support/tickets/%s/attachment/%s/%s/%d",
		url.PathEscape(id),
		url.PathEscape(attachmentType),
		url.PathEscape(relID),
		index,
	)
}

func (c *Client) getRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("got an error response on %s %s: code %d, response: %s",
			req.Method, redactURL(req.URL), resp.StatusCode, string(body))
	}
	return body, nil
}
