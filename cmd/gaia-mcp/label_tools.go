package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerLabelTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_label_list",
		mcp.WithDescription("List repo labels (name + color + description)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
	), ctxBoundHandler(handleLabelList))

	s.AddTool(mcp.NewTool("gaia_label_create",
		mcp.WithDescription("Create a label."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("color", mcp.Required(), mcp.Description("hex color without leading #")),
		mcp.WithString("description"),
	), ctxBoundHandler(handleLabelCreate))

	s.AddTool(mcp.NewTool("gaia_label_edit",
		mcp.WithDescription("Edit a label by current name. Set rename to change the name."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("name", mcp.Required(), mcp.Description("current label name")),
		mcp.WithString("rename", mcp.Description("new label name")),
		mcp.WithString("color"),
		mcp.WithString("description"),
	), ctxBoundHandler(handleLabelEdit))

	s.AddTool(mcp.NewTool("gaia_label_delete",
		mcp.WithDescription("Delete a label by name. Requires confirm=true to actually remove."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("name", mcp.Required()),
		mcp.WithBoolean("confirm", mcp.Description("set true to actually delete; otherwise the call is a no-op preview")),
	), ctxBoundHandler(handleLabelDelete))
}

func handleLabelList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	labels, err := p.ListLabels(ctx, owner, repo)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(labels, nil), nil
}

func handleLabelCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	name := optString(args, "name")
	color := optString(args, "color")
	if name == "" || color == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "name and color are required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	lab, err := p.CreateLabel(ctx, owner, repo, provider.CreateLabelOptions{
		Name:        name,
		Color:       color,
		Description: optString(args, "description"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(lab, nil), nil
}

func handleLabelEdit(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	name := optString(args, "name")
	if name == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "name is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	lab, err := p.EditLabel(ctx, owner, repo, name, provider.EditLabelOptions{
		NewName:     optString(args, "rename"),
		Color:       optString(args, "color"),
		Description: optString(args, "description"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(lab, nil), nil
}

func handleLabelDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	name := optString(args, "name")
	if name == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "name is required")), nil
	}
	if !optBool(args, "confirm") {
		return toolResult(map[string]any{"would_delete": name, "confirmed": false}, nil), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.DeleteLabel(ctx, owner, repo, name); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"deleted": name}, nil), nil
}
