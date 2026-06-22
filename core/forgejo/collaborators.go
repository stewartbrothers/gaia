package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiCollaborator mirrors one entry in Forgejo's collaborators list, which
// is a BARE ARRAY of user objects. Crucially the repo permission level is
// NOT inline here — it's resolved with a separate per-user call.
type apiCollaborator struct {
	Login string `json:"login"`
}

// apiPermission mirrors Forgejo's per-collaborator permission response:
// GET /repos/{o}/{r}/collaborators/{user}/permission → {permission: "..."}.
type apiPermission struct {
	Permission string `json:"permission"`
}

// ListCollaborators returns the repo's collaborator access list. Forgejo's
// list endpoint returns a bare array of users without the permission
// level, so for each collaborator we make one extra call to
// /collaborators/{login}/permission to resolve it ("admin"/"write"/"read"/
// "none"). This is N+1 calls; acceptable for an access-audit read where N
// is small (the set of users with explicit repo access).
func (p *Provider) ListCollaborators(ctx context.Context, owner, repo string, opts provider.ListCollaboratorsOptions) ([]types.Collaborator, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/collaborators?%s", owner, repo, q.Encode())

	var raw []apiCollaborator
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}

	out := make([]types.Collaborator, 0, len(raw))
	for i := range raw {
		c := types.Collaborator{Login: raw[i].Login}
		perm, err := p.collaboratorPermission(ctx, owner, repo, raw[i].Login)
		if err != nil {
			return nil, nil, err
		}
		c.Permission = perm
		out = append(out, c)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// collaboratorPermission resolves one collaborator's effective permission
// level via Forgejo's per-user permission endpoint.
func (p *Provider) collaboratorPermission(ctx context.Context, owner, repo, login string) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/collaborators/%s/permission", owner, repo, login)
	var perm apiPermission
	if err := p.client.Get(ctx, path, &perm); err != nil {
		return "", err
	}
	return perm.Permission, nil
}
