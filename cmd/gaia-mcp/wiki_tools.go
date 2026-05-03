package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// registerWikiTools wires the five gaia_wiki_* MCP tools — same
// surface as `gaia wiki list/view/search/edit/delete` — onto the
// server. Search is the agent-cost win: one MCP tool call replaces
// N WebFetches across the wiki.
func registerWikiTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_wiki_list",
		mcp.WithDescription("List wiki pages on a repo. Returns title + path + last_commit; bodies are not included (call gaia_wiki_view for body)."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handleWikiList))

	s.AddTool(mcp.NewTool("gaia_wiki_view",
		mcp.WithDescription("Get one wiki page by path (slug), including its markdown body."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("path", mcp.Required(), mcp.Description("page slug, e.g. \"Home\" or \"Setup-Guide\"")),
	), ctxBoundHandler(handleWikiView))

	s.AddTool(mcp.NewTool("gaia_wiki_search",
		mcp.WithDescription("Search wiki pages for a query. Client-side title + body match (Forgejo has no native wiki search). Capped at max_pages (default 100); larger wikis should narrow the query."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("query", mcp.Required(), mcp.Description("search term; case-insensitive")),
		mcp.WithNumber("max_pages", mcp.Description("cap on pages scanned (default 100)")),
	), ctxBoundHandler(handleWikiSearch))

	s.AddTool(mcp.NewTool("gaia_wiki_edit",
		mcp.WithDescription("Create or replace a wiki page (upsert)."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("path", mcp.Required(), mcp.Description("page slug")),
		mcp.WithString("body", mcp.Required(), mcp.Description("markdown body")),
	), ctxBoundHandler(handleWikiEdit))

	s.AddTool(mcp.NewTool("gaia_wiki_delete",
		mcp.WithDescription("Delete a wiki page. Requires confirm=true to actually remove (preview otherwise)."),
		mcp.WithString("repo", mcp.Required()),
		mcp.WithString("path", mcp.Required()),
		mcp.WithBoolean("confirm"),
	), ctxBoundHandler(handleWikiDelete))
}

func handleWikiList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	pages, page, err := p.ListWikiPages(ctx, owner, repo, provider.ListWikiPagesOptions{
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(pages, page), nil
}

func handleWikiView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	path := optString(args, "path")
	if path == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "path is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	page, err := p.GetWikiPage(ctx, owner, repo, path)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(page, nil), nil
}

func handleWikiSearch(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	query := optString(args, "query")
	if query == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "query is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	hits, err := p.SearchWikiPages(ctx, owner, repo, query, provider.SearchWikiOptions{
		MaxPages: optInt(args, "max_pages"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(hits, nil), nil
}

func handleWikiEdit(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	path := optString(args, "path")
	body := optString(args, "body")
	if path == "" || body == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "path and body are required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	page, err := p.EditWikiPage(ctx, owner, repo, path, body)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(page, nil), nil
}

func handleWikiDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, repo, err := repoFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	path := optString(args, "path")
	if path == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "path is required")), nil
	}
	if !optBool(args, "confirm") {
		return toolResult(map[string]any{"would_delete": path, "confirmed": false}, nil), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.DeleteWikiPage(ctx, owner, repo, path); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{"deleted": path}, nil), nil
}
