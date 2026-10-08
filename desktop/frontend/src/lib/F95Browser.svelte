<script>
  import {onMount, onDestroy} from 'svelte'
  import {
    GetCookieStatus,
    SearchF95Zone,
    GetThreadPreview,
    AddGameFromF95Zone,
    BrowseF95Zone,
  } from '../../wailsjs/go/main/App'
  import {engineColor, engineStyle} from './engineColors.js'
  import {safeExternalUrl} from './sanitizeUrl.js'
  import {formatCount} from './format.js'
  import {createVirtualList} from './virtualList.svelte.js'
  import {browser, setBrowseFilters} from './viewState.svelte.js'

  // ── State ──────────────────────────────────────────────────
  // Query/results/preview state lives in viewState so the browser tab keeps
  // its search (and its preview pane) when the user navigates away and back.
  // The seq counters are module-level too: an in-flight response from a
  // destroyed instance must not be able to overwrite a remounted view — with
  // shared counters, only a NEWER request supersedes an older one, exactly as
  // if the view had never unmounted.

  let {
    onAdded = () => {},
  } = $props()

  // Cookie status
  let cookieStatus = $state('')       // 'available' | 'not_found' | 'error' | ''
  let cookieError = $state('')        // human-readable failure for the error state
  let cookieChecking = $state(false)

  // Add-to-library in-flight lives in viewState (browser.addingInFlight) so
  // a remount mid-add keeps the button disabled — a second click before the
  // first AddGameFromF95Zone settles would double-add the game.

  // ── Cookie check on mount ──────────────────────────────────
  // Search depends on the cookie status, so the check must never hang the UI:
  // race the binding against a 20s timeout (same pattern as
  // DownloadsView.loadData) and surface a timeout/rejection as a visible
  // error state with a Retry button instead of leaving the Search button
  // silently disabled forever.
  async function checkCookieStatus() {
    if (cookieChecking) return
    cookieChecking = true
    cookieError = ''
    let timer
    const timeout = new Promise((_, reject) => {
      timer = setTimeout(() => {
        reject(new Error('Cookie check timed out after 20s'))
      }, 20000)
    })
    try {
      cookieStatus = await Promise.race([GetCookieStatus(), timeout])
    } catch (e) {
      console.error('Failed to get cookie status:', e)
      cookieStatus = 'error'
      cookieError = /timed out/i.test(String(e))
        ? 'The cookie check timed out after 20s — the app may be unresponsive. Try again.'
        : String(e).replace(/^Error:\s*/, '')
    } finally {
      clearTimeout(timer)
      cookieChecking = false
    }
  }

  onMount(async () => {
    // Clear inherited spinner flags only when the request they belong to has
    // settled (or never existed). A search/preview started before navigation
    // is still current after remount (the seq counters are shared), so its
    // spinner must keep spinning until the backend responds — only a settled
    // request leaves searchInFlight/previewInFlight false.
    if (!browser.searchInFlight) browser.loading = false
    if (!browser.previewInFlight && !browser.preview) browser.previewing = false
    // Search is explicit-only (Search button / Enter) — never auto-triggered
    // on mount, so a persisted query from a previous visit stays as-is until
    // the user asks for a new search. With no persisted search, the Discover
    // feed fills the surface.
    if (!browser.searched && discoverStale()) loadDiscover(true)
    await checkCookieStatus()
  })

  // ── Discover feed (default surface, no search) ─────────────
  // F95Zone's latest-updates index, cookie-free. The sort tabs pick the
  // ranking; infinite scroll (plus a visible Load more fallback) walks pages.
  const DISCOVER_SORTS = [
    {key: 'date', label: 'Latest'},
    {key: 'likes', label: 'Popular'},
    {key: 'views', label: 'Most Viewed'},
    {key: 'rating', label: 'Top Rated'},
  ]
  const DISCOVER_STALE_MS = 10 * 60 * 1000   // refresh the feed on return after this long

  let sentinelEl = $state(null)

  function discoverStale() {
    const d = browser.discover
    return !d.loadedAt || Date.now() - d.loadedAt > DISCOVER_STALE_MS
  }

  async function loadDiscover(reset) {
    const d = browser.discover
    if (d.loading || d.loadingMore) return
    // Append loads require an existing page and pages left to fetch; the
    // first page is always an explicit reset.
    if (!reset && (d.items.length === 0 || (d.totalPages > 0 && d.page >= d.totalPages))) return

    const page = reset ? 1 : d.page + 1
    const seq = ++d.seq
    if (reset) d.loading = true
    else d.loadingMore = true
    d.error = ''

    try {
      const res = await BrowseF95Zone(page, d.sort)
      if (seq !== d.seq) return        // stale — a newer load/sort owns the feed
      d.items = reset ? res.results : [...d.items, ...res.results]
      d.page = res.page
      d.totalPages = res.totalPages
      d.totalCount = res.totalCount
      d.loadedAt = Date.now()
    } catch (e) {
      if (seq !== d.seq) return
      // A concurrent search/sync holds the single network slot; that's not a
      // feed failure — keep whatever is already loaded.
      if (!/another network request/i.test(String(e))) d.error = String(e)
    } finally {
      if (seq === d.seq) {
        d.loading = false
        d.loadingMore = false
      }
    }
  }

  // The sort control drives both surfaces. On the Discover feed it's a server
  // sort, so switching reloads from page 1 (invalidating any in-flight page).
  // On search results it only re-orders the already-fetched set client-side, so
  // no network request is made while a search holds the single network slot.
  function setSort(sort) {
    const d = browser.discover
    if (d.sort === sort) return
    d.sort = sort
    if (browser.searched) return
    // Invalidate any in-flight response for the old sort and reset to page 1.
    d.seq++
    d.items = []
    d.page = 0
    d.totalPages = 0
    d.totalCount = 0
    d.loading = false
    d.loadingMore = false
    d.error = ''
    loadDiscover(true)
  }

  // Infinite scroll: fetch the next page when the sentinel nears the
  // viewport. Re-created when the sentinel mounts/unmounts (search mode hides
  // it); loadDiscover guards against duplicate or past-the-end fetches.
  $effect(() => {
    const el = sentinelEl
    if (!el) return
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && !browser.searched) loadDiscover(false)
    }, {rootMargin: '400px'})
    io.observe(el)
    return () => io.disconnect()
  })

  // ── Discover grid virtualization ────────────────────────────
  // The feed is infinite-scroll, so without windowing every loaded cover card
  // stays in the DOM and decoded in memory — a few pages in, the webview janks
  // and RSS climbs by hundreds of MB. Mirrors GameList's grid: chunk the flat
  // result list into fixed rows and render only the visible range (+overscan)
  // over a spacer sized to the full content height.
  const DISCOVER_GAP = 10
  const DISCOVER_CARD_MIN = 220
  // Caption block under the cover. Must equal .discover-card .result-info's
  // fixed height in CSS (change one, change both) so every row is uniform.
  const DISCOVER_TEXT_H = 68

  let discoverEl = $state(null)
  let discoverColumns = $state(1)
  let discoverRowHeight = $state(300)

  function updateDiscoverLayout() {
    if (!discoverEl) return
    const avail = Math.max(discoverEl.clientWidth, DISCOVER_CARD_MIN)
    const cols = Math.max(1, Math.floor((avail + DISCOVER_GAP) / (DISCOVER_CARD_MIN + DISCOVER_GAP)))
    const cardWidth = (avail - DISCOVER_GAP * (cols - 1)) / cols
    discoverColumns = cols
    discoverRowHeight = cardWidth * 9 / 16 + DISCOVER_TEXT_H + DISCOVER_GAP
  }

  $effect(() => {
    if (!discoverEl) return
    updateDiscoverLayout()
    const ro = new ResizeObserver(() => updateDiscoverLayout())
    ro.observe(discoverEl)
    return () => ro.disconnect()
  })

  // ── Filters (engine include/exclude + hide AI CG) ──────────
  // Applied client-side to both surfaces. The Discover feed carries `engine`
  // (derived from the feed's prefix IDs) and `isAICG`; search results are
  // enriched with the same fields by the backend. Items whose engine is unknown
  // (`engine === ''`) are dropped only when an engine filter is active — their
  // membership can't be confirmed. AI-CG items are dropped only when the toggle
  // is on.
  const BROWSE_ENGINES = [
    'RenPy', 'Unity', 'UnrealEngine', 'RPGM', 'HTML', 'Godot', 'WebGL',
    'WolfRPG', 'Java', 'Flash', 'QSP', 'RAGS', 'ADRIFT', 'Tads', 'Others',
  ]

  function passesFilters(item) {
    const f = browser.filters
    if (f.hideAICG && item.isAICG) return false
    if (f.engines.length > 0) {
      const known = !!item.engine
      const listed = f.engines.includes(item.engine)
      if (f.engineMode === 'include' ? (!known || !listed) : (known && listed)) return false
    }
    return true
  }

  let filteredDiscoverItems = $derived.by(() => browser.discover.items.filter(passesFilters))

  // Sort the (small) search result set client-side to match the active sort;
  // 'date' keeps the search engine's relevance order (XenForo results carry no
  // timestamp).
  function sortSearchResults(list) {
    const sort = browser.discover.sort
    const by = sort === 'likes' ? (r) => r.likes || 0
      : sort === 'views' ? (r) => r.views || 0
        : sort === 'rating' ? (r) => r.rating || 0
          : null
    return by ? [...list].sort((a, b) => by(b) - by(a)) : list
  }

  let filteredResults = $derived.by(() => sortSearchResults(browser.results.filter(passesFilters)))

  // Engine options: the canonical list plus any engine the loaded data uses
  // that isn't canonical (future-proofing for new F95Zone engines).
  let engineFilterOptions = $derived.by(() => {
    const present = new Set(BROWSE_ENGINES)
    for (const it of browser.discover.items) if (it.engine) present.add(it.engine)
    for (const it of browser.results) if (it.engine) present.add(it.engine)
    return [...present].sort((a, b) => a.localeCompare(b, undefined, {sensitivity: 'base'}))
  })

  let filtersActive = $derived(
    browser.filters.hideAICG || browser.filters.engines.length > 0
  )

  // Engine dropdown open/close with click-outside dismissal.
  let engineFilterEl = $state(null)
  let engineFilterOpen = $state(false)
  $effect(() => {
    if (!engineFilterOpen) return
    const onDown = (e) => {
      if (engineFilterEl && !engineFilterEl.contains(e.target)) engineFilterOpen = false
    }
    document.addEventListener('pointerdown', onDown)
    return () => document.removeEventListener('pointerdown', onDown)
  })

  function toggleEngine(eng) {
    const sel = browser.filters.engines
    const next = sel.includes(eng) ? sel.filter((e) => e !== eng) : [...sel, eng]
    setBrowseFilters({engines: next})
  }

  // Chunk the flat list into rows so the virtualizer only has to window rows.
  let discoverRows = $derived.by(() => {
    const rows = []
    const items = filteredDiscoverItems
    for (let i = 0; i < items.length; i += discoverColumns) {
      rows.push(items.slice(i, i + discoverColumns))
    }
    return rows
  })

  // See virtualList.svelte.js for why this isn't a plain reactive $store read.
  const discoverVirtualizer = createVirtualList({
    count: 0,
    getScrollElement: () => discoverEl,
    estimateSize: () => discoverRowHeight,
    overscan: 3,
  })

  $effect(() => {
    const el = discoverEl
    const rows = discoverRows
    const rowHeight = discoverRowHeight
    discoverVirtualizer.setOptions({
      count: rows.length,
      getScrollElement: () => el,
      estimateSize: () => rowHeight,
      overscan: 3,
      getItemKey: (i) => rows[i]?.map((r) => r.threadId).join(',') ?? i,
    })
  })

  // The subscriber in createVirtualList is process-global per instance; it
  // must be torn down when this view unmounts on a tab switch.
  onDestroy(() => discoverVirtualizer.unsubscribe())

  // ── Derived ─────────────────────────────────────────────────
  let canSearch = $derived(browser.query.trim().length >= 2 && cookieStatus === 'available' && !browser.loading)

  // Engine display colors — imported from engineColors.js

  // ── Search (explicit only) ─────────────────────────────────
  // Clearing the field (or typing a too-short query) must clear results AND
  // invalidate any in-flight request so it can't resurrect stale results.
  // Typing itself never triggers a search — only the Search button or Enter.
  function resetSearchResults() {
    browser.searchSeq++
    browser.previewSeq++
    browser.loading = false
    browser.searchInFlight = false
    browser.previewInFlight = false
    browser.searched = false
    browser.results = []
    browser.error = ''
    browser.selected = null
    browser.preview = null
    browser.previewing = false
    browser.addResult = null
    browser.expandedOverview = false
  }

  function handleSearchInput(e) {
    browser.query = e.target.value

    if (browser.query.trim().length < 2) {
      resetSearchResults()
    }
  }

  async function doSearch() {
    const q = browser.query.trim()
    if (q.length < 2) {
      resetSearchResults()
      return
    }

    const seq = ++browser.searchSeq
    browser.previewSeq++                    // a new search invalidates any in-flight preview
    browser.loading = true
    browser.searchInFlight = true
    browser.error = ''
    browser.searched = true
    browser.results = []
    browser.selected = null
    browser.preview = null
    browser.previewing = false
    browser.previewInFlight = false
    browser.addResult = null
    browser.expandedOverview = false

    try {
      const res = await SearchF95Zone(q)
      if (seq !== browser.searchSeq) return   // stale — a newer search owns the results
      browser.results = res
    } catch (e) {
      if (seq !== browser.searchSeq) return
      browser.error = String(e)
    }
    if (seq === browser.searchSeq) {
      browser.loading = false
      browser.searchInFlight = false
    }
  }

  // ── Preview ────────────────────────────────────────────────
  async function handlePreview(result) {
    browser.selected = result
    browser.previewing = true
    browser.previewInFlight = true
    browser.preview = null
    browser.previewError = ''
    browser.addResult = null
    browser.expandedOverview = false        // don't leak the previous game's expanded state

    const seq = ++browser.previewSeq
    try {
      const p = await GetThreadPreview(result.url)
      if (seq !== browser.previewSeq) return  // stale — a newer preview/search owns the pane
      browser.preview = p
    } catch (e) {
      if (seq !== browser.previewSeq) return
      browser.previewError = String(e)
    }
    if (seq === browser.previewSeq) browser.previewInFlight = false
  }

  function closePreview() {
    browser.previewSeq++               // drop any in-flight preview response
    browser.previewing = false
    browser.previewInFlight = false
    browser.preview = null
    browser.selected = null
    browser.addResult = null
    browser.expandedOverview = false
  }

  // ── Add to Library ─────────────────────────────────────────
  async function handleAddToLibrary() {
    if (!browser.preview || browser.addingInFlight) return

    const seq = browser.previewSeq
    browser.addingInFlight = true
    browser.addResult = null

    try {
      const gameId = await AddGameFromF95Zone(
        browser.selected.url,
        browser.preview.title,
        browser.preview.prefix || '',
      )
      // Drop stale results: if the user navigated away (preview closed / new
      // preview started) mid-add, don't let this response resurrect the pane.
      if (seq === browser.previewSeq) {
        browser.addResult = { success: true, gameId }
        onAdded()
      }
    } catch (e) {
      if (seq === browser.previewSeq) {
        browser.addResult = { success: false, error: String(e) }
      }
    } finally {
      // Cleared unconditionally: the shared flag must not stay set past the
      // backend call settling, or a remounted view would be stuck disabled.
      browser.addingInFlight = false
    }
  }

  // ── Overview truncation ────────────────────────────────────
  function truncate(text, maxLen = 300) {
    if (!text || text.length <= maxLen) return text
    return text.slice(0, maxLen) + '…'
  }

  // ── Engine styles — imported from shared module ───────────────
  // See engineColors.js for the canonical palette matching TUI styles
</script>

<div class="f95-browser">
  <div class="browser-header">
    <h2>F95Zone Browser</h2>
    <p class="browser-subtitle">Search and discover games on F95Zone.</p>
  </div>

  <!-- ── Cookie Status ─────────────────────────────────────── -->
  {#if cookieStatus === 'available'}
    <div class="cookie-status cookie-ok">
      <span class="cookie-icon">✓</span>
      <span class="cookie-text">F95Zone connected</span>
    </div>
  {:else if cookieStatus === 'not_found'}
    <div class="cookie-status cookie-missing">
      <span class="cookie-icon">!</span>
      <div class="cookie-body">
        <p class="cookie-title">Log into F95Zone in your browser first</p>
        <p class="cookie-detail">
          The browser needs your F95Zone session cookies to search and browse.
          Log in at <strong>f95zone.to</strong> in your browser, then restart this app.
        </p>
      </div>
    </div>
  {:else if cookieStatus === 'error'}
    <div class="cookie-status cookie-error">
      <span class="cookie-icon">✕</span>
      <div class="cookie-body">
        <p class="cookie-title">Couldn't check the F95Zone connection</p>
        <p class="cookie-detail">{cookieError}</p>
        <button
          class="cookie-retry"
          onclick={checkCookieStatus}
          disabled={cookieChecking}
        >
          {cookieChecking ? 'Checking…' : 'Retry'}
        </button>
      </div>
    </div>
  {:else}
    <div class="cookie-status cookie-loading">
      <div class="spinner"></div>
      <p class="status-text">Checking cookie status…</p>
    </div>
  {/if}

  <!-- ── Search Bar ────────────────────────────────────────── -->
  <div class="search-bar">
    <input
      type="text"
      class="search-input"
      placeholder="Search F95Zone games… (e.g. 'Summertime Saga')"
      value={browser.query}
      oninput={handleSearchInput}
      onkeydown={(e) => e.key === 'Enter' && doSearch()}
    />
    <button
      class="btn btn-primary"
      onclick={doSearch}
      disabled={!canSearch}
    >
      {#if browser.loading}
        <span class="spinner-small"></span>
      {:else}
        Search
      {/if}
    </button>
  </div>

  <!-- ── Browse toolbar: sort + filters (both surfaces) ────── -->
  {#if browser.searched || browser.discover.items.length > 0}
    <div class="browse-toolbar">
      <div class="discover-tabs">
        {#each DISCOVER_SORTS as s}
          <button
            class="discover-tab"
            class:active={browser.discover.sort === s.key}
            onclick={() => setSort(s.key)}
          >{s.label}</button>
        {/each}
      </div>

      <div class="discover-actions">
        <div class="filter-engine" bind:this={engineFilterEl}>
          <button
            class="filter-btn"
            class:active={browser.filters.engines.length > 0}
            onclick={() => engineFilterOpen = !engineFilterOpen}
            aria-expanded={engineFilterOpen}
          >
            {browser.filters.engineMode === 'include' ? 'Engines' : 'Exclude engines'}
            {#if browser.filters.engines.length}
              <span class="filter-count">{browser.filters.engines.length}</span>
            {/if}
            <span class="filter-caret" class:open={engineFilterOpen}>▾</span>
          </button>
          {#if engineFilterOpen}
            <div class="filter-panel">
              <div class="filter-mode">
                <button
                  class="mode-btn"
                  class:active={browser.filters.engineMode === 'include'}
                  onclick={() => setBrowseFilters({engineMode: 'include'})}
                >Include</button>
                <button
                  class="mode-btn"
                  class:active={browser.filters.engineMode === 'exclude'}
                  onclick={() => setBrowseFilters({engineMode: 'exclude'})}
                >Exclude</button>
              </div>
              <div class="engine-list">
                {#each engineFilterOptions as eng (eng)}
                  <label class="engine-opt">
                    <input
                      type="checkbox"
                      checked={browser.filters.engines.includes(eng)}
                      onchange={() => toggleEngine(eng)}
                    />
                    <span class="engine-opt-name" style={engineStyle(eng)}>{eng}</span>
                  </label>
                {/each}
              </div>
              {#if browser.filters.engines.length > 0}
                <button class="filter-clear" onclick={() => setBrowseFilters({engines: []})}>
                  Clear engines
                </button>
              {/if}
            </div>
          {/if}
        </div>

        <label class="filter-toggle">
          <input
            type="checkbox"
            checked={browser.filters.hideAICG}
            onchange={(e) => setBrowseFilters({hideAICG: e.target.checked})}
          />
          Hide AI CG
        </label>

        {#if !browser.searched && browser.discover.totalCount > 0}
          <span class="discover-count">{browser.discover.totalCount.toLocaleString()} games</span>
        {/if}
        {#if !browser.searched}
          <button
            class="discover-refresh"
            onclick={() => loadDiscover(true)}
            disabled={browser.discover.loading || browser.discover.loadingMore}
            title="Refresh"
            aria-label="Refresh feed"
          >⟳</button>
        {/if}
      </div>
    </div>

    {#if filtersActive}
      <p class="filter-note">
        {#if browser.searched}
          Showing {filteredResults.length} of {browser.results.length}{#if browser.results.length > filteredResults.length} · {browser.results.length - filteredResults.length} hidden by filters{/if}
        {:else}
          {filteredDiscoverItems.length} of {browser.discover.items.length} loaded{#if browser.discover.items.length > filteredDiscoverItems.length} · {browser.discover.items.length - filteredDiscoverItems.length} hidden by filters{/if}
        {/if}
      </p>
    {/if}
  {/if}

  <!-- ── Content Area: Results + Preview ──────────────────── -->
  <div class="browser-content" class:has-preview={browser.previewing}>
    <!-- ── Results Section ──────────────────────────────── -->
    <div class="results-section">
      {#if browser.searched}
        {#if browser.error}
          <div class="error-section">
            <p class="error-title">Search failed:</p>
            <p class="error-line">{browser.error}</p>
          </div>
        {:else if browser.loading}
          <div class="loading-state">
            <div class="spinner-lg"></div>
            <p>Searching F95Zone…</p>
          </div>
        {:else if browser.results.length === 0}
          <div class="empty-state">
            <p class="empty-icon">⌕</p>
            <p class="empty-title">No results found</p>
            <p class="empty-detail">Try a different search term.</p>
          </div>
        {:else if filteredResults.length === 0}
          <div class="empty-state">
            <p class="empty-icon">⌕</p>
            <p class="empty-title">No results match the filters</p>
            <p class="empty-detail">Adjust the engine filter or turn off “Hide AI CG”.</p>
          </div>
        {:else}
          <div class="results-grid">
            {#each filteredResults as result (result.url)}
              <button
                class="result-card"
                class:selected={browser.selected?.url === result.url}
                onclick={() => handlePreview(result)}
              >
                <div class="result-thumb">
                  {#if result.thumbnailUrl}
                    <img src={result.thumbnailUrl} alt={result.title} loading="lazy" decoding="async" />
                  {:else}
                    <div class="result-thumb-placeholder">
                      <span class="placeholder-icon">▭</span>
                    </div>
                  {/if}
                </div>
                <div class="result-info">
                  <span class="result-title" title={result.title}>
                    {result.title}
                  </span>
                  <div class="result-meta">
                    {#if result.prefix}
                      <span
                        class="engine-badge"
                        style={engineStyle(result.prefix)}
                      >
                        {result.prefix}
                      </span>
                    {/if}
                    {#if result.matchScore > 0}
                      <span class="result-match">{result.matchScore}%</span>
                    {/if}
                  </div>
                </div>
              </button>
            {/each}
          </div>
        {/if}
      {:else}
        <!-- ── Discover feed (default surface, no search) ──── -->
        <div class="discover">
          {#if cookieStatus !== 'available'}
            <p class="discover-note">Log into F95Zone in your browser to preview and add games.</p>
          {/if}

          {#if browser.discover.loading && browser.discover.items.length === 0}
            <div class="loading-state">
              <div class="spinner-lg"></div>
              <p>Loading F95Zone…</p>
            </div>
          {:else if browser.discover.error && browser.discover.items.length === 0}
            <div class="error-section">
              <p class="error-title">Couldn't load the feed:</p>
              <p class="error-line">{browser.discover.error}</p>
            </div>
          {:else if browser.discover.items.length === 0}
            <div class="empty-state">
              <p class="empty-icon">⊙</p>
              <p class="empty-title">Nothing to show</p>
              <p class="empty-detail">Try refreshing the feed.</p>
            </div>
          {:else if filteredDiscoverItems.length === 0}
            <div class="empty-state">
              <p class="empty-icon">⌕</p>
              <p class="empty-title">No games match the filters</p>
              <p class="empty-detail">Adjust the engine filter or turn off “Hide AI CG”.</p>
            </div>
          {:else}
            <!-- Windowed feed: only the visible rows (+ overscan) are in the
                 DOM, so infinite scroll can't grow the webview without bound. -->
            <div class="discover-scroll" bind:this={discoverEl}>
              <div class="discover-spacer" style="height: {discoverVirtualizer.totalSize}px">
                {#each discoverVirtualizer.virtualRows as vRow (vRow.key)}
                  <div
                    class="discover-row"
                    style="grid-template-columns: repeat({discoverColumns}, 1fr); height: {discoverRowHeight - DISCOVER_GAP}px; transform: translateY({vRow.start + DISCOVER_GAP}px)"
                  >
                    {#each discoverRows[vRow.index] ?? [] as result (result.threadId)}
                      <button
                        class="result-card discover-card"
                        class:selected={browser.selected?.url === result.url}
                        disabled={cookieStatus !== 'available'}
                        title={cookieStatus === 'available' ? result.title : 'Log into F95Zone to preview'}
                        onclick={() => handlePreview(result)}
                      >
                        <div class="result-thumb">
                          {#if result.coverUrl}
                            <img src={result.coverUrl} alt={result.title} loading="lazy" decoding="async" />
                          {:else}
                            <div class="result-thumb-placeholder">
                              <span class="placeholder-icon">▭</span>
                            </div>
                          {/if}
                        </div>
                        <div class="result-info">
                          <span class="result-title" title={result.title}>{result.title}</span>
                          <div class="result-meta">
                            {#if result.engine}
                              <span class="engine-badge" style={engineStyle(result.engine)}>{result.engine}</span>
                            {/if}
                            {#if result.version}
                              <span class="discover-version">{result.version}</span>
                            {/if}
                            {#if result.rating > 0}
                              <span class="discover-rating">{result.rating.toFixed(2)}/5</span>
                            {/if}
                            {#if result.views > 0}
                              <span class="discover-stat">{formatCount(result.views)} views</span>
                            {/if}
                          </div>
                          <div class="result-meta">
                            {#if result.creator}
                              <span class="discover-creator" title={result.creator}>{result.creator}</span>
                            {/if}
                            {#if result.date}
                              <span class="discover-stat">{result.date}</span>
                            {/if}
                          </div>
                        </div>
                      </button>
                    {/each}
                  </div>
                {/each}
              </div>

              {#if browser.discover.error}
                <div class="error-section">
                  <p class="error-line">{browser.discover.error}</p>
                </div>
              {/if}

              {#if browser.discover.loadingMore}
                <div class="discover-more"><span class="spinner"></span> Loading more…</div>
              {:else if browser.discover.totalPages > 0 && browser.discover.page < browser.discover.totalPages}
                <button class="btn discover-load-more" onclick={() => loadDiscover(false)}>
                  Load more
                </button>
              {:else}
                <p class="discover-end">End of results</p>
              {/if}

              <div class="discover-sentinel" bind:this={sentinelEl}></div>
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- ── Preview Section ───────────────────────────────── -->
    {#if browser.previewing}
      <div class="preview-section">
        <button class="preview-close" onclick={closePreview}>✕</button>

        {#if browser.previewError}
          <div class="error-section">
            <p class="error-title">Preview failed:</p>
            <p class="error-line">{browser.previewError}</p>
          </div>
        {:else if !browser.preview}
          <div class="loading-state">
            <div class="spinner-lg"></div>
            <p>Loading thread preview…</p>
          </div>
        {:else}
          <!-- Cover Art -->
          <div class="preview-cover">
            {#if browser.preview.coverUrl}
              <img src={browser.preview.coverUrl} alt={browser.preview.title} decoding="async" />
            {:else}
              <div class="preview-cover-placeholder">
                <span>▭</span>
                <span>{browser.preview.title}</span>
              </div>
            {/if}
          </div>

          <!-- Title & Meta -->
          <div class="preview-meta">
            <h3 class="preview-title">{browser.preview.title}</h3>
            <div class="preview-badges">
              {#if browser.preview.prefix}
                <span
                  class="engine-badge"
                  style={engineStyle(browser.preview.prefix)}
                >
                  {browser.preview.prefix}
                </span>
              {/if}
              {#if browser.preview.status && browser.preview.status !== 'unknown'}
                <span class="status-badge" class:status-active={browser.preview.status === 'active'}
                  class:status-completed={browser.preview.status === 'completed'}
                  class:status-on-hold={browser.preview.status === 'on_hold'}
                  class:status-abandoned={browser.preview.status === 'abandoned'}>
                  {browser.preview.status}
                </span>
              {/if}
            </div>

            {#if browser.preview.developer}
              <div class="preview-meta-row">
                <span class="preview-meta-label">Developer</span>
                <span class="preview-meta-value">{browser.preview.developer}</span>
              </div>
            {/if}
            {#if browser.preview.version}
              <div class="preview-meta-row">
                <span class="preview-meta-label">Version</span>
                <span class="preview-meta-value">{browser.preview.version}</span>
              </div>
            {/if}

            <!-- Tags -->
            {#if browser.preview.tags?.length > 0}
              <div class="preview-tags">
                {#each browser.preview.tags as tag}
                  <span class="tag">{tag}</span>
                {/each}
              </div>
            {/if}
          </div>

          <!-- Overview -->
          {#if browser.preview.overview}
            <div class="preview-overview">
              <h4>Overview</h4>
              <p>
                {#if browser.expandedOverview || browser.preview.overview.length <= 300}
                  {browser.preview.overview}
                {:else}
                  {truncate(browser.preview.overview)}
                {/if}
              </p>
              {#if browser.preview.overview.length > 300}
                <button class="btn-link" onclick={() => browser.expandedOverview = !browser.expandedOverview}>
                  {browser.expandedOverview ? 'Show less' : 'Show more'}
                </button>
              {/if}
            </div>
          {/if}

          <!-- Store Links -->
          {#if browser.preview.storeLinks && Object.keys(browser.preview.storeLinks).length > 0}
            <div class="preview-stores">
              <h4>Store Links</h4>
              <div class="store-links">
                {#each Object.entries(browser.preview.storeLinks) as [name, url]}
                  {#if safeExternalUrl(url)}
                    <a href={safeExternalUrl(url)} target="_blank" rel="noopener" class="store-link">
                      {#if name === 'steam'}
                        ◈
                      {:else}
                        →
                      {/if}
                      {name}
                    </a>
                  {/if}
                {/each}
              </div>
            </div>
          {/if}

          <!-- Download Links -->
          {#if browser.preview.downloadLinks?.length > 0}
            <div class="preview-downloads">
              <h4>Download Links</h4>
              <div class="download-list">
                {#each browser.preview.downloadLinks as dl}
                  {#if safeExternalUrl(dl.url)}
                    <a href={safeExternalUrl(dl.url)} target="_blank" rel="noopener" class="download-link">
                      <span class="dl-host">{dl.host}</span>
                      <span class="dl-name">{dl.name || 'Link'}</span>
                      {#if dl.platform}
                        <span class="dl-platform">{dl.platform}</span>
                      {/if}
                    </a>
                  {/if}
                {/each}
              </div>
            </div>
          {/if}

          <!-- Add to Library Button -->
          <div class="preview-actions">
            {#if browser.addResult?.success}
              <div class="add-success">
                <span>✓</span>
                <span>Game added to library (ID: {browser.addResult.gameId})</span>
              </div>
            {:else if browser.addResult?.error}
              <div class="add-error">
                <span>✗</span>
                <span>{browser.addResult.error}</span>
              </div>
            {:else}
              <button
                class="btn btn-primary add-btn"
                onclick={handleAddToLibrary}
                disabled={browser.addingInFlight}
              >
                {#if browser.addingInFlight}
                  Adding…
                {:else}
                  + Add to Library
                {/if}
              </button>
            {/if}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .f95-browser {
    flex: 1;
    overflow: auto;
    padding: var(--space-8) var(--gutter);
    width: 100%;
    display: flex;
    flex-direction: column;
  }

  .browser-header {
    margin-bottom: 20px;
  }
  .browser-header h2 {
    font-size: var(--text-2xl);
    font-weight: 700;
    margin: 0 0 4px;
  }
  .browser-subtitle {
    font-size: var(--text-base);
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Cookie Status ─────────────────── */
  .cookie-status {
    margin: 0 0 16px;
    padding: 12px 16px;
    border-radius: var(--radius-1);
    display: flex;
    gap: 10px;
    align-items: flex-start;
  }
  .cookie-icon {
    font-size: var(--text-lg);
    font-weight: 700;
    flex-shrink: 0;
    line-height: 1.4;
  }
  .cookie-body { flex: 1; min-width: 0; }
  .cookie-title { font-size: var(--text-md); font-weight: 600; margin: 0 0 2px; }
  .cookie-detail { font-size: var(--text-sm); color: var(--text-secondary); margin: 0; line-height: 1.5; }
  .cookie-detail strong { color: var(--text-primary); }
  .cookie-text { font-size: var(--text-base); font-weight: 500; }

  .cookie-ok {
    border: 1px solid var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .cookie-ok .cookie-icon,
  .cookie-ok .cookie-text { color: var(--success); }

  .cookie-missing {
    border: 1px solid var(--warning);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
  }
  .cookie-missing .cookie-icon,
  .cookie-missing .cookie-title { color: var(--warning); }

  .cookie-error {
    border: 1px solid var(--danger);
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .cookie-error .cookie-icon,
  .cookie-error .cookie-title { color: var(--danger); }
  .cookie-retry {
    margin-top: 8px;
    padding: 4px 12px;
    border: 1px solid var(--danger);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--danger);
    font-size: var(--text-sm);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.12s;
  }
  .cookie-retry:hover:not(:disabled) {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
  }
  .cookie-retry:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .cookie-loading {
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    align-items: center;
  }
  .cookie-loading .status-text {
    font-size: var(--text-base);
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Spinner ────────────────────────── */
  .spinner {
    width: 16px;
    height: 16px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
    flex-shrink: 0;
  }
  .spinner-small {
    display: inline-block;
    width: 14px;
    height: 14px;
    border: 2px solid rgba(255,255,255,0.3);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .spinner-lg {
    width: 32px;
    height: 32px;
    border: 3px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* ── Search Bar ─────────────────────── */
  .search-bar {
    display: flex;
    gap: 8px;
    margin-bottom: 20px;
  }
  .search-input {
    flex: 1;
    padding: 10px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-md);
    outline: none;
    transition: border-color 0.12s;
  }
  .search-input:focus { border-color: var(--accent); }
  .search-input::placeholder { color: var(--text-muted); }

  /* ── Buttons ────────────────────────── */
  .btn {
    padding: 8px 18px;
    border: none;
    border-radius: var(--radius-1);
    font-size: var(--text-base);
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
    transition: all 0.12s;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .btn-primary {
    background: var(--accent);
    color: var(--on-accent);
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }

  .btn-link {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: var(--text-sm);
    padding: 4px 0;
  }
  .btn-link:hover { text-decoration: underline; }

  .add-btn {
    width: 100%;
    padding: 10px 18px;
    font-size: var(--text-md);
    font-weight: 600;
  }

  /* ── Browser Content ────────────────── */
  .browser-content {
    display: flex;
    gap: 24px;
    flex: 1;
    min-height: 0;
  }
  .browser-content.has-preview .results-section {
    flex: 0 0 55%;
    max-width: 55%;
  }

  .results-section {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
  }

  /* ── Results Grid ──────────────────── */
  .results-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 10px;
  }

  .result-card {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    overflow: hidden;
    cursor: pointer;
    text-align: left;
    padding: 0;
    transition: all 0.12s;
  }
  .result-card:hover {
    border-color: var(--accent);
    background: var(--bg-hover);
  }
  .result-card.selected {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  .result-thumb {
    width: 100%;
    aspect-ratio: 16 / 9;
    overflow: hidden;
    background: var(--bg-tertiary);
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .result-thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .result-thumb-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    font-size: var(--text-3xl);
    opacity: 0.4;
  }

  .result-info {
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .result-title {
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    line-height: 1.3;
  }
  .result-meta {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .result-match {
    font-size: var(--text-2xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
  }

  /* ── Discover feed ─────────────────── */
  .discover {
    display: flex;
    flex-direction: column;
    gap: 12px;
    flex: 1;
    min-height: 0;
    height: 100%;
  }
  /* ── Browse toolbar: sort + filters (both surfaces) ─────── */
  .browse-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
    flex-shrink: 0;
  }
  .filter-engine { position: relative; }
  .filter-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--text-sm);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.12s;
  }
  .filter-btn:hover { color: var(--text-primary); border-color: var(--accent); }
  .filter-btn.active { color: var(--accent); border-color: var(--accent); }
  .filter-count {
    min-width: 16px;
    padding: 0 5px;
    border-radius: 8px;
    background: var(--accent);
    color: var(--on-accent);
    font-size: var(--text-2xs);
    font-weight: 700;
    line-height: 16px;
    text-align: center;
  }
  .filter-caret { font-size: var(--text-2xs); transition: transform 0.12s; }
  .filter-caret.open { transform: rotate(180deg); }
  .filter-panel {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 20;
    width: 230px;
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  }
  .filter-mode {
    display: flex;
    gap: 4px;
    margin-bottom: 8px;
  }
  .mode-btn {
    flex: 1;
    padding: 4px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-secondary);
    font-size: var(--text-xs);
    font-weight: 600;
    cursor: pointer;
  }
  .mode-btn.active {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--on-accent);
  }
  .engine-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 260px;
    overflow-y: auto;
  }
  .engine-opt {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 3px 4px;
    border-radius: var(--radius-1);
    cursor: pointer;
    font-size: var(--text-sm);
  }
  .engine-opt:hover { background: var(--bg-hover); }
  .engine-opt input { accent-color: var(--accent); }
  .engine-opt-name {
    padding: 1px 6px;
    border: 1px solid transparent;
    border-radius: var(--radius-1);
    font-size: var(--text-xs);
    font-weight: 600;
  }
  .filter-clear {
    margin-top: 8px;
    width: 100%;
    padding: 4px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--text-xs);
    cursor: pointer;
  }
  .filter-clear:hover { color: var(--text-primary); border-color: var(--accent); }
  .filter-toggle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--text-sm);
    color: var(--text-secondary);
    cursor: pointer;
    user-select: none;
  }
  .filter-toggle input { accent-color: var(--accent); }
  .filter-note {
    margin: -4px 0 10px;
    font-size: var(--text-xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .discover-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    position: relative;
  }
  .discover-spacer {
    position: relative;
    width: 100%;
  }
  .discover-row {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: grid;
    /* Must equal DISCOVER_GAP in the script. */
    gap: 0 10px;
  }
  .discover-tabs {
    display: flex;
    gap: 4px;
  }
  .discover-tab {
    padding: 5px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--text-sm);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.12s;
  }
  .discover-tab:hover { color: var(--text-primary); border-color: var(--accent); }
  .discover-tab.active {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--on-accent);
  }
  .discover-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .discover-count {
    font-size: var(--text-xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .discover-refresh {
    padding: 4px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--text-md);
    line-height: 1;
    cursor: pointer;
    transition: all 0.12s;
  }
  .discover-refresh:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
  .discover-refresh:disabled { opacity: 0.5; cursor: not-allowed; }
  .discover-note {
    margin: 0;
    padding: 8px 12px;
    border: 1px solid var(--warning);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
    color: var(--warning);
    font-size: var(--text-sm);
  }
  .discover-version {
    font-size: var(--text-2xs);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .discover-rating {
    font-size: var(--text-2xs);
    color: var(--warning);
    font-weight: 600;
    white-space: nowrap;
  }
  .discover-stat {
    font-size: var(--text-2xs);
    color: var(--text-muted);
    white-space: nowrap;
  }
  .discover-creator {
    font-size: var(--text-2xs);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .result-info .result-meta + .result-meta { margin-top: 2px; }
  /* Fixed caption height so the windowed rows are uniform. Must equal
     DISCOVER_TEXT_H in the script. */
  .discover-card .result-info {
    height: 68px;
    overflow: hidden;
  }
  /* Discover cards are disabled (not clickable) until F95Zone is connected;
     keep them legible rather than dimming the whole tile. */
  .result-card:disabled { cursor: default; opacity: 0.9; }
  .result-card:disabled:hover { border-color: var(--border); background: var(--bg-secondary); }
  .discover-more {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 16px 0;
    color: var(--text-secondary);
    font-size: var(--text-sm);
  }
  .discover-load-more {
    display: block;
    margin: 12px auto 0;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    color: var(--text-primary);
  }
  .discover-load-more:hover { border-color: var(--accent); background: var(--bg-hover); }
  .discover-end {
    margin: 12px 0 0;
    text-align: center;
    font-size: var(--text-xs);
    color: var(--text-muted);
  }
  .discover-sentinel { height: 1px; }

  /* ── Engine Badge ──────────────────── */
  .engine-badge {
    display: inline-block;
    padding: 1px 6px;
    border-radius: var(--radius-1);
    font-size: var(--text-2xs);
    font-weight: 600;
    line-height: 1.5;
  }

  /* ── Status Badge ──────────────────── */
  .status-badge {
    display: inline-block;
    padding: 1px 6px;
    border-radius: var(--radius-1);
    font-size: var(--text-2xs);
    font-weight: 600;
    line-height: 1.5;
    text-transform: capitalize;
  }
  .status-active { background: color-mix(in srgb, var(--success) 15%, transparent); color: var(--success); }
  .status-completed { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); }
  .status-on-hold { background: color-mix(in srgb, var(--warning) 15%, transparent); color: var(--warning); }
  .status-abandoned { background: color-mix(in srgb, var(--danger) 15%, transparent); color: var(--danger); }

  /* ── Loading / Empty / Error ───────── */
  .loading-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 48px 0;
    color: var(--text-secondary);
    font-size: var(--text-base);
  }
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 48px 0;
    text-align: center;
  }
  .empty-icon { font-size: var(--text-4xl); opacity: 0.5; margin-bottom: 8px; }
  .empty-title { font-size: var(--text-lg); font-weight: 600; color: var(--text-primary); }
  .empty-detail { font-size: var(--text-base); color: var(--text-muted); }

  .error-section {
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--danger);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .error-title {
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--danger);
    margin: 0 0 4px;
  }
  .error-line {
    font-size: var(--text-sm);
    color: var(--text-secondary);
    margin: 0;
    font-family: var(--font-mono);
    word-break: break-word;
  }

  /* ── Preview Section ────────────────── */
  .preview-section {
    flex: 0 0 45%;
    max-width: 45%;
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    padding: 20px;
    position: relative;
  }

  .preview-close {
    position: absolute;
    top: 12px;
    right: 12px;
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: var(--text-xl);
    cursor: pointer;
    padding: 4px 8px;
    border-radius: var(--radius-1);
    line-height: 1;
  }
  .preview-close:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  /* ── Preview Cover ────────────────── */
  .preview-cover {
    width: 100%;
    aspect-ratio: 16 / 9;
    border-radius: var(--radius-1);
    overflow: hidden;
    margin-bottom: 16px;
    background: var(--bg-tertiary);
  }
  .preview-cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .preview-cover-placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    font-size: var(--text-md);
    color: var(--text-muted);
  }
  .preview-cover-placeholder span:first-child {
    font-size: var(--text-4xl);
    opacity: 0.5;
  }

  /* ── Preview Meta ──────────────────── */
  .preview-meta {
    margin-bottom: 16px;
  }
  .preview-title {
    font-size: var(--text-xl);
    font-weight: 700;
    margin: 0 0 8px;
    line-height: 1.3;
    color: var(--text-primary);
  }
  .preview-badges {
    display: flex;
    gap: 6px;
    margin-bottom: 12px;
    flex-wrap: wrap;
  }
  /* Same label/value pattern as GameDetail's .meta-row/.meta-label/.meta-value
     (docs/desktop-ui-research.md §7 item 9) — this preview used to be an
     inline "<strong>Label:</strong> value" one-off that didn't match. */
  .preview-meta-row {
    display: flex;
    gap: 8px;
    margin: 0 0 4px;
  }
  .preview-meta-label {
    min-width: 70px;
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    flex-shrink: 0;
  }
  .preview-meta-value {
    font-size: var(--text-base);
    color: var(--text-primary);
  }

  /* ── Tags ─────────────────────────────── */
  .preview-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 8px;
  }
  .tag {
    display: inline-block;
    padding: 2px 7px;
    border-radius: var(--radius-1);
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    font-size: var(--text-xs);
    font-weight: 500;
  }

  /* ── Overview ─────────────────────── */
  .preview-overview {
    margin-bottom: 16px;
  }
  .preview-overview h4 {
    font-size: var(--text-base);
    font-weight: 600;
    margin: 0 0 6px;
    color: var(--text-primary);
  }
  .preview-overview p {
    font-size: var(--text-base);
    color: var(--text-secondary);
    margin: 0 0 4px;
    line-height: 1.6;
    white-space: pre-line;
  }

  /* ── Store Links ──────────────────── */
  .preview-stores {
    margin-bottom: 16px;
  }
  .preview-stores h4 {
    font-size: var(--text-base);
    font-weight: 600;
    margin: 0 0 6px;
    color: var(--text-primary);
  }
  .store-links {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .store-link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--accent);
    font-size: var(--text-sm);
    text-decoration: none;
    text-transform: capitalize;
  }
  .store-link:hover {
    background: var(--bg-hover);
    border-color: var(--accent);
  }

  /* ── Download Links ───────────────── */
  .preview-downloads {
    margin-bottom: 16px;
  }
  .preview-downloads h4 {
    font-size: var(--text-base);
    font-weight: 600;
    margin: 0 0 6px;
    color: var(--text-primary);
  }
  .download-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .download-link {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    text-decoration: none;
    transition: all 0.12s;
  }
  .download-link:hover {
    background: var(--bg-hover);
    border-color: var(--accent);
  }
  .dl-host {
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--accent);
    text-transform: capitalize;
    flex-shrink: 0;
    min-width: 70px;
  }
  .dl-name {
    flex: 1;
    font-size: var(--text-sm);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dl-platform {
    font-size: var(--text-2xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
    flex-shrink: 0;
  }

  /* ── Preview Actions ──────────────── */
  .preview-actions {
    margin-top: 8px;
  }

  .add-success {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border: 1px solid var(--success);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--success) 10%, transparent);
    color: var(--success);
    font-size: var(--text-base);
    font-weight: 500;
  }
  .add-success span:first-child {
    font-size: var(--text-xl);
    font-weight: 700;
  }
  .add-error {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    border: 1px solid var(--danger);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    color: var(--danger);
    font-size: var(--text-base);
  }
  .add-error span:first-child {
    font-size: var(--text-lg);
    font-weight: 700;
    flex-shrink: 0;
  }
</style>
