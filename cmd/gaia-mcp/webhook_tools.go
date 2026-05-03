package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerWebhookTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_webhook_list",
		mcp.WithDescription("List webhooks configured on a repo. Trimmed Webhook records inside the gaia envelope."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleWebhookList))

	s.AddTool(mcp.NewTool("gaia_webhook_view",
		mcp.WithDescription("Get one webhook by ID."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
	), ctxBoundHandler(handleWebhookView))

	s.AddTool(mcp.NewTool("gaia_webhook_create",
		mcp.WithDescription("Create a new webhook. Secret travels in the request body but is never returned."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("url", mcp.Required(), mcp.Description("destination URL")),
		mcp.WithString("content_type", mcp.Required(), mcp.Description("json or form")),
		mcp.WithString("secret", mcp.Description("HMAC signing secret")),
		mcp.WithArray("events", mcp.Required(), mcp.Description("events to subscribe to (e.g. push, pull_request)")),
		mcp.WithBoolean("active"),
	), ctxBoundHandler(handleWebhookCreate))

	s.AddTool(mcp.NewTool("gaia_webhook_edit",
		mcp.WithDescription("Edit a webhook by ID. add_events/remove_events apply incrementally over the current event list."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
		mcp.WithString("url"),
		mcp.WithString("content_type", mcp.Description("json or form")),
		mcp.WithString("secret", mcp.Description("rotate to this new secret")),
		mcp.WithArray("add_events"),
		mcp.WithArray("remove_events"),
		mcp.WithBoolean("active", mcp.Description("set to true/false to flip activation; omit for no change")),
	), ctxBoundHandler(handleWebhookEdit))

	s.AddTool(mcp.NewTool("gaia_webhook_delete",
		mcp.WithDescription("Delete a webhook by ID. confirm=true required to actually remove (preview otherwise)."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
		mcp.WithBoolean("confirm"),
	), ctxBoundHandler(handleWebhookDelete))

	s.AddTool(mcp.NewTool("gaia_webhook_deliveries",
		mcp.WithDescription("List recent delivery summaries for a webhook (no payload bodies). Pass delivery_id for the full request/response payload of one delivery."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
		mcp.WithNumber("delivery_id", mcp.Description("if set, returns the single full delivery instead of a list")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleWebhookDeliveries))

	s.AddTool(mcp.NewTool("gaia_webhook_redeliver",
		mcp.WithDescription("Re-fire a previously-sent delivery. Receiver sees the same payload + signature with a redelivery flag."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
		mcp.WithNumber("delivery_id", mcp.Required()),
	), ctxBoundHandler(handleWebhookRedeliver))

	s.AddTool(mcp.NewTool("gaia_webhook_test",
		mcp.WithDescription("Send a synthetic ping event to a webhook. Useful for validating receiver reachability."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithNumber("id", mcp.Required()),
	), ctxBoundHandler(handleWebhookTest))
}

func optInt64(args map[string]any, key string) int64 {
	switch v := args[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func handleWebhookList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	hooks, page, err := p.ListWebhooks(ctx, owner, repo, provider.ListWebhooksOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(hooks, page), nil
}

func handleWebhookView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id := optInt64(args, "id")
	if id <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "id is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	h, err := p.GetWebhook(ctx, owner, repo, id)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(h, nil), nil
}

func handleWebhookCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	urlV := optString(args, "url")
	ct := optString(args, "content_type")
	if urlV == "" || ct == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "url and content_type are required")), nil
	}
	if ct != "json" && ct != "form" {
		return toolError(exitcode.Errorf(exitcode.Usage, "content_type must be json or form; got %q", ct)), nil
	}
	events := optStringSlice(args, "events")
	if len(events) == 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "events is required (at least one)")), nil
	}
	active := true
	if v, ok := args["active"].(bool); ok {
		active = v
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	h, err := p.CreateWebhook(ctx, owner, repo, provider.CreateWebhookOptions{
		URL:         urlV,
		ContentType: ct,
		Secret:      optString(args, "secret"),
		Events:      events,
		Active:      active,
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(h, nil), nil
}

func handleWebhookEdit(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id := optInt64(args, "id")
	if id <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "id is required")), nil
	}
	if ct := optString(args, "content_type"); ct != "" && ct != "json" && ct != "form" {
		return toolError(exitcode.Errorf(exitcode.Usage, "content_type must be json or form; got %q", ct)), nil
	}
	opts := provider.EditWebhookOptions{
		URL:          optString(args, "url"),
		ContentType:  optString(args, "content_type"),
		Secret:       optString(args, "secret"),
		AddEvents:    optStringSlice(args, "add_events"),
		RemoveEvents: optStringSlice(args, "remove_events"),
	}
	if v, present := args["active"]; present {
		if b, ok := v.(bool); ok {
			opts.Active = &b
		}
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	h, err := p.EditWebhook(ctx, owner, repo, id, opts)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(h, nil), nil
}

func handleWebhookDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id := optInt64(args, "id")
	if id <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "id is required")), nil
	}
	if !optBool(args, "confirm") {
		return toolResult(map[string]any{"would_delete": id, "confirmed": false}, nil), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.DeleteWebhook(ctx, owner, repo, id); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"deleted": id}, nil), nil
}

func handleWebhookDeliveries(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id := optInt64(args, "id")
	if id <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "id is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if dID := optInt64(args, "delivery_id"); dID > 0 {
		detail, err := p.GetWebhookDelivery(ctx, owner, repo, id, dID)
		if err != nil {
			return toolError(err), nil
		}
		return toolResult(detail, nil), nil
	}
	dels, page, err := p.ListWebhookDeliveries(ctx, owner, repo, id, provider.ListDeliveriesOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(dels, page), nil
}

func handleWebhookRedeliver(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id := optInt64(args, "id")
	dID := optInt64(args, "delivery_id")
	if id <= 0 || dID <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "id and delivery_id are required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.RedeliverWebhook(ctx, owner, repo, id, dID); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"redelivered": dID}, nil), nil
}

func handleWebhookTest(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	id := optInt64(args, "id")
	if id <= 0 {
		return toolError(exitcode.Errorf(exitcode.Usage, "id is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.TestWebhook(ctx, owner, repo, id); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"tested": id}, nil), nil
}
