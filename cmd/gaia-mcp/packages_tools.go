package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func registerPackagesTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("gaia_packages_list",
		mcp.WithDescription("List packages owned by 'owner' (user/org). Trimmed Package records inside the gaia envelope."),
		mcp.WithString("owner", mcp.Required(), mcp.Description("package owner (user or org)")),
		mcp.WithString("type", mcp.Description("registry kind filter (npm|maven|container|generic|...)")),
		mcp.WithString("q", mcp.Description("name-substring filter")),
		mcp.WithNumber("limit"),
		mcp.WithString("cursor"),
	), ctxBoundHandler(handlePackagesList))

	s.AddTool(mcp.NewTool("gaia_packages_view",
		mcp.WithDescription("View one package version by (owner, type, name, version)."),
		mcp.WithString("owner", mcp.Required()),
		mcp.WithString("type", mcp.Required(), mcp.Description("registry kind")),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("version", mcp.Required()),
	), ctxBoundHandler(handlePackagesView))

	s.AddTool(mcp.NewTool("gaia_packages_delete",
		mcp.WithDescription("Delete one package version. confirm=true required to actually remove (preview otherwise)."),
		mcp.WithString("owner", mcp.Required()),
		mcp.WithString("type", mcp.Required()),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("version", mcp.Required()),
		mcp.WithBoolean("confirm"),
	), ctxBoundHandler(handlePackagesDelete))
}

func handlePackagesList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner := optString(args, "owner")
	if owner == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "owner is required")), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	pkgs, page, err := p.ListPackages(ctx, owner, provider.ListPackagesOptions{
		Type:   optString(args, "type"),
		Q:      optString(args, "q"),
		Limit:  optInt(args, "limit"),
		Cursor: optString(args, "cursor"),
	})
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(pkgs, page), nil
}

func handlePackagesView(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, pkgType, name, version, err := packageArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	pkg, err := p.GetPackage(ctx, owner, pkgType, name, version)
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(pkg, nil), nil
}

func handlePackagesDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, pkgType, name, version, err := packageArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	if !optBool(args, "confirm") {
		return toolResult(map[string]any{
			"would_delete": map[string]any{
				"owner":   owner,
				"type":    pkgType,
				"name":    name,
				"version": version,
			},
			"confirmed": false,
		}, nil), nil
	}
	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.DeletePackage(ctx, owner, pkgType, name, version); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{
		"deleted": map[string]any{
			"owner":   owner,
			"type":    pkgType,
			"name":    name,
			"version": version,
		},
	}, nil), nil
}

// packageArgs extracts the four required fields shared by view and
// delete. All four are required; missing any of them returns a
// usage error so the agent gets a single clear message instead of
// a 404 from the upstream.
func packageArgs(args map[string]any) (owner, pkgType, name, version string, err error) {
	owner = optString(args, "owner")
	pkgType = optString(args, "type")
	name = optString(args, "name")
	version = optString(args, "version")
	if owner == "" || pkgType == "" || name == "" || version == "" {
		return "", "", "", "", exitcode.Errorf(exitcode.Usage,
			"owner, type, name, and version are all required")
	}
	return owner, pkgType, name, version, nil
}
