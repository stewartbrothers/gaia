package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerReleaseTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_release_list",
		mcp.WithDescription("List releases on a repo (newest first). Trimmed Release records inside the gaia envelope."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleReleaseList))

	s.AddTool(mcp.NewTool("gaia_release_view",
		mcp.WithDescription("Get one release by tag."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("tag", mcp.Required()),
	), ctxBoundHandler(handleReleaseView))

	s.AddTool(mcp.NewTool("gaia_release_create",
		mcp.WithDescription("Create a new release."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("tag", mcp.Required(), mcp.Description("tag name; created if missing")),
		mcp.WithString("name", mcp.Description("release name (defaults to tag)")),
		mcp.WithString("body", mcp.Description("release notes (markdown)")),
		mcp.WithString("target", mcp.Description("branch or commit; defaults to default branch")),
		mcp.WithBoolean("draft"),
		mcp.WithBoolean("prerelease"),
	), ctxBoundHandler(handleReleaseCreate))

	s.AddTool(mcp.NewTool("gaia_release_edit",
		mcp.WithDescription("Edit a release identified by tag. Empty fields are unchanged. Set draft=true/false or prerelease=true/false to flip those bits explicitly."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("tag", mcp.Required(), mcp.Description("current tag identifying the release")),
		mcp.WithString("rename", mcp.Description("new tag name")),
		mcp.WithString("name"),
		mcp.WithString("body"),
		mcp.WithBoolean("draft", mcp.Description("explicit set; omit to leave unchanged")),
		mcp.WithBoolean("prerelease", mcp.Description("explicit set; omit to leave unchanged")),
	), ctxBoundHandler(handleReleaseEdit))

	s.AddTool(mcp.NewTool("gaia_release_delete",
		mcp.WithDescription("Delete a release by tag. confirm=true required to actually remove (preview otherwise)."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("tag", mcp.Required()),
		mcp.WithBoolean("confirm"),
	), ctxBoundHandler(handleReleaseDelete))
}

func handleReleaseList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	rels, page, err := p.ListReleases(ctx, owner, repo, provider.ListReleasesOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(rels, page), nil
}

func handleReleaseView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	tag := optString(args, "tag")
	if tag == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "tag is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	rel, err := p.GetRelease(ctx, owner, repo, tag)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(rel, nil), nil
}

func handleReleaseCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	tag := optString(args, "tag")
	if tag == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "tag is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	rel, err := p.CreateRelease(ctx, owner, repo, provider.CreateReleaseOptions{
		TagName:         tag,
		Name:            optString(args, "name"),
		Body:            optString(args, "body"),
		TargetCommitish: optString(args, "target"),
		Draft:           optBool(args, "draft"),
		Prerelease:      optBool(args, "prerelease"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(rel, nil), nil
}

func handleReleaseEdit(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	tag := optString(args, "tag")
	if tag == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "tag is required")), nil
	}
	opts := provider.EditReleaseOptions{
		TagName: optString(args, "rename"),
		Name:    optString(args, "name"),
		Body:    optString(args, "body"),
	}
	if v, present := args["draft"]; present {
		if b, ok := v.(bool); ok {
			opts.Draft = &b
		}
	}
	if v, present := args["prerelease"]; present {
		if b, ok := v.(bool); ok {
			opts.Prerelease = &b
		}
	}

	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	rel, err := p.EditRelease(ctx, owner, repo, tag, opts)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(rel, nil), nil
}

func handleReleaseDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	tag := optString(args, "tag")
	if tag == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "tag is required")), nil
	}
	if !optBool(args, "confirm") {
		return toolResult(map[string]any{"would_delete": tag, "confirmed": false}, nil), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.DeleteRelease(ctx, owner, repo, tag); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"deleted": tag}, nil), nil
}
