# ClawHub REST API — source review 2026-04-19

Source repo: https://github.com/openclaw/clawhub
Commit pinned: 89246f19272a30d1fd2337c7b1a3838e7e4ddaed

## Domain

`clawhub.com` issues a 307 redirect to `https://clawhub.ai`. The canonical
registry base used by the CLI (confirmed in `packages/clawhub/src/cli/registry.ts`):

```
DEFAULT_REGISTRY = "https://clawhub.ai"
```

All endpoints below use `https://clawhub.ai` as the base.

---

## Endpoints

The source defines two sets of routes in `convex/http.ts`:

- **Legacy** (deprecated, still active): `/api/search`, `/api/skill`, `/api/skill/resolve`, `/api/download`
- **V1 (current)**: `/api/v1/search`, `/api/v1/skills`, `/api/v1/resolve`, `/api/v1/download`

Task 12 should target the V1 routes.

---

### Search

- **Method/URL:** `GET /api/v1/search`
- **Query params:** `q` (required), `limit` (optional int), `highlightedOnly` (bool), `nonSuspiciousOnly` (bool)
- **Auth:** none required for public content
- **Real sample** (`GET https://clawhub.ai/api/v1/search?q=github`):

```json
{
  "results": [
    {
      "score": 3.713023900106303,
      "slug": "openclaw-github-assistant",
      "displayName": "OpenClaw GitHub Assistant",
      "summary": "Query and manage GitHub repositories - list repos, check CI status, create issues, search repos, and view recent activity.",
      "version": null,
      "updatedAt": 1776570385169
    },
    {
      "score": 3.449134119851454,
      "slug": "github",
      "displayName": "Github",
      "summary": "Interact with GitHub using the `gh` CLI.",
      "version": null,
      "updatedAt": 1774865646622
    }
  ]
}
```

Note: `version` is always `null` in the search response (the field is present but the server omits it). This diverges from the spec assumption — see delta table below.

---

### Skill metadata

- **Method/URL:** `GET /api/v1/skills/<slug>`
- **Query params:** none
- **Auth:** none required for public content
- **Real sample** (`GET https://clawhub.ai/api/v1/skills/github`):

```json
{
  "skill": {
    "slug": "github",
    "displayName": "Github",
    "summary": "Interact with GitHub using the `gh` CLI. Use `gh issue`, `gh pr`, `gh run`, and `gh api` for issues, PRs, CI runs, and advanced queries.",
    "tags": { "latest": "1.0.0" },
    "stats": {
      "comments": 7,
      "downloads": 160245,
      "installsAllTime": 4149,
      "installsCurrent": 4025,
      "stars": 520,
      "versions": 1
    },
    "createdAt": 1767545344344,
    "updatedAt": 1774865646622
  },
  "latestVersion": {
    "version": "1.0.0",
    "createdAt": 1767545344344,
    "changelog": "",
    "license": null
  },
  "metadata": null,
  "owner": {
    "handle": "steipete",
    "userId": "s179zksw999xz8ms4cy7pb2fr183m5jq",
    "displayName": "Peter Steinberger",
    "image": "https://avatars.githubusercontent.com/u/58493?v=4"
  },
  "moderation": null
}
```

There is **no `sha256` or `tarball_url` field** in this response. Those live on the version detail endpoint.

---

### Skill version detail

- **Method/URL:** `GET /api/v1/skills/<slug>/versions/<version>`
- **Query params:** none
- **Auth:** none required for public content
- **Real sample** (`GET https://clawhub.ai/api/v1/skills/github/versions/1.0.0`):

```json
{
  "skill": { "slug": "github", "displayName": "Github" },
  "version": {
    "version": "1.0.0",
    "createdAt": 1767545344344,
    "changelog": "",
    "changelogSource": null,
    "license": null,
    "files": [
      {
        "path": "SKILL.md",
        "size": 1113,
        "sha256": "51b28818a6f0359287d5c8244a6fdc59a4ac5504596deb193b28b81832221c86",
        "contentType": "text/markdown"
      }
    ],
    "security": {
      "status": "clean",
      "hasWarnings": true,
      "checkedAt": 1776567608382,
      "model": "gpt-5-mini",
      "hasScanResult": true,
      "sha256hash": "d0bef7d74621458724b7de544cdcedd8aaee25f00bb43142a85bc9ce62b0c2d7",
      "virustotalUrl": "https://www.virustotal.com/gui/file/d0bef7d74621458724b7de544cdcedd8aaee25f00bb43142a85bc9ce62b0c2d7",
      "capabilityTags": []
    }
  }
}
```

The `sha256` in `files[]` is a per-file hash. `security.sha256hash` is the hash of the
whole zip bundle (used for VirusTotal lookup). For integrity verification during install,
use `security.sha256hash` against the downloaded zip.

---

### Tarball / Download

- **Method/URL:** `GET /api/v1/download`
- **Query params:** `slug` (required), `version` (optional — omit to get latest), `tag` (optional)
- **Auth:** none required for public content
- **Response:** `application/zip` binary stream; `Content-Disposition: attachment; filename="<slug>-<version>.zip"`
- **No redirect** — the download is served directly from the Convex backend (files are stored in Convex storage, assembled into a deterministic zip on the fly)
- **Confirmed working** (`GET https://clawhub.ai/api/v1/download?slug=github&version=1.0.0` → 200, 895 bytes)

There is **no pre-signed CDN URL** — the download endpoint IS the distribution mechanism.

**Integrity:** The zip's SHA-256 is exposed at `version.security.sha256hash` in the version
detail response. Consumers should fetch that first, download the zip, then verify.

---

### Additional list endpoints (not in spec, but useful)

- `GET /api/v1/skills` — paginated list of all skills (params: `limit`, `cursor`, `sort`, `nonSuspiciousOnly`)
- `GET /api/v1/skills/<slug>/versions` — paginated version list for a skill
- `GET /api/v1/resolve?slug=<s>&hash=<h>` — check whether a given sha256 hash matches a version

---

## Auth

- **Read operations** (search, skill metadata, version details, download): **no auth required**
- **Write operations** (publish, delete, whoami): `Authorization: Bearer <token>` required
- Token obtained via `clawhub auth login` OAuth flow; stored per-user

---

## Rate Limits

From `convex/lib/httpRateLimit.ts` — window is **60 seconds**:

| Operation type | Anonymous (per IP) | Authenticated (per user) |
|---|---|---|
| read (search, skill GET) | 180 req/min | 900 req/min |
| write (publish) | 45 req/min | 180 req/min |
| download (zip) | 30 req/min | 180 req/min |

Rate limit headers returned on every response (confirmed by curl):
- `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset` (delay seconds, standardized)
- `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` (epoch seconds, legacy)
- `Retry-After` only on 429 responses

---

## Deltas from spec assumptions

| Spec assumed | Reality | Impact on Task 12 |
|---|---|---|
| Domain is `https://clawhub.com` | Domain is `https://clawhub.ai`; `clawhub.com` 307-redirects there | Use `clawhub.ai` as default registry constant; follow redirects or hardcode `.ai` |
| `GET /api/skills/search?q=<query>` | Correct path is `GET /api/v1/search?q=<query>` (v1 prefix) | Update path in Go client |
| Search response: `{slug, display_name, version, description}` | Actual: `{score, slug, displayName, summary, version, updatedAt}`; `version` is always `null`; field is `displayName` not `display_name`, `summary` not `description` | Go struct must use `DisplayName`/`Summary`, handle null version |
| `GET /api/skills/<slug>` returns `{slug, version, sha256, tarball_url}` | Actual: `GET /api/v1/skills/<slug>` returns a richer object; sha256 is NOT here — it lives at `/api/v1/skills/<slug>/versions/<version>` under `security.sha256hash` | Two-step fetch: metadata then version detail to get sha256; no tarball_url field exists |
| Tarball URL can be relative or absolute | No tarball URL exists at all — download is `GET /api/v1/download?slug=<s>&version=<v>` returning a zip directly | Go client must call download endpoint; no URL to pass around |
| No auth required for read operations | Confirmed correct | No change needed |

---

## Recommended Go types for Task 12

```go
// SkillSummary is one entry in the search results array.
type SkillSummary struct {
    Score       float64  `json:"score"`
    Slug        string   `json:"slug"`
    DisplayName string   `json:"displayName"`
    Summary     *string  `json:"summary"`
    // Version is always null in search responses; omit or keep as *string.
    Version     *string  `json:"version"`
    UpdatedAt   int64    `json:"updatedAt"`
}

// SearchResponse wraps the search endpoint payload.
type SearchResponse struct {
    Results []SkillSummary `json:"results"`
}

// SkillMetadata is the /api/v1/skills/<slug> response.
type SkillMetadata struct {
    Skill struct {
        Slug        string            `json:"slug"`
        DisplayName string            `json:"displayName"`
        Summary     *string           `json:"summary"`
        Tags        map[string]string `json:"tags"` // e.g. {"latest": "1.0.0"}
        Stats       struct {
            Downloads       int `json:"downloads"`
            InstallsCurrent int `json:"installsCurrent"`
            Stars           int `json:"stars"`
            Versions        int `json:"versions"`
        } `json:"stats"`
        CreatedAt int64 `json:"createdAt"`
        UpdatedAt int64 `json:"updatedAt"`
    } `json:"skill"`
    LatestVersion *struct {
        Version   string  `json:"version"`
        CreatedAt int64   `json:"createdAt"`
        Changelog string  `json:"changelog"`
        License   *string `json:"license"`
    } `json:"latestVersion"`
    Owner *struct {
        Handle      *string `json:"handle"`
        DisplayName *string `json:"displayName"`
        Image       *string `json:"image"`
    } `json:"owner"`
}

// VersionFile is one file entry inside a version detail response.
type VersionFile struct {
    Path        string  `json:"path"`
    Size        int     `json:"size"`
    SHA256      string  `json:"sha256"`
    ContentType *string `json:"contentType"`
}

// VersionDetail is the /api/v1/skills/<slug>/versions/<version> response.
type VersionDetail struct {
    Skill struct {
        Slug        string `json:"slug"`
        DisplayName string `json:"displayName"`
    } `json:"skill"`
    Version struct {
        Version   string        `json:"version"`
        CreatedAt int64         `json:"createdAt"`
        Changelog string        `json:"changelog"`
        License   *string       `json:"license"`
        Files     []VersionFile `json:"files"`
        Security  *struct {
            Status      string  `json:"status"`
            HasWarnings bool    `json:"hasWarnings"`
            SHA256Hash  *string `json:"sha256hash"`
        } `json:"security"`
    } `json:"version"`
}

// Download: GET /api/v1/download?slug=<s>&version=<v>
// Returns raw application/zip bytes. No URL field — call the endpoint directly.
// Default registry: "https://clawhub.ai"
```
