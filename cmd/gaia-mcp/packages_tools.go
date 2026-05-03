package main

import (
	"context"
	"encoding/base64"
	"strings"

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

	s.AddTool(mcp.NewTool("gaia_packages_upload",
		mcp.WithDescription("Publish one artifact to a package version (Forgejo generic registry). The body is supplied as base64 in 'body_base64' or as raw text in 'body' (the latter is convenient for text-shaped artifacts; binary content needs base64). filename is the on-server filename within the version. Only Forgejo + pkgType=generic is implemented; other combinations return a documented 'not implemented' error."),
		mcp.WithString("owner", mcp.Required()),
		mcp.WithString("type", mcp.Required(), mcp.Description("registry kind; only 'generic' is supported in #122")),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("version", mcp.Required()),
		mcp.WithString("filename", mcp.Required(), mcp.Description("on-server filename within the package version")),
		mcp.WithString("content_type", mcp.Description("MIME type; defaults to application/octet-stream")),
		mcp.WithString("body", mcp.Description("raw body (mutually exclusive with body_base64)")),
		mcp.WithString("body_base64", mcp.Description("base64-encoded body (preferred for binary artifacts)")),
	), ctxBoundHandler(handlePackagesUpload))
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

func handlePackagesUpload(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	owner, pkgType, name, version, err := packageArgs(args)
	if err != nil {
		return toolError(err), nil
	}
	fileName := optString(args, "filename")
	if fileName == "" {
		return toolError(exitcode.Errorf(exitcode.Usage, "filename is required")), nil
	}

	body, err := uploadBodyFromArgs(args)
	if err != nil {
		return toolError(err), nil
	}

	p, err := build(ctx)
	if err != nil {
		return toolError(err), nil
	}
	if err := p.UploadPackage(ctx, owner, pkgType, name, version,
		provider.UploadPackageOptions{
			FileName:    fileName,
			ContentType: optString(args, "content_type"),
		}, body); err != nil {
		return toolError(err), nil
	}
	return toolResult(map[string]any{
		"uploaded": map[string]any{
			"owner":    owner,
			"type":     pkgType,
			"name":     name,
			"version":  version,
			"filename": fileName,
		},
	}, nil), nil
}

// uploadBodyFromArgs returns the upload body based on either
// "body_base64" (preferred for binaries) or "body" (raw text).
// Exactly one must be set. MCP transports JSON, so binary data has
// to round-trip through base64 — that's the documented path; "body"
// is a convenience for text-shaped artifacts that don't survive
// base64 round-tripping cleanly.
func uploadBodyFromArgs(args map[string]any) (*strings.Reader, error) {
	rawText := optString(args, "body")
	rawB64 := optString(args, "body_base64")
	if rawText == "" && rawB64 == "" {
		return nil, exitcode.Errorf(exitcode.Usage, "one of body or body_base64 is required")
	}
	if rawText != "" && rawB64 != "" {
		return nil, exitcode.Errorf(exitcode.Usage, "body and body_base64 are mutually exclusive")
	}
	if rawB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(rawB64)
		if err != nil {
			return nil, exitcode.Wrap(err, exitcode.Usage, "decode body_base64")
		}
		return strings.NewReader(string(decoded)), nil
	}
	return strings.NewReader(rawText), nil
}
