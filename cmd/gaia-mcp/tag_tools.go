package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerTagTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_tag_list",
		mcp.WithDescription("List the repository's git tags (name, target commit, and annotated-tag message)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("limit", mcp.Description("max tags to return")),
		mcp.WithString("cursor", mcp.Description("opaque pagination cursor from a previous response")),
	), ctxBoundHandler(handleTagList))

	s.AddTool(mcp.NewTool("gaia_tag_create",
		mcp.WithDescription("Create a git tag. `from` is a branch, tag, or commit to tag; omit it to use the repo's default branch. Independent of releases."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("name", mcp.Required(), mcp.Description("new tag name")),
		mcp.WithString("from", mcp.Description("source ref (branch, tag, or commit); default: the repo's default branch")),
	), ctxBoundHandler(handleTagCreate))

	s.AddTool(mcp.NewTool("gaia_tag_delete",
		mcp.WithDescription("Delete a git tag by name."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithString("name", mcp.Required(), mcp.Description("tag name to delete")),
	), ctxBoundHandler(handleTagDelete))
}

func handleTagList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	tags, page, err := p.ListTags(ctx, owner, repo, provider.ListTagsOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(tags, page), nil
}

func handleTagCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	tag, err := p.CreateTag(ctx, owner, repo, name, provider.CreateTagOptions{From: optString(args, "from")})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(tag, nil), nil
}

func handleTagDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
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
	if err := p.DeleteTag(ctx, owner, repo, name); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"deleted": true, "tag": name}, nil), nil
}
