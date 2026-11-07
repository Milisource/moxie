# F95Zone programmatic surface — live probe report

> **Probed 2026-11-07** from a single IP with a normal Firefox 141 User-Agent,
> **anonymous** (no cookie, no `xf_user`, no `cf_clearance` passed by us), using
> **25 HTTP requests total**, spaced ≥3 s. Every claim below is tagged
> **EVIDENCE** (exact request + response snippet) or **INFERENCE**. Prior
> findings in [`latest-data-api.md`](../latest-data-api.md) (verified
> 2026-10-08) are treated as ground truth and are *not* re-derived here; this
> report starts where that one stops.
>
> Scope: what a Go CLI/TUI + desktop app ("moxie") can actually call on
> `f95zone.to`, what needs a session, and what each unused endpoint would
> unlock.
>
> Read with: [`scraper.md`](../scraper.md),
> [`latest-data-api.md`](../latest-data-api.md),
> [`research-f95zone-recaptcha.md`](../research-f95zone-recaptcha.md).

---

## 0. Probe log (what was actually sent)

| # | Request (method path) | Status | Bytes | Note |
|---|---|---|---|---|
| 0 | `GET /robots.txt` | 200 | 4 796 | text/plain |
| 1 | `GET /sam/latest_alpha/latest_data.php?cmd=list&cat=games&prefixes[]=7&date=30&rows=15` | 200 | 20 003 | JSON |
| 2 | `…cmd=list&cat=games&tags[]=2265&rows=15` | 200 | 16 373 | JSON |
| 3 | `…cmd=list&cat=games&sort=title&rows=15` | 200 | 15 594 | JSON |
| 4 | `…cmd=list&cat=games&creator=dik&rows=15` | 200 | 13 056 | JSON |
| 5 | `…cmd=tags&cat=games&prefixes[]=7` | 200 | 3 158 | JSON |
| 6 | `…cmd=rss&cat=games` | 200 | 14 313 | XML, 30 items |
| 7 | `GET /sam/latest_alpha/` | 200 | **87** | login wall body |
| 8 | `GET /sam/latest_alpha/assets/js/latest.min.js` | 200 | 42 372 | public JS |
| 9 | `GET /api/` | **400** | 181 | XF error JSON |
| 10 | `GET /threads/524/` | 301 | 0 | → canonical slug |
| 11 | `GET /threads/524/latest` | 303 | 0 | → `#post-21316555` |
| 12 | `GET /watched/threads` | **403** | 98 376 | login page |
| 13 | `GET /whats-new/` | 200 | 159 325 | trending index |
| 14 | `GET /find-threads/started` | **403** | 98 381 | login page |
| 15 | `GET /forums/games.2/index.rss` | 200 | 20 789 | 19 items |
| 16 | `HEAD /sitemap.xml` | 200 | — | sitemap index |
| 17 | `GET /sam/checker.php?threads=524,1024` | **404** | 43 | all-untracked chunk |
| 18 | `GET /threads/game-uploading-rules-2024-02-29.524/` (follow) | 200 | 288 875 | anon thread page |
| 19 | `GET /sitemap.xml` | 200 | 553 | index of 4 |
| 20 | `POST /sam/latest_alpha/latest_data.php` (`cmd=options`) | **500** | 53 | anonymous |
| 21 | `GET /threads/321921` (follow) | 301→200 | 228 898 | real game thread |
| 22 | `GET /tags/` | 200 | 115 801 | tag cloud, 113 tags |

No response carried `cf-mitigated`, `Retry-After`, or a `Set-Cookie` we asked
for. The only cookies in play were none. **All 25 requests succeeded at the
HTTP layer except the four deliberately-auth-gated/error paths.**

---

## 1. Endpoint inventory

`Auth` legend: **anon** = no cookie; **session** = `xf_user` cookie needed;
**clearance** = Cloudflare `cf_clearance` in addition to session; **key** =
XenForo API key.

| Method | URL | Auth | Returns | Status 2026-11-07 | moxie verdict |
|---|---|---|---|---|---|
| GET | `/api/` | key (`XF-Api-Key`) | XF2 REST JSON | **400 `no_api_key_in_request`** | unusable (no key issued) |
| GET | `/threads/<id>/` | anon (metadata) / session (links) | XenForo HTML | 301 → canonical slug → 200 | **uses** (with session) |
| GET | `/threads/<id>/latest` | anon | 303 → `…/#post-<id>` | 303 | unused, valuable |
| GET | `/threads/<id>/unread` | session | 303 → first unread | not tested | unusable anon |
| POST | `/search/search` | session + `_xfToken` | search HTML | known (not re-tested) | uses as fallback |
| GET | `/sam/latest_alpha/` | session | SPA HTML + `latestUpdates` taxonomy global | **200 login wall, 87 B** | partial (taxonomy not implemented) |
| GET | `/sam/latest_alpha/assets/js/latest.min.js` | anon | 42 KB JS | 200 | unused; only endpoints, no names |
| GET | `/sam/latest_alpha/latest_data.php?cmd=list` | anon | JSON rows | 200 | **uses partially** (no filters) |
| GET | `…?cmd=tags` | anon | JSON `{tag_id,count}` | 200 | unused |
| GET | `…?cmd=rss` | anon | RSS, 30 items | 200 | unused in code |
| POST | `…?cmd=options` | session | — | **500 anon** | not useful |
| GET | `/sam/checker.php?threads=<csv>` | anon | JSON versions | 200 / 404 (all-untracked) | uses |
| GET | `/forums/<id>/index.rss` | anon | RSS 19–20 items | 200 | unused |
| GET | `/whats-new/` | anon | trending index HTML | 200 (robots-disallowed) | unused; compliance caveat |
| GET | `/tags/` | anon | tag cloud, names + counts | 200 | unused |
| GET | `/sitemap.xml` | anon | index of 4 sitemaps | 200 | unused |
| GET | `/watched/threads` | session | HTML watch list | **403 login page** | unusable anon |
| GET | `/find-threads/started` | session | HTML | **403 login page** | unusable anon |
| GET | `/attachments/<file>.<id>/` | session/signature | bytes | known | uses |
| GET | `/dl/<postID>/?u&k&r&t` | link token | check page → destination | known | uses |
| GET | `/masked/<host>/…` | anon/session | masked interstitial | known | uses |
| GET | `/forums/games.2/` | anon | forum HTML + prefix filter list | known | uses once (prefix map) |
| GET | `/robots.txt` | anon | rules + sitemap | 200 | unused |

---

## 2. Per-endpoint detail (evidence)

### 2.1 XenForo REST API — enabled, key-gated

```
GET https://f95zone.to/api/          (Accept: application/json)
HTTP/2 400   content-type: application/json
{"errors":[{"code":"no_api_key_in_request",
            "message":"No API key was included in the request.","params":[]}]}
```

**EVIDENCE.** This is a real XenForo 2 REST API endpoint, not a 404 stub: it
returns the framework's canonical error envelope. It wants `XF-Api-Key`.

- **INFERENCE:** one could only use it with a super-admin-issued key. There is
  no public registration path; F95Zone does not advertise keys. Treat as
  **unusable for moxie** unless a user personally obtains a key.
- **Correction to `scraper.md`:** that doc says the API "requires a
  super-administrator-issued key, which F95Zone doesn't offer third parties."
  The conclusion is right; the new evidence is that the API is **enabled**, so
  a future key would immediately unlock typed JSON for threads/forums/users.

### 2.2 `/sam/latest_alpha/` — login wall, JavaScript is public

```
GET /sam/latest_alpha/
HTTP/2 200   content-type: text/html; charset=UTF-8   (87 bytes)

<pre>Sorry, you have to be <a href="/login">logged in</a> to access this page</a></pre>
```

**EVIDENCE.** Note the status is **200, not 302/403** — code that checks only
the status code will treat this as success and parse an empty page. The body is
a hard login wall.

The asset it loads is **not** gated:

```
GET /sam/latest_alpha/assets/js/latest.min.js   → 200, 42 372 B, application/javascript
```

**EVIDENCE.** The bundle references only four endpoint forms:

```
latest_data.php?cmd=list&cat=…
latest_data.php?cmd=tags&cat=…
latest_data.php?cmd=options      (POST)
…and cmd=rss is produced by  url.replace(/cmd=list/,"cmd=rss")
```

**EVIDENCE:** `var l=this.url.replace(/cmd=list/,"cmd=rss")...` — so **RSS
inherits the full `cmd=list` filter query**, not just `cat`. No other
`/sam/…` or `*.php` endpoint exists in the bundle.

The `latestUpdates = {prefixes,tags,options}` taxonomy global is **not** in
this JS; it is inlined into the gated HTML. The JS being public does not leak
tag names.

### 2.3 `latest_data.php` — anonymous filters CONFIRMED

All four previously-unconfirmed filter/sort forms work **without any cookie**:

```
GET …?cmd=list&cat=games&prefixes[]=7&date=30&rows=15
→ 200, {"status":"ok","msg":{"data":[…891 results…],
        "pagination":{"page":1,"total":60},"count":891}}
```

```
GET …?cmd=list&cat=games&tags[]=2265&rows=15
→ 200, pagination.total=207, count=3101
```

```
GET …?cmd=list&cat=games&sort=title&rows=15
→ first titles: "!!Slutia Dungeon Crawler RPG",
                "!!Vengeance of the Minotaur RPG",
                "!NPC SEX! …"
```

```
GET …?cmd=list&cat=games&creator=dik&rows=15
→ count=12, 1 page; rows include "Dice Dik Saga"
```

**EVIDENCE.** `creator=dik → 12` reproduces the 2026-10-08 value exactly, which
strongly suggests the filter semantics are unchanged. `sort=title` is
alphabetical (`!` sorts first), not by likes/date.

Tag facets are equally anonymous and filter-scoped:

```
GET …?cmd=tags&cat=games&prefixes[]=7
→ 200, {"data":[{"tag_id":2214,"count":173},{"tag_id":1507,"count":3686},
        {"tag_id":2265,"count":1371}, …]}
```

**EVIDENCE.** Unfiltered `ai cg` (2265) was 2 793 in the 2026-10-08 doc; scoped
to Ren'Py it is 1 371. Facets are genuinely scoped to the active filter set.

**No CSRF / no session cookie is issued:**

```
grep -i '^set-cookie' on every cmd=list / cmd=tags / cmd=rss response → nothing
```

**EVIDENCE.** There is no token to acquire before `cmd=list|tags|rss`.

`cmd=rss&cat=games` returns **30** `<item>`s with the stable five-field shape:

```xml
<item><title>[UPDATE] Confluxia [v0.1]</title>
  <description><![CDATA[<img src="https://preview.f95zone.to/2026/11/6690368_Banner.jpg"/>]]></description>
  <dc:creator>VellumVenora</dc:creator>
  <author>VellumVenora &lt;rss@f95&gt;</author>
  <link>https://f95zone.to/threads/321984</link>
  <guid>https://f95zone.to/threads/321984</guid>
  <pubDate>Sat, 07 Nov 2026 13:02:48 +0000</pubDate></item>
```

**EVIDENCE.** RSS is the cheapest possible "what changed" poll: one request,
no HTML, `[NEW]`/`[UPDATE]` in the title, thread ID in `<link>`.

### 2.4 `cmd=options` — anonymous POST fails, no token prompt

```
POST /sam/latest_alpha/latest_data.php
Content-Type: application/x-www-form-urlencoded
X-Requested-With: XMLHttpRequest
body: cmd=options

HTTP/2 500   (53 bytes)
An unexpected error occurred. Please try again later.
```

**EVIDENCE.** With no session the handler dies before it can ask for a CSRF
token; there is no `400 Security error occurred` (the XenForo CSRF failure
shape moxie sees on `/search/search`). **INFERENCE:** with a session it will
most likely require `_xfToken`, but that was not testable anonymously. It
persists per-user UI options and is irrelevant to moxie either way.

### 2.5 Thread pages are anonymous-readable — but links are not

```
GET /threads/321921            → 301 https://f95zone.to/threads/late-checkout-ch-1-kivi-games.321921/
GET <canonical slug>           → 200, 228 898 bytes
<title>Ren&#039;Py - Late Checkout [Ch. 1] [Kivi Games] | F95zone …</title>
```

**EVIDENCE.** A normal browser UA from this IP was **not** Cloudflare-challenged
on any GET today. The page contains:

- `h1.p-title-value` with `span.p-title-prefix` links to
  `/forums/games.2/?prefix_id=7`;
- tag list `a.tagItem.tagItem--tag_ai-cg` href `/tags/ai-cg/`;
- the OP `article.message.message-threadStarterPost` → `.message-content .bbWrapper`;
- the full `Overview:` metadata block, `Thread Updated: 2026-11-07`,
  `Release Date`, `Developer`, `Censored`, `Version: Ch. 1`, `OS`, `Language`;
- a `DiscussionForumPosting` JSON-LD block (see §4).

But for a guest, **downloads, attachments and spoilers are replaced by login
prompts**:

```html
Win/Linux: <div class="messageHide messageHide--link">
  <a href="/login/" data-xf-click="overlay" …>Log in or register to see links</a></div>
```

```html
<div class="messageHide messageHide--attach">
  <a href="/login/">Log in or register to see attachments</a></div>
```

```html
Genre: Spoiler — "You don't have permission to view the spoiler content."
```

**EVIDENCE.** Consequences:

- version/dev/status/tags/dates are scrapable anonymously;
- **download links, save attachments and the developer's off-site links are
  not** — a session is mandatory (this matches moxie's design);
- the OP banner is an attachment, so guests see **no cover image** in the post
  (0 `bbImage` in the OP; only the avatar). Cover must come from
  `latest_data.php`/cache API or a session scrape.

`/threads/<id>/latest` is a **303 to the last post anchor**, and works
anonymously:

```
GET /threads/524/latest
HTTP/2 303
location: https://f95zone.to/threads/game-uploading-rules-2024-02-29.524/#post-21316555
```

**EVIDENCE.** `/threads/<id>/unread` was not tested; XenForo's unread tracking
is user-scoped, so it is expected to be a no-op/login redirect for guests
(**INFERENCE**).

### 2.6 RSS feeds beyond `latest_data.php`

```
GET /forums/games.2/index.rss        → 200, 20 789 B, 19 <item>
```

```xml
<item>
  <title>Project T.H.R.U.S.T. [v0.8.2d] [Pinebrook Games]</title>
  <pubDate>Sat, 07 Nov 2026 17:21:10 +0000</pubDate>
  <link>https://f95zone.to/threads/project-t-h-r-u-s-t-v0-8-2d-pinebrook-games.268976/</link>
  <guid isPermaLink="false">urn:xenforo:thread:268976</guid>
  <author>invalid@example.com (Lerd0)</author>
  <category domain="https://f95zone.to/forums/games.2/">Games</category>
  <dc:creator>Lerd0</dc:creator>
  <content:encoded><![CDATA[<div class="bbWrapper">…Overview:…</div>
    <a href="…" class="link link--internal">Read more</a>]]></content:encoded>
</item>
```

**EVIDENCE.** Per-forum XenForo RSS is anonymous and **includes a chunk of the
OP body** (`content:encoded`), plus the creator and a stable
`urn:xenforo:thread:<id>` guid. This is a *new-thread* stream, whereas
`latest_data.php?cmd=rss` is an *updated-thread* stream. Both are cheap.

### 2.7 `/tags/` — anonymous name vocabulary

```
GET /tags/  → 200, 115 801 B, <title>Search tags | F95zone …</title>
```

```html
<a href="/tags/2dcg/" class="tagCloud-tag tagCloud-tagLevel6"
   title="Used 18,852 times" rel="nofollow">2dcg</a>
<a href="/tags/ai-cg/" class="tagCloud-tag tagCloud-tagLevel3"
   title="Used 3,337 times" rel="nofollow">ai cg</a>
```

**EVIDENCE.** 113 tags on the page, each with a slug, a display name and a
site-wide usage count. It does **not** expose numeric tag IDs — those only
come from `latest_data.php` rows or the gated taxonomy global. But it is a
zero-auth source of *names* for slug/name resolution, and each
`/tags/<slug>/` page is a browsable thread list.

### 2.8 Discovery/pagination endpoints

| Endpoint | Evidence |
|---|---|
| `/whats-new/` | `200`, 159 KB, `<h1>What's new</h1>`, a **Trending content** block with 5 threads, plus links to `/whats-new/posts/` and `/whats-new/latest-activity`. **Not** the full "new posts" feed. |
| `/sitemap.xml` | `200`, a `<sitemapindex>` of `sitemap-1.xml` … `sitemap-4.xml`, `lastmod 2026-11-06T06:09:28+00:00`. Children were **not** fetched (budget). |
| `/watched/threads` | `403` + `<title>Log in | F95zone …</title>` — a XenForo permission response, **not** a Cloudflare challenge. |
| `/find-threads/started` | same `403` login page. |

**EVIDENCE.** `/whats-new/` is a *trending* shelf when logged out, not a
complete feed; the real feeds are `/whats-new/posts/` and
`/whats-new/latest-activity`, whose anonymous behaviour was not tested.

### 2.9 Robots / crawler rules (compliance)

```
GET /robots.txt → 200, 4 796 B
```

Key clauses **as served today**:

```
User-agent: *
Content-Signal: search=yes,ai-train=no,use=reference
Allow: /

# … large list of AI/training crawlers: Disallow: /

User-agent: *
Disallow: /account/
Disallow: /attachments/
Disallow: /goto/
Disallow: /misc/language
Disallow: /misc/style
Disallow: /posts/
Disallow: /login/
Disallow: /search/
Disallow: /whats-new/
Disallow: /admin.php
# Forum lists with filters or sorting (prefix_id, order, direction...): endless combinations
Disallow: /forums/*?
Disallow: /index.php?sam-item/
Disallow: /sam-item/
DisallowAITraining: /
Content-Usage: ai=n
Allow: /

Sitemap: https://f95zone.to/sitemap.xml
```

**EVIDENCE.** Things moxie currently does that robots disallows **for
crawlers**:

- `POST /search/search` → `/search/` is disallowed;
- `/attachments/…` downloads → disallowed (though signed user downloads are
  not "crawling");
- `/forums/games.2/?prefix_id=7` → `/forums/*?` disallowed;
- `/misc/language`, `/misc/style` → disallowed (moxie does not use them).

**Compliance note (not legal advice):** moxie acts for a logged-in user, not as
a search-engine crawler, but the `/search/` and `/attachments/` disallows mean
an automated bulk crawler posture is explicitly unwelcome. Keep the existing
politeness budget; do not turn the sitemap into a bulk crawler without a human
decision.

---

## 3. Auth, limits, anti-bot

### 3.1 Evidence-based auth matrix

| Surface | Anonymous | Session (`xf_user`) | Cloudflare clearance |
|---|---|---|---|
| `latest_data.php` `cmd=list/tags/rss` | **yes** | optional (lifts quota) | not needed today |
| `latest_data.php` `cmd=options` | 500 | required (likely + `_xfToken`) | — |
| `checker.php` | yes | optional | not needed |
| `api.f95checker.dev` | yes | cookie lifts cap | n/a |
| Thread page metadata | **yes** | same | not needed today |
| Thread download links / attachments / spoilers | **no** (`messageHide`) | required | yes in practice |
| `/sam/latest_alpha/` taxonomy | no (200 login wall) | required | yes |
| `/search/search` POST | no | required + `_xfToken` | yes |
| `/watched/threads`, `/find-threads/*` | no (403 login page) | required | yes |
| `/tags/`, `/sitemap.xml`, `/whats-new/` index, forum RSS | yes | same | not needed |
| `/api/` | no | key (`XF-Api-Key`) | — |

### 3.2 Rate limits — what was observed

- 6 anonymous `latest_data.php` requests in ≈20 s (mixed `cmd=list/tags/rss`)
  all returned **200**, no `429`, no `Retry-After`, no `Set-Cookie`, no
  "limited amount of requests" message.
- 1 anonymous `POST cmd=options` returned **500**, not a quota error.
- The previously documented anonymous cap text is:
  *"Anonymous users have a limited amount of requests per hour"* (quoted in
  `latest-data-api.md`; not re-triggered deliberately today).
- `/sam/checker.php?threads=524,1024` returned **404**
  `{"status":"error","msg":"Thread not found"}` because both IDs are
  untracked — this reproduces the documented "all-untracked chunk" rule.

**INFERENCE:** the cap is a per-IP hourly budget enforced inside the handler,
not a reverse-proxy rate limiter, because it returns a JSON `status:error`
body rather than 429/Retry-After. The exact threshold remains unknown; do not
probe it.

### 3.3 Cloudflare reality today

- No probe returned `cf-mitigated`, a challenge body, or a 403/503 CF page.
- `403` responses on `/watched/threads` and `/find-threads/started` carried a
  **XenForo login page**, not `Just a moment…`, `_cf_chl_opt`, or
  `cf-challenge-running`.
- The UA used was a coherent Firefox 141 navigation profile
  (`Accept`, `Accept-Language`, `Sec-Fetch-*`).

**INFERENCE:** Cloudflare is in front of the origin (`server: cloudflare`,
`cf-ray` present) but is not challenging ordinary anonymous GETs from this IP
with a normal browser UA. Blocks are adaptive/rate-driven, consistent with
`research-f95zone-recaptcha.md`. The UA↔`cf_clearance` binding remains real for
session paths.

---

## 4. Thread-page structured data reference (current XenForo 2.3 markup)

### 4.1 JSON-LD — exact shape (observed 2/2 threads)

One `<script type="application/ld+json">` per page:

```json
{
  "@context": "https://schema.org",
  "@type": "WebPage",
  "url": "https://f95zone.to/threads/late-checkout-ch-1-kivi-games.321921/",
  "mainEntity": {
    "@type": "DiscussionForumPosting",
    "@id": "https://f95zone.to/threads/late-checkout-ch-1-kivi-games.321921/",
    "headline": "Late Checkout [Ch. 1] [Kivi Games]",
    "datePublished": "2026-11-07T01:59:26+00:00",
    "keywords": "2dcg, ai cg, cheating, corruption, male protagonist, mobile game, multiple endings, netorare, oral sex, romance, voyeurism",
    "url": "https://f95zone.to/threads/late-checkout-ch-1-kivi-games.321921/",
    "articleSection": "Games",
    "author": { "@type": "Person",
                "@id": "https://f95zone.to/members/kivigames.12086222/",
                "name": "kivigames",
                "url": "https://f95zone.to/members/kivigames.12086222/" },
    "interactionStatistic": [
      { "@type": "InteractionCounter",
        "interactionType": "https://schema.org/ViewAction",
        "userInteractionCount": 7186 },
      { "@type": "InteractionCounter",
        "interactionType": "https://schema.org/CommentAction",
        "userInteractionCount": 18 },
      { "@type": "InteractionCounter",
        "interactionType": "https://schema.org/LikeAction",
        "userInteractionCount": 164 }
    ],
    "dateModified": "2026-11-07T12:16:08+00:00",
    "text": "You play as Sam, a programmer … (full OP plain text)"
  }
}
```

**EVIDENCE.** Fields present: `url`, `mainEntity.@id`, `headline`,
`datePublished`, `dateModified`, `keywords` (**comma-separated tag names**),
`articleSection` (`Games`), `author`, `interactionStatistic` (views, comments,
likes), `text` (entire OP as plain text).

**Not present: `image`.** Zero `image` keys in either probed thread. moxie's
JSON-LD cover fallback (`ld["image"]`) therefore appears **dead on current
markup** — the cover is an attachment and never enters JSON-LD. `og:image` is
also not the cover; it is the site logo:

```html
<meta property="og:image" content="https://f95zone.to/assets/images/logo-sqw192.png" />
```

### 4.2 Head / meta

```
og:url, og:site_name, og:type=website, og:title, twitter:title,
og:image (site logo), twitter:image (site logo), twitter:card=summary
```

**EVIDENCE.** No `description`/`keywords` meta tags worth parsing; the useful
keywords are inside JSON-LD.

### 4.3 Title, prefixes, tags

```html
<h1 class="p-title-value">
  <span class="p-title-prefix">
    <a href="/forums/games.2/?prefix_id=7" class="labelLink" rel="nofollow">
      <span class="label pre-renpy" dir="auto">Ren&#039;Py</span></a>
    <span class="label-append">&nbsp;</span>
  </span>Late Checkout [Ch. 1] [Kivi Games]
</h1>
```

```html
<span class="js-tagList">
  <a href="/tags/2dcg/" class="tagItem tagItem--tag_2dcg" dir="auto">2dcg</a>
  <a href="/tags/ai-cg/" class="tagItem tagItem--tag_ai-cg" dir="auto">ai cg</a>
  …
</span>
```

**EVIDENCE.** Prefix labels carry the **`prefix_id` in the href** — an
anonymous thread page therefore leaks the ID→name pair for every prefix on
that thread (`prefix_id=7` ↔ Ren'Py). Tag anchors carry the slug in
`tagItem--tag_<slug>` but **not** the numeric tag ID.

### 4.4 OP structure and metadata block

- Container: `article.message.message-threadStarterPost` →
  `.message-content.js-messageContent` → `.bbWrapper`.
- Attribution is now **`.message-attribution`** (XF 2.3) — the older
  `.messageMeta` class in moxie notes is **not** what the site emits. Visible
  text: `Today at 1:59 AM #1`.
- Post permalink anchor: `#post-<id>` (e.g. `#post-21316555`).
- Absolute timestamps come from `<time class="u-dt" datetime="…"
  data-timestamp="…">`; relative text alone is ambiguous.
- Metadata block as rendered for a guest:

```
Overview:
You play as Sam, …
Thread Updated: 2026-11-07
Release Date: 2026-11-07
Developer: Kivi Games - [Log in or register to see links]
Censored: No
Version: Ch. 1
OS: Windows, Linux, Mac, Android
Language: English, Turkish, Spanish, Portuguese, French, German, Russian, Arabic, Japanese, Chinese
Genre: [spoiler — login required]
Installation: [spoiler — login required]
Changelog: [spoiler — login required]
Developer Notes: [spoiler — login required]
DOWNLOAD
Win/Linux: [login to see links]
Mac: [login to see links]
Android: [login to see links]
```

**EVIDENCE.** `Thread Updated:` is the author-maintained update date; JSON-LD
`dateModified` was `2026-11-07T12:16:08+00:00` while `datePublished` was
`01:59:26` — a **moderator edit** bumped `dateModified`. Both are available.

### 4.5 BBCode rendering conventions (observed)

| Source | Rendered |
|---|---|
| `[B]…[/B]` | `<b>…</b>` |
| `[SIZE=22px]…[/SIZE]` | `<span style="font-size: 22px">…</span>` |
| `[SPOILER]…[/SPOILER]` | spoiler block, login-gated content for guests |
| `[CODE]…[/CODE]` | `<pre class="bbCodeBlock …">` (**inference** from XenForo convention; not present on the probed threads) |
| `[IMG]…[/IMG]` / attached image | `<img class="bbImage" data-src=…>`; attachment images are **hidden for guests** |

**EVIDENCE for B/SIZE/SPOILER/IMG**, **INFERENCE for CODE**. The "DOWNLOAD"
heading is `<b><span style="font-size: 22px">DOWNLOAD</span></b>`, so a
bold/h2–h5 heading walk still finds it (the text is inside `<b>`).

### 4.6 Download-link reality for a guest

No `/dl/` or `/masked/` anchors exist in an anonymous thread page: every row is
a `.messageHide--link` login prompt. `/dl/` link format (`/dl/<postID>/?u&k&r&t`,
the hidden `u-hidden` duplicate, expiry) can only be re-verified with a
session — see `scraper.md` §Download-link format, still valid from 2026-10-24.

---

## 5. Gap analysis vs moxie

| Endpoint | Verdict |
|---|---|
| `latest_data.php?cmd=list` | **uses partially** — sends only `cat`, `sort∈{date,likes,views,rating}`, `rows=30`, `search`, `page`; omits `prefixes[]`, `noprefixes[]`, `tags[]`, `notags[]`, `tagtype`, `date`, `creator`, `ignored`, `sort=title`, `rows∈{15,45,60,75,90}` |
| `latest_data.php?cmd=tags` | **unused, valuable** — counted facets, anonymous |
| `latest_data.php?cmd=rss` | **unused in code** — `latest_data-api.md` documents it, `latestapi.go` has no caller |
| `latest_data.php?cmd=options` | **not useful** (site UI state) |
| `checker.php` | **uses** |
| Thread page HTML | **uses**, but only with a session for links; misses JSON-LD `keywords` / `text` / `interactionStatistic` |
| `/threads/<id>/latest` | **unused, valuable** — "jump to last post" without scraping the OP |
| `/forums/<id>/index.rss` | **unused, valuable** — new-thread stream + OP excerpt, anonymous |
| `/tags/` | **unused, valuable** — anonymous tag name/slug/count vocabulary |
| `/sitemap.xml` + children | **unused** — bulk thread URL discovery; high crawl cost, robots-allowed |
| `/whats-new/` | **unused**; robots-disallowed — do not add as a crawler source |
| `/api/` | **unusable anon / key-gated** |
| `/watched/threads` | **unusable anon**; session-only watch-list import |
| `/find-threads/started` | **unusable anon**; session-only |
| `/sam/latest_alpha/` taxonomy | **unused**; session-only (200 login wall) — `/tags/` is the anonymous partial substitute |
| `latest.min.js` | **unused / not needed** — confirms no hidden endpoints |
| `robots.txt` | **unused** — not documented in moxie docs; compliance gap |

---

## 6. What this unlocks, per surface

### 6.1 `latest_data.php` server-side filters (anonymous)

**CLI**
- `moxie browse --prefix renpy --tag "ai cg" --days 30 --sort title` — real
  server-side filtering, authoritative counts, no client-side "N of M".
- `moxie sync` association search becomes cheaper: send `prefixes[]` +
  `date` + `creator` to shrink candidate sets before scoring.
- `moxie browse --creator <name>` mirrors the site's CREATOR/TITLE field,
  which `search=` does not.

**Desktop (Svelte)**
- Browse feed toolbar: engine include/exclude (prefix IDs), tag include/exclude,
  date presets, creator box, "Name" sort. Replace `passesFilters` /
  "N of M loaded · hidden by filters" with `msg.count` and `pagination.total`.
- Detail pane: refresh a single thread with the same filters to show
  "similar games" (same tags/prefixes, recent).

### 6.2 `cmd=tags` facets (anonymous)

**CLI**
- `moxie browse --list-tags --prefix renpy` prints counted tags for the
  current filter, so users can discover valid tag names before filtering.

**Desktop**
- A counted tag picker that updates as filters change (counts scoped to the
  active subset), instead of a static list.

### 6.3 `cmd=rss` (anonymous, 30 items)

**CLI**
- `moxie update --watch` can poll one RSS request instead of paging the JSON
  feed: `[NEW]`/`[UPDATE]` + thread ID + timestamp, then run the normal
  version check only for IDs present.
- `moxie sync --rss` as a cheap "what changed since last run" pre-pass.

**Desktop**
- Update queue: seed from one RSS fetch on app start ("3 games in your library
  updated") without touching thread HTML.
- Browse: a lightweight "Recently updated" ticker backed by the same cache.

### 6.4 `/forums/games.2/index.rss` (anonymous)

**CLI**
- `moxie scrape --new` discovers brand-new game threads (title + creator + OP
  excerpt) and can associate them even when the local library has no match yet.

**Desktop**
- Discover → "New threads" tab: a true chronology feed, distinct from
  `latest_data.php`'s update feed; cards get title, creator, cover from the
  forum feed's OP excerpt/`content:encoded`.

### 6.5 `/tags/` (anonymous)

**CLI**
- `moxie tags` / tag autocomplete sourced without a session; map slugs to
  display names for `cmd=tags` facet IDs (ID→name still needs one thread row
  per ID, or the session taxonomy).

**Desktop**
- Tag chips render readable names immediately on first run (before login),
  instead of raw IDs like `2265`.
- Tag browse: `/tags/<slug>/` pages are a ready thread list for a "tag" view.

### 6.6 `/threads/<id>/latest` (anonymous)

**CLI**
- `moxie browse <id> --latest` opens the last post; useful for "what's the
  current changelog/discussion" without paging.
- `moxie scrape --comments` could follow the 303 target's `#post-<id>` to read
  only the newest post.

**Desktop**
- Detail pane: "Last activity" link/button that jumps to the newest post.
- Update queue: after a version bump, deep-link the user to the exact post
  that announced it.

### 6.7 Sitemap (anonymous, robots-allowed)

**CLI**
- A one-shot `moxie catalog import --sitemap` backfill of thread URLs for a
  cold library (huge; must be rate-limited and opt-in).
- INFERENCE: children are `sitemap-1..4.xml`; contents not fetched.

**Desktop**
- "Rebuild catalog" maintenance action with a progress bar, explicitly
  user-initiated (not a background crawler).

### 6.8 Session-only surfaces (future, if login is already present)

- `/watched/threads`: import the user's F95 watch list as the moxie library
  seed — CLI `moxie import --watched`, desktop "Import from F95Zone".
- `/find-threads/started`: "games I engaged with" signal for the detail pane.
- `/sam/latest_alpha/`: authoritative prefix/tag names for both surfaces.
- Thread pages with session: downloads, saves, spoilers, developer links.

### 6.9 Unusable

- `/api/`: no key. If a user ever supplies `XF-Api-Key`, typed thread/forum
  JSON would replace HTML parsing — but do not plan around it.
- `/whats-new/`: robots-disallowed; only a user-clicked "open in browser"
  action, never a crawler.

---

## 7. Open questions that need a logged-in session

1. **`/sam/latest_alpha/` taxonomy global** — exact `latestUpdates` shape and
   whether it still carries all 153 tag IDs + names. Requires a session
   (`xf_user`) and ideally `cf_clearance`. Anonymous probe: 200 + 87-byte wall.
2. **`cmd=options` POST** — does it need `_xfToken`, and what is the payload?
   Anonymous POST returned 500 before any token check.
3. **`/threads/<id>/unread`** — expected 303 to first unread for a session;
   untested.
4. **`/watched/threads` and `/find-threads/started` with session** — item
   shape, pagination, whether watch lists expose thread IDs cleanly.
5. **`/whats-new/posts/` and `/whats-new/latest-activity`** — anonymous
   behaviour and whether they are richer than the logged-out trending index
   (robots still disallows them).
6. **Download-link markup with session** — re-confirm `/dl/<postID>/?u&k&r&t`,
   the hidden `u-hidden` duplicate, and `.messageHide` reveal; also whether
   attachments (`/attachments/`) are signed per session.
7. **Anonymous `latest_data.php` quota** — exact threshold, HTTP status and
   whether a JSON error or `Retry-After` appears. Do not probe deliberately;
   capture it if it happens naturally.
8. **`/api/` with a real key** — does F95Zone issue keys to donors/registered
   users at all? Unknown; endpoint is live and key-gated.
9. **Sitemap children** — `sitemap-1..4.xml` contents and coverage (all
   threads vs recent only).
10. **Other categories anonymously** — `cat=comics|assets|animations` were
    confirmed in the 2026-10-08 doc; not re-tested today, but no reason to
    doubt them.

A logged-in probe needs: an `xf_user` + `xf_session` + `xf_csrf` cookie jar and
a `cf_clearance` minted under the **same** UA (moxie's `f95session.Refresh`
already does this). Store the UA alongside the cookies or every request will
403.

---

## 8. Corrections needed to existing docs

1. **`latest-data-api.md` §Auth** — "Whether the filter params work
   anonymously was not confirmed." **CORRECTED:** `prefixes[]`, `tags[]`,
   `date`, `creator`, `sort=title`, `rows` all work anonymously; no CSRF
   token, no `Set-Cookie`, no session. This is the highest-value correction.
2. **`latest-data-api.md` §`cmd=rss`** — add: it returns **30 items** and
   inherits the full `cmd=list` query (the site derives it with
   `url.replace(/cmd=list/,"cmd=rss")`), so filters/sorts apply to RSS too.
3. **`latest-data-api.md` §Auth** — add that `/sam/latest_alpha/` returns
   **HTTP 200** with an 87-byte `<pre>Sorry, you have to be logged in…</pre>`;
   status-code-only checks will misparse it. The JS asset is public but
   contains no taxonomy.
4. **`scraper.md` §Data sources considered and rejected** — XenForo REST API:
   the endpoint **is enabled** and returns `no_api_key_in_request`; the
   conclusion (no third-party key) stands, but the mechanism is now evidenced.
5. **`scraper.md` / task brief §Thread parsing** — `.messageMeta` is obsolete;
   current XF 2.3 markup is `.message-attribution` (and
   `.message-attribution-main`). Absolute dates live in
   `<time class="u-dt" datetime=…>`; JSON-LD `dateModified` is the reliable
   post-edit timestamp.
6. **`scraper.md` §JSON-LD** — the `image` fallback is likely dead: 2/2 probed
   threads had **no** `image` key (`og:image` is the site logo). JSON-LD now
   carries `keywords` (tag names), `text` (full OP), `interactionStatistic`
   (views/comments/likes) and `articleSection` — all unused by moxie.
7. **`scraper.md` §Thread Parsing** — add the guest gating: anonymous thread
   GETs return metadata but `.messageHide` replaces download links,
   attachments and spoilers; the OP banner is an attachment, so guests see no
   cover. Version/developer/date/tags remain anonymous.
8. **No doc currently records `robots.txt`.** Add the disallow list
   (`/search/`, `/attachments/`, `/posts/`, `/whats-new/`, `/login/`,
   `/account/`, `/goto/`, `/forums/*?`, `/misc/*`, `/sam-item/`) and the
   sitemap declaration. `/search/` and `/attachments/` are directly relevant
   to moxie's search fallback and save downloads; flag for a human
   compliance decision.
9. **`scraper.md` §Cookie-Free Endpoints** — add `/forums/<id>/index.rss` and
   `/tags/` to the anonymous table; both are cheap and currently unused.
10. **`latest-data-api.md` §`cmd=options`** — record that an anonymous POST
    returns **500** `An unexpected error occurred…` (not a CSRF 400), i.e. it
    requires a session before token validation.

---

## 9. Bottom line

- The **only** anonymous JSON API worth adding is the one we already have:
  `latest_data.php`. Its filter/sort language is fully anonymous — no cookie,
  no token — so the planned "server-side filters" work is unblocked.
- The **cheapest new signals** are RSS: `cmd=rss` (updated threads, 30 items)
  and `/forums/games.2/index.rss` (new threads + OP excerpt), both anonymous.
- **`/tags/`** solves tag *names* without a session; numeric IDs still need a
  thread row or the gated taxonomy.
- **Thread pages are anonymously readable for metadata but not for links** —
  which is exactly the split moxie already assumes, now evidenced.
- **`/api/` exists but is key-gated**; do not build on it.
- The **anti-bot wall did not appear** for 25 polite anonymous requests today.
  That does not mean it is gone; it means the block is adaptive, so the
  existing pacer and circuit breaker stay.
