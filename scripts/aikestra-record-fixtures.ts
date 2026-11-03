// Records request/response exchanges from Aikestra's real API handler
// (packages/api over a throwaway Postgres schema, git content faked by
// Aikestra's own test harness) as gaia's contract-test fixtures (#404).
//
// Usage, from an Aikestra checkout with Postgres up (`bun run db:up`):
//   bun /path/to/gaia/scripts/aikestra-record-fixtures.ts \
//     "$PWD" /path/to/gaia/core/aikestra/testdata/fixtures.json
const [root, out] = process.argv.slice(2);
if (!root || !out) throw new Error("usage: bun aikestra-record-fixtures.ts <aikestra checkout> <out.json>");

const { createHarness } = await import(`${root}/packages/api/src/test-support.ts`);
const { projectScope, atProject, AT_SYSTEM } = await import(`${root}/packages/domain/src/index.ts`);

const h = await createHarness();
const fixtures: Record<string, unknown> = {};

type Opts = { token?: string | null; body?: unknown; headers?: Record<string, string> };

async function rec(name: string, method: string, path: string, opts: Opts = {}) {
  const res = await h.call(method, path, opts);
  const wwwAuth = res.headers.get("www-authenticate");
  fixtures[name] = {
    request: { method, path, ...(opts.body !== undefined ? { body: opts.body } : {}) },
    response: {
      status: res.status,
      ...(wwwAuth ? { www_authenticate: wwwAuth } : {}),
      ...(res.body !== "" ? { body: res.body } : {}),
    },
  };
  return res;
}

try {
  const ada = await h.user("ada@example.com");
  const bob = await h.user("bob@example.com");
  const outsider = await h.user("out@example.com");
  const acme = await h.project("acme", ada);
  await h.assign({ kind: "user", id: bob.id }, "project-member", atProject(acme.id));
  const ada_ = (await h.token(ada)).token;
  const bob_ = (await h.token(bob)).token;
  const out_ = (await h.token(outsider)).token;
  const admin = await h.user("admin@example.com");
  await h.assign({ kind: "user", id: admin.id }, "system-admin", AT_SYSTEM);
  const admin_ = (await h.token(admin)).token;
  const agent_ = (await h.token(ada, "delegated-agent")).token;

  // The OpenAPI document needs no token; keep only what gaia reads.
  const doc = await rec("openapi", "GET", "/openapi.json");
  (fixtures.openapi as { response: { body: unknown } }).response.body = {
    openapi: doc.body.openapi,
    info: doc.body.info,
  };

  // Identity and auth.
  await rec("user", "GET", "/user", { token: ada_ });
  await rec("user_agent", "GET", "/user", { token: agent_ });
  await rec("unauthenticated", "GET", "/user", { token: "ak_not_a_real_token" });

  // Issues: four open, so limit=2 ends on an exactly-full page.
  await rec("issue_create", "POST", "/projects/acme/issues", {
    token: ada_,
    body: { title: "Login is slow", body: "Make it fast", type: "bug", assignee: "me" },
  });
  await rec("issue_create_minimal", "POST", "/projects/acme/issues", {
    token: ada_,
    body: { title: "Write docs" },
  });
  await h.call("POST", "/projects/acme/issues", { token: ada_, body: { title: "Ship it" } });
  await h.call("POST", "/projects/acme/issues", { token: ada_, body: { title: "Tidy up" } });

  const p1 = await rec("issues_page1", "GET", "/projects/acme/issues?limit=2", { token: ada_ });
  await rec("issues_page2", "GET", `/projects/acme/issues?limit=2&cursor=${p1.body.next_cursor}`, {
    token: ada_,
  });
  await rec(
    "issues_filtered",
    "GET",
    "/projects/acme/issues?state=all&type=bug&assignee=me&q=login&limit=30",
    { token: ada_ },
  );
  await rec("issue_get", "GET", "/projects/acme/issues/1", { token: ada_ });
  await rec("issue_get_missing", "GET", "/projects/acme/issues/999", { token: ada_ });
  await rec("issue_get_no_access", "GET", "/projects/acme/issues/1", { token: out_ });

  await rec("issue_patch", "PATCH", "/projects/acme/issues/2", {
    token: ada_,
    body: { title: "Write the docs", type: "docs" },
  });
  await rec("issue_close", "PUT", "/projects/acme/issues/2/state", {
    token: ada_,
    body: { state: "closed" },
  });
  await rec("issue_reopen", "PUT", "/projects/acme/issues/2/state", {
    token: ada_,
    body: { state: "open" },
  });
  await rec("issue_assign", "PUT", "/projects/acme/issues/2/assignee", {
    token: ada_,
    body: { assignee: "bob@example.com" },
  });
  await rec("issue_unassign", "PUT", "/projects/acme/issues/2/assignee", {
    token: ada_,
    body: { assignee: null },
  });
  await rec("issue_priority_on", "PUT", "/projects/acme/issues/2/priority", {
    token: ada_,
    body: { priority: true },
  });
  await rec("issue_priority_off", "PUT", "/projects/acme/issues/2/priority", {
    token: ada_,
    body: { priority: false },
  });

  // Required notes.
  await h.d.settings.set(projectScope(acme.id), "issues.require_notes_on_update", true, ada.id);
  await rec("note_required", "PUT", "/projects/acme/issues/2/state", {
    token: ada_,
    body: { state: "closed" },
  });
  await rec("issue_close_with_note", "PUT", "/projects/acme/issues/2/state", {
    token: ada_,
    body: { state: "closed", note: "Done" },
  });
  await rec("issue_reopen_with_note", "PUT", "/projects/acme/issues/2/state", {
    token: ada_,
    body: { state: "open", note: "Not done" },
  });
  await h.d.settings.set(projectScope(acme.id), "issues.require_notes_on_update", false, ada.id);

  // Comments.
  await rec("comment_create", "POST", "/projects/acme/issues/1/comments", {
    token: ada_,
    body: { body: "First look" },
  });
  await h.call("POST", "/projects/acme/issues/1/comments", { token: bob_, body: { body: "Agreed" } });
  await rec("comments", "GET", "/projects/acme/issues/1/comments?limit=100", { token: ada_ });
  const c1 = await rec("comments_page1", "GET", "/projects/acme/issues/1/comments?limit=1", {
    token: ada_,
  });
  await rec(
    "comments_page2",
    "GET",
    `/projects/acme/issues/1/comments?limit=1&cursor=${c1.body.next_cursor}`,
    { token: ada_ },
  );

  // Relationships: 1 Blocks 3, 3 Blocks 4, 1 is 3's related.
  await rec("relationship_add_blocked_by", "POST", "/projects/acme/issues/3/relationships", {
    token: ada_,
    body: { relation: "blocked_by", issue: 1 },
  });
  await rec("relationship_add_blocks", "POST", "/projects/acme/issues/3/relationships", {
    token: ada_,
    body: { relation: "blocks", issue: 4 },
  });
  await h.call("POST", "/projects/acme/issues/3/relationships", {
    token: ada_,
    body: { relation: "related_to", issue: 2 },
  });
  await h.call("POST", "/projects/acme/issues", { token: ada_, body: { title: "Child", parent: 3 } });
  const rels = await rec("relationships", "GET", "/projects/acme/issues/3/relationships?limit=100", {
    token: ada_,
  });
  await rec("issue_get_blocked", "GET", "/projects/acme/issues/3", { token: ada_ });
  await rec("cycle", "POST", "/projects/acme/issues/1/relationships", {
    token: ada_,
    body: { relation: "blocked_by", issue: 3 },
  });
  await rec("relationship_add_missing", "POST", "/projects/acme/issues/3/relationships", {
    token: ada_,
    body: { relation: "blocks", issue: 999 },
  });

  // Boards and placement.
  await rec("boards", "GET", "/projects/acme/boards?limit=100", { token: ada_ });
  const board = await rec("board", "GET", "/projects/acme/boards/main", { token: ada_ });
  const second = board.body.stages[1].name;
  await rec("board_missing", "GET", "/projects/acme/boards/nope", { token: ada_ });
  await rec("placement_blocked", "PUT", "/projects/acme/issues/3/placement", {
    token: ada_,
    body: { stage: second.toLowerCase() },
  });
  await rec("placement", "PUT", "/projects/acme/issues/2/placement", {
    token: ada_,
    body: { stage: second.toLowerCase(), board: "main", note: "Starting" },
  });
  await rec("placement_bad_stage", "PUT", "/projects/acme/issues/2/placement", {
    token: ada_,
    body: { stage: "Nowhere" },
  });

  const blockedBy = rels.body.items.find((r: { relation: string }) => r.relation === "blocked_by");
  await rec(
    "relationship_delete",
    "DELETE",
    `/projects/acme/issues/3/relationships/${blockedBy.id}`,
    { token: ada_ },
  );

  // Repos and refs.
  await rec("repo_create", "POST", "/projects/acme/repos", { token: ada_, body: { name: "app" } });
  await rec("conflict", "POST", "/projects/acme/repos", { token: ada_, body: { name: "app" } });
  await rec("repos", "GET", "/projects/acme/repos?limit=100", { token: ada_ });
  await rec("refs", "GET", "/projects/acme/repos/app/refs?limit=100", { token: ada_ });
  // A System admin sees the Project but holds no repo.* Capability.
  await rec("forbidden", "POST", "/projects/acme/repos", { token: admin_, body: { name: "x" } });

  // Request-shape errors.
  await rec("invalid_fields", "GET", "/projects/acme/issues/1?fields=nope", { token: ada_ });
  await rec("invalid_cursor", "GET", "/projects/acme/issues?cursor=bad", { token: ada_ });
  await rec("invalid_request", "GET", "/projects/acme/issues?limit=500", { token: ada_ });
  await rec("method_not_allowed", "DELETE", "/projects/acme/issues/1", { token: ada_ });
  await rec("unsupported_media_type", "POST", "/projects/acme/issues", {
    token: ada_,
    headers: { "content-type": "text/plain" },
    body: { title: "x" },
  });
  await rec("validation_failed", "POST", "/projects/acme/issues", {
    token: ada_,
    body: { title: "Typed", type: "nonsense" },
  });
} finally {
  await h.close();
}

await Bun.write(out, `${JSON.stringify(fixtures, null, 2)}\n`);
console.log(Object.entries(fixtures).map(([k, v]) => `${k} ${(v as { response: { status: number } }).response.status}`).join("\n"));
