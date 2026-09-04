<script>
  import {onMount, onDestroy, tick} from 'svelte'
  import {createVirtualizer} from '@tanstack/svelte-virtual'
  import {SearchGames, RemoveGame, SetGameStatus, RenameGame, GetCoverBaseURL, PlayGame} from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'
  import {GAME_STATUSES, statusLabel} from './statuses.js'
  import {library} from './viewState.svelte.js'

  let {
    games = [],
    loading = false,
    gameStates = {},
    onOpenDetail = (id) => {},
    onUpdate = () => {},
    onLaunched = (msg) => {},
  } = $props()

  // ── Play-state model (P0-3) ─────────────────────────────────
  // The library renders a play affordance per card/row that switches state
  // while a launch is happening/most recent — Steam's play-bar pattern. We
  // cannot know when the launched process exits (PlayGame detaches), so the
  // "playing" state is optimistic and reverts after a short timeout. A
  // successful launch also nudges the recency arrangement (the mock backend
  // records a play entry; the real one does via db.RecordPlay).
  const PLAY_REVERT_MS = 6000
  const PLAY_ERROR_REVERT_MS = 3500
  let playState = $state({})   // gameId → {state:'launching'|'playing'|'error', msg}

  function playControl(g) {
    return playState[g.id] ?? {state: 'idle', msg: ''}
  }

  async function handlePlay(e, game) {
    e.stopPropagation()
    e.preventDefault()
    const cur = playState[game.id]
    if (cur && (cur.state === 'launching' || cur.state === 'playing')) return
    playState = {...playState, [game.id]: {state: 'launching', msg: ''}}
    try {
      const msg = await PlayGame(game.id)
      const clean = String(msg || '').replace(/^Error:\s*/, '')
      playState = {...playState, [game.id]: {state: 'playing', msg: clean}}
      onLaunched(clean)
      // The real backend records the play entry server-side; the mock does
      // too — refresh so the recency arrangement reflects the new entry.
      await onUpdate()
      setTimeout(() => {
        playState = {...playState, [game.id]: {state: 'idle', msg: ''}}
      }, PLAY_REVERT_MS)
    } catch (err) {
      const msg = String(err).replace(/^Error:\s*/, '')
      playState = {...playState, [game.id]: {state: 'error', msg}}
      setTimeout(() => {
        playState = {...playState, [game.id]: {state: 'idle', msg: ''}}
      }, PLAY_ERROR_REVERT_MS)
    }
  }

  // ── Play-state derivation (P0-4 quick views) ────────────────
  // Orthogonal to `status` (the user's curation axis): a game is "installed"
  // when it has a real local path (not a /virtual/ F95Zone reference) and
  // "ready to play" when it is installed and nothing (update/install
  // pipeline) is currently mutating it.
  const VIRTUAL_PATH = '/virtual/'
  const UPDATE_BUSY_PHASES = ['syncing', 'selecting-link', 'downloading', 'extracting', 'merging', 'updating-db']

  function isVirtual(g) {
    return !!g?.path?.startsWith(VIRTUAL_PATH)
  }

  // `gameStates` is replaced wholesale on every download/extract progress
  // tick (up to 5x/sec while any update is running), but only phase
  // *transitions* actually change which games are "busy". Without this
  // guard, the 'ready' quick view's `displayed` derived below would
  // re-filter + re-sort the entire library on every percent tick. Track just
  // the busy-id membership and only touch this $state (and thus retrigger
  // `displayed`) when that membership set actually changes.
  let busyIds = $state(new Set())
  $effect(() => {
    const next = new Set()
    for (const id in gameStates) {
      const s = gameStates[id]
      if (s && UPDATE_BUSY_PHASES.includes(s.phase)) next.add(id)
    }
    const unchanged = next.size === busyIds.size && [...next].every(id => busyIds.has(id))
    if (!unchanged) busyIds = next
  })

  // ── Cover loading ─────────────────────────────────────────────
  // Covers are served by the backend over loopback HTTP (see GetCoverBaseURL)
  // and the webview caches them itself — no base64 over the IPC bridge.
  // An empty coverBase means the cover server failed to start: rows fall
  // back to the placeholder.
  let coverBase = $state('')

  // Games whose <img> failed to load + the retry epoch live in viewState so
  // the failure set (and its retry logic) survives tab switches. A retry
  // epoch appended to the src forces the webview to re-request them after a
  // sync/backfill caches the file — the plain URL would otherwise keep
  // returning the cached 404. Retry bookkeeping lives in App.svelte's
  // always-subscribed handlers (covers:complete, sync:game-done) so it also
  // fires while this view is unmounted.

  function coverSrc(id, variant = 'thumb') {
    const epoch = library.failedCovers.has(id) ? `?r=${library.coverEpoch}` : ''
    return `${coverBase}/cover/${id}/${variant}${epoch}`
  }

  function markFailed(id) {
    library.failedCovers = new Set([...library.failedCovers, id])
  }

  onMount(async () => {
    try {
      coverBase = await GetCoverBaseURL()
    } catch (e) {
      console.error('Failed to get cover base URL', e)
    }
    // A persisted query (from a previous visit to this tab) re-runs the
    // search immediately — no debounce, the user already typed it. Await it
    // so the scroll restore below lands on the FINAL list (search results),
    // not the full list that renders first.
    if (library.search.trim().length >= 2) await doSearch(library.search)
    // Restore the scroll offset after the first paint: rows render
    // synchronously from the games prop, so a tick suffices.
    await tick()
    if (tableBodyEl) tableBodyEl.scrollTop = library.scrollTop
    if (gridEl) gridEl.scrollTop = library.gridScrollTop
  })

  onDestroy(() => {
    clearTimeout(debounceTimer)
    unsubGridVirtualizer()
    unsubTableVirtualizer()
  })

  // ── Search & Filters ──────────────────────────────────────────
  // library.search / filters / sort live in viewState so they survive tab
  // switches; only the in-flight request state is local.
  let debounceTimer                        // plain var, not reactive
  let searchResults = $state(null)         // null = use full list, array = search results
  let isSearching = $state(false)
  let tableBodyEl = $state.raw()           // list scroll container, bound in markup
  let gridEl = $state.raw()                // grid scroll container, bound in markup

  // Extract distinct engines from the game list
  let engines = $derived.by(() => {
    const set = new Set()
    for (const g of games) set.add(g.engine)
    const arr = [...set].filter(Boolean).sort()
    arr.unshift('All')
    return arr
  })

  // Available statuses (shared with the context menu via lib/statuses.js)
  const statuses = ['All', ...GAME_STATUSES]

  // ── Quick views (P0-4) ────────────────────────────────────────
  // Primary play-state tabs; engine/status filters below stay secondary.
  const QUICK_VIEWS = [
    {value: 'all',       label: 'All'},
    {value: 'installed', label: 'Installed'},
    {value: 'ready',     label: 'Ready to play'},
    {value: 'recent',    label: 'Recently played'},
  ]

  function quickViewMatches(v, g) {
    switch (v) {
      case 'installed': return !isVirtual(g)
      case 'ready':     return !isVirtual(g) && !busyIds.has(String(g.id))
      case 'recent':    return tsVal(g, 'lastPlayed') !== null
      default:          return true
    }
  }

  // ── Sorting / arrangement ─────────────────────────────────────
  // Default arrangement is recency-first (P0-2): games with a last-played
  // timestamp sort most-recent first, never-played games group last (title
  // asc). "Date added" is a first-class option — moxie owns created_at,
  // which competitors like Heroic can't even offer. Timestamp fields are
  // optional on the summary (see mock-data.js enrichment note); missing
  // values degrade to stable fallbacks instead of breaking the sort.
  const SORT_OPTIONS = [
    {value: 'recent', label: 'Recently played'},
    {value: 'added',  label: 'Date added'},
    {value: 'title',  label: 'Title A–Z'},
    {value: 'engine', label: 'Engine'},
    {value: 'version',label: 'Version'},
    {value: 'size',   label: 'Size'},
    {value: 'status', label: 'Status'},
  ]

  // Numeric-aware version comparison: "10.0" sorts after "9.0". Splits on
  // non-alphanumerics and compares token-wise — numeric tokens numerically,
  // anything else lexically (numeric tokens first when mixed).
  function compareVersions(a, b) {
    const ta = String(a || '')
    const tb = String(b || '')
    if (ta === tb) return 0
    const pa = ta.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)
    const pb = tb.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)
    const len = Math.max(pa.length, pb.length)
    for (let i = 0; i < len; i++) {
      const x = pa[i]
      const y = pb[i]
      if (x === undefined) return -1
      if (y === undefined) return 1
      const xNum = /^\d+$/.test(x)
      const yNum = /^\d+$/.test(y)
      if (xNum && yNum) {
        const n = parseInt(x, 10) - parseInt(y, 10)
        if (n !== 0) return n
      } else if (xNum !== yNum) {
        return xNum ? -1 : 1
      } else {
        const c = x.localeCompare(y)
        if (c !== 0) return c
      }
    }
    return 0
  }

  function tsVal(g, key) {
    const s = g?.[key]
    if (!s) return null
    const t = Date.parse(s)
    return Number.isNaN(t) ? null : t
  }

  function toggleSort(col) {
    if (library.sortColumn === col) {
      library.sortDesc = !library.sortDesc
    } else {
      library.sortColumn = col
      library.sortDesc = false
    }
  }

  function sortIcon(col) {
    if (library.sortColumn !== col) return '▽'
    return library.sortDesc ? '▲' : '▼'
  }

  function onSortSelect(e) {
    library.sortColumn = e.target.value
    // Recency & date-added default to most-recent-first; column sorts start
    // ascending (clicking a header toggles direction).
    library.sortDesc = e.target.value === 'added'
  }

  function sortCompare(a, b) {
    switch (library.sortColumn) {
      case 'recent': {
        const ta = tsVal(a, 'lastPlayed')
        const tb = tsVal(b, 'lastPlayed')
        if (ta === null && tb === null) {
          return (a.title || '').localeCompare(b.title || '', undefined, {numeric: true})
        }
        if (ta === null) return 1
        if (tb === null) return -1
        return tb - ta                     // most recent first
      }
      case 'added': {
        const ta = tsVal(a, 'createdAt')
        const tb = tsVal(b, 'createdAt')
        let cmp
        if (ta === null && tb === null) cmp = (a.id || 0) - (b.id || 0)
        else if (ta === null) cmp = 1
        else if (tb === null) cmp = -1
        else cmp = ta - tb
        return library.sortDesc ? -cmp : cmp
      }
      case 'title':
        return (a.title || '').localeCompare(b.title || '', undefined, {numeric: true})
      case 'engine':
        return (a.engine || '').localeCompare(b.engine || '')
      case 'version':
        return compareVersions(a.version, b.version)
      case 'status':
        return (a.status || '').localeCompare(b.status || '')
      case 'size':
        return (a.sizeBytes || 0) - (b.sizeBytes || 0)
      default:
        return 0
    }
  }

  // ── Derived: quick-view-filtered + sorted list ───────────────
  let displayed = $derived.by(() => {
    // 1. Use search results if available, else full list
    let list = searchResults ?? games

    // 2. Filter by quick view (primary axis)
    if (library.quickView !== 'all') {
      list = list.filter(g => quickViewMatches(library.quickView, g))
    }

    // 3. Filter by engine (secondary)
    if (library.engine && library.engine !== 'All') {
      list = list.filter(g => g.engine === library.engine)
    }

    // 4. Filter by status (secondary, curation axis)
    if (library.status && library.status !== 'All') {
      list = list.filter(g => g.status === library.status)
    }

    // 5. Sort / arrangement
    const sorted = [...list]
    sorted.sort(sortCompare)
    return sorted
  })

  // ── Grid/table virtualization (perf follow-up) ────────────────
  // Neither view windowed its rows before this — a library of a few hundred+
  // games built a proportionally large DOM (worse for the grid, since many
  // cards sit above-the-fold at once). @tanstack/svelte-virtual renders only
  // the visible range (+ overscan) and pads the rest with a sized spacer so
  // the scrollbar still reflects the true content height.
  //
  // The grid's card width is fluid (`auto-fill, minmax(176px, 1fr)`), so the
  // number of columns per row — and thus row height, since the cover keeps a
  // 16:9 aspect ratio off that width — depends on container width. A
  // ResizeObserver on the scroll container recomputes both whenever it
  // changes (window resize, sidebar toggle, view-mode switch).
  const GRID_CARD_MIN = 176
  const GRID_GAP = 16
  const GRID_PAD = 32                // 16px horizontal padding, both sides
  const GRID_TEXT_HEIGHT = 8 + 40 + 4 + 18   // title margin + 2-line title + meta margin + meta row
  let gridColumns = $state(1)
  let gridRowHeight = $state(280)

  function updateGridLayout() {
    if (!gridEl) return
    const avail = Math.max(gridEl.clientWidth - GRID_PAD, GRID_CARD_MIN)
    const cols = Math.max(1, Math.floor((avail + GRID_GAP) / (GRID_CARD_MIN + GRID_GAP)))
    const cardWidth = (avail - GRID_GAP * (cols - 1)) / cols
    const coverHeight = cardWidth * 9 / 16
    gridColumns = cols
    gridRowHeight = coverHeight + GRID_TEXT_HEIGHT + GRID_GAP
  }

  $effect(() => {
    if (!gridEl) return
    updateGridLayout()
    const ro = new ResizeObserver(() => updateGridLayout())
    ro.observe(gridEl)
    return () => ro.disconnect()
  })

  // Chunk the flat `displayed` list into fixed-size rows so the virtualizer
  // only has to window rows, not think about wrapping.
  let gridRows = $derived.by(() => {
    const rows = []
    for (let i = 0; i < displayed.length; i += gridColumns) {
      rows.push(displayed.slice(i, i + gridColumns))
    }
    return rows
  })

  // NOTE on the subscribe-by-hand pattern below: `setOptions()` unconditionally
  // force-emits a new store value on every call (see the library's source —
  // it re-sets the writable even when the visible range didn't change, so
  // count-only updates still notify). Reading the store reactively (`$store`)
  // from *inside* the same $effect that calls `.setOptions()` would make that
  // effect depend on its own output and spin forever
  // (`effect_update_depth_exceeded`). So: `.setOptions()`/`.measure()` are
  // called on a plain (non-reactive) reference to the instance, and a
  // hand-rolled `.subscribe()` mirrors the current virtual items/total size
  // into actual `$state` for the template to read.
  let gridVirtualizerApi        // plain ref — same singleton instance every emit
  let gridVirtualRows = $state.raw([])
  let gridTotalSize = $state(0)
  const unsubGridVirtualizer = createVirtualizer({
    count: 0,
    getScrollElement: () => gridEl,
    estimateSize: () => gridRowHeight,
    overscan: 3,
  }).subscribe(v => {
    gridVirtualizerApi = v
    gridVirtualRows = v.getVirtualItems()
    gridTotalSize = v.getTotalSize()
  })

  $effect(() => {
    const el = gridEl
    const rows = gridRows
    const rowHeight = gridRowHeight
    gridVirtualizerApi?.setOptions({
      count: rows.length,
      getScrollElement: () => el,
      estimateSize: () => rowHeight,
      overscan: 3,
      getItemKey: (i) => rows[i]?.map(g => g.id).join(',') ?? i,
    })
    gridVirtualizerApi?.measure()
  })

  // Table rows are uniform height, so windowing is simpler — one measurement
  // for the whole list. Matches the row's rendered height: 40px cover thumb
  // + 4px top/bottom padding + 1px border.
  const TABLE_ROW_HEIGHT = 49

  let tableVirtualizerApi
  let tableVirtualRows = $state.raw([])
  let tableTotalSize = $state(0)
  const unsubTableVirtualizer = createVirtualizer({
    count: 0,
    getScrollElement: () => tableBodyEl,
    estimateSize: () => TABLE_ROW_HEIGHT,
    overscan: 8,
  }).subscribe(v => {
    tableVirtualizerApi = v
    tableVirtualRows = v.getVirtualItems()
    tableTotalSize = v.getTotalSize()
  })

  $effect(() => {
    const el = tableBodyEl
    const rows = displayed
    tableVirtualizerApi?.setOptions({
      count: rows.length,
      getScrollElement: () => el,
      estimateSize: () => TABLE_ROW_HEIGHT,
      overscan: 8,
      getItemKey: (i) => rows[i]?.id ?? i,
    })
    tableVirtualizerApi?.measure()
  })

  // ── Search with debounce ──────────────────────────────────────
  // Monotonic request id: a slower, older response must never overwrite a
  // newer one (or resurrect results after the field was cleared).
  let searchSeq = 0

  async function doSearch(query) {
    if (!query || query.trim().length < 2) {
      searchSeq++                    // invalidate any in-flight request
      searchResults = null
      isSearching = false
      return
    }
    const seq = ++searchSeq
    isSearching = true
    try {
      const res = await SearchGames(query.trim())
      if (seq !== searchSeq) return   // stale — a newer search owns the results
      searchResults = res
    } catch (e) {
      if (seq !== searchSeq) return
      console.error('Search failed:', e)
      searchResults = null
    }
    if (seq === searchSeq) isSearching = false
  }

  function onSearchInput(e) {
    library.search = e.target.value
    clearTimeout(debounceTimer)
    // Clearing (or shortening below the threshold) must clear results right
    // away and invalidate any in-flight request — don't wait for the debounce.
    if (!library.search || library.search.trim().length < 2) {
      doSearch(library.search)
      return
    }
    debounceTimer = setTimeout(() => doSearch(library.search), 300)
  }

  // ── Engine colors — imported from shared module ───────────────
  // See engineColors.js for the canonical palette matching TUI styles

  function hasUpdate(game) {
    return game.latestVersion && game.version &&
           game.latestVersion !== game.version
  }

  function lastPlayedDate(g) {
    const t = tsVal(g, 'lastPlayed')
    return t === null ? null : new Date(t)
  }

  function relativePlayed(d) {
    const mins = Math.floor((Date.now() - d.getTime()) / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    if (days < 30) return `${days}d ago`
    const months = Math.floor(days / 30)
    if (months < 12) return `${months}mo ago`
    return `${Math.floor(months / 12)}y ago`
  }

  // ── Context Menu ──────────────────────────────────────────────
  let contextMenu = $state(null)     // {x, y, game} or null
  let contextMenuView = $state('main') // 'main' | 'status'

  function onRowContextMenu(e, game) {
    e.preventDefault()
    closeContextMenu()
    // position menu, keeping it inside viewport
    const menuW = 200, menuH = 220
    const x = Math.min(e.clientX, window.innerWidth - menuW)
    const y = Math.min(e.clientY, window.innerHeight - menuH)
    contextMenu = {x, y, game}
    contextMenuView = 'main'
  }

  function closeContextMenu() {
    contextMenu = null
    contextMenuView = 'main'
  }

  async function handleStatus(status) {
    if (!contextMenu?.game) return
    const g = contextMenu.game
    closeContextMenu()
    try {
      await SetGameStatus(g.id, status)
      await onUpdate()
    } catch (e) {
      console.error('Failed to set status:', e)
    }
  }

  async function handleRename() {
    if (!contextMenu?.game) return
    const g = contextMenu.game
    closeContextMenu()
    const newTitle = window.prompt('Enter new title:', g.title)
    if (!newTitle || newTitle.trim() === '' || newTitle.trim() === g.title) return
    try {
      await RenameGame(g.id, newTitle.trim())
      await onUpdate()
    } catch (e) {
      console.error('Failed to rename:', e)
    }
  }

  async function handleRemove() {
    if (!contextMenu?.game) return
    const g = contextMenu.game
    closeContextMenu()
    if (!window.confirm(`Are you sure you want to remove "${g.title}" from your library?`)) return
    try {
      await RemoveGame(g.id, false)
      await onUpdate()
    } catch (e) {
      console.error('Failed to remove:', e)
    }
  }

  // ── Curated empty-state text per quick view ──────────────────
  let emptyTitle = $derived.by(() => {
    if (library.search.trim().length >= 2) return 'No matches'
    switch (library.quickView) {
      case 'recent':     return 'Nothing played recently'
      case 'installed':  return 'Nothing installed yet'
      case 'ready':      return 'Nothing ready to play'
      default:           return 'No games match'
    }
  })

  let emptyHint = $derived.by(() => {
    if (library.search.trim().length >= 2) return 'Try a different title, tag, or developer.'
    switch (library.quickView) {
      case 'recent':     return 'Games you launch from the library will appear here.'
      case 'installed':  return 'Scan or add a directory to install titles, or grab one from the F95Zone browser.'
      case 'ready':      return 'Install a not-yet-downloaded title or let a running update finish.'
      default:           return 'Relax the engine or status filters.'
    }
  })
</script>

<div class="game-list">
  {#if games.length === 0 && !isSearching && !loading}
    <div class="empty">
      <div class="empty-icon">📂</div>
      <p class="empty-title">No games yet</p>
      <p class="empty-desc">Scan a directory to find and add games to your library.</p>
    </div>
  {:else}
    {#if isSearching}
      <div class="searching-indicator">Searching…</div>
    {:else if loading && games.length === 0}
      <div class="searching-indicator">Loading library…</div>
    {/if}

    <!-- Quick view tabs (primary play-state axis, P0-4) + grid/list toggle -->
    <div class="quick-bar">
      <div class="quick-tabs">
        {#each QUICK_VIEWS as qv}
          <button
            class="quick-tab"
            class:active={library.quickView === qv.value}
            aria-pressed={library.quickView === qv.value}
            onclick={() => library.quickView = qv.value}
          >
            {qv.label}
          </button>
        {/each}
      </div>

      <div class="view-toggle" role="group" aria-label="Library layout">
        <button
          class="vbtn"
          class:active={library.viewMode === 'grid'}
          aria-pressed={library.viewMode === 'grid'}
          title="Grid view"
          onclick={() => library.viewMode = 'grid'}
        ><span class="vbtn-icon">▦</span>Grid</button>
        <button
          class="vbtn"
          class:active={library.viewMode === 'list'}
          aria-pressed={library.viewMode === 'list'}
          title="List view"
          onclick={() => library.viewMode = 'list'}
        ><span class="vbtn-icon">☰</span>List</button>
      </div>
    </div>

    <!-- Secondary filters: search · engine · arrangement · status chips -->
    <div class="filter-bar">
      <div class="search-wrapper">
        <span class="search-icon">🔍</span>
        <input
          type="text"
          class="search-input"
          placeholder="Search titles, tags, developers…"
          value={library.search}
          oninput={onSearchInput}
        />
      </div>

      <span class="select-arrow">
        <select class="filter-select" bind:value={library.engine}>
          {#each engines as eng}
            <option value={eng}>{eng}</option>
          {/each}
        </select>
      </span>

      <span class="select-arrow">
        <select class="filter-select sort-select" aria-label="Arrangement" value={library.sortColumn} onchange={onSortSelect}>
          {#each SORT_OPTIONS as o}
            <option value={o.value}>{o.label}</option>
          {/each}
        </select>
      </span>

      <div class="status-chips">
        {#each statuses as s}
          <button
            class="chip"
            class:chip-active={library.status === s}
            onclick={() => library.status = library.status === s ? '' : s}
          >
            {statusLabel(s)}
          </button>
        {/each}
      </div>
    </div>

    {#if displayed.length === 0}
      <div class="empty small">
        <p class="empty-title">{emptyTitle}</p>
        <p class="empty-desc">{emptyHint}</p>
      </div>
    {:else if library.viewMode === 'grid'}
      <!-- ── Cover grid (default library presentation, P0-1) ── -->
      <div
        class="grid-scroll"
        bind:this={gridEl}
        onscroll={(e) => library.gridScrollTop = e.currentTarget.scrollTop}
      >
        <div class="grid-spacer" style="height: {gridTotalSize}px">
          {#each gridVirtualRows as vRow (vRow.key)}
            <div
              class="grid-row"
              style="grid-template-columns: repeat({gridColumns}, 1fr); height: {gridRowHeight - GRID_GAP}px; transform: translateY({vRow.start + GRID_GAP}px)"
            >
              {#each gridRows[vRow.index] ?? [] as game (game.id)}
                <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions a11y_no_noninteractive_tabindex -->
                {@const ctl = playControl(game)}
                <div
                  class="card"
                  role="button"
                  tabindex="0"
                  onclick={() => onOpenDetail(game.id)}
                  onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onOpenDetail(game.id); } }}
                  oncontextmenu={(e) => onRowContextMenu(e, game)}
                >
                  <div class="cover-frame">
                    {#if game.hasCover && coverBase && !library.failedCovers.has(game.id)}
                      <img
                        class="cover-img"
                        src={coverSrc(game.id)}
                        alt="{game.title} cover"
                        loading="lazy"
                        onerror={() => markFailed(game.id)}
                      />
                    {:else if game.hasCover && coverBase}
                      <span class="cover-ph" title="Cover unavailable — retries after the next sync or cover fetch">
                        <span class="cover-ph-icon">🖼</span>
                      </span>
                    {:else}
                      <span class="cover-ph" title="No cover — use Covers in the sidebar to fetch one">
                        <span class="cover-ph-icon">🎮</span>
                      </span>
                    {/if}

                    {#if hasUpdate(game)}
                      <span class="card-badge badge-update" title="Update available: {game.latestVersion}">
                        ⇪ {game.version} → {game.latestVersion}
                      </span>
                    {/if}
                    {#if isVirtual(game)}
                      <span class="card-badge badge-virtual">Not installed</span>
                    {/if}

                    <button
                      class="card-play"
                      class:state-launching={ctl.state === 'launching'}
                      class:state-playing={ctl.state === 'playing'}
                      class:state-error={ctl.state === 'error'}
                      disabled={ctl.state === 'launching'}
                      title={ctl.msg || 'Play'}
                      onclick={(e) => handlePlay(e, game)}
                    >
                      {#if ctl.state === 'launching'}
                        <span class="play-spinner"></span><span>Launching…</span>
                      {:else if ctl.state === 'playing'}
                        <span>⏸ Playing</span>
                      {:else if ctl.state === 'error'}
                        <span>↻ Retry</span>
                      {:else}
                        <span>▶ Play</span>
                      {/if}
                    </button>
                  </div>

                  <div class="card-title" title={game.title}>{game.title}</div>
                  <div class="card-meta">
                    {#if game.engine}
                      <span class="card-engine" style="--ec: {engineColor(game.engine)}">{game.engine}</span>
                    {/if}
                    {#if lastPlayedDate(game)}
                      <span class="card-last-played" title="Last played {lastPlayedDate(game).toLocaleString()}">
                        ▶ {relativePlayed(lastPlayedDate(game))}
                      </span>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          {/each}
        </div>
      </div>
    {:else}
      <!-- ── List / table view (kept as an explicit toggle) ── -->
      <div class="table-header">
        <span class="col-cover">Cover</span>
        <button class="col-title col-sortable" onclick={() => toggleSort('title')}>
          Title <span class="sort-arrow">{sortIcon('title')}</span>
        </button>
        <button class="col-engine col-sortable" onclick={() => toggleSort('engine')}>
          Engine <span class="sort-arrow">{sortIcon('engine')}</span>
        </button>
        <button class="col-version col-sortable" onclick={() => toggleSort('version')}>
          Version <span class="sort-arrow">{sortIcon('version')}</span>
        </button>
        <button class="col-size col-sortable" onclick={() => toggleSort('size')}>
          Size <span class="sort-arrow">{sortIcon('size')}</span>
        </button>
        <button class="col-status col-sortable" onclick={() => toggleSort('status')}>
          Status <span class="sort-arrow">{sortIcon('status')}</span>
        </button>
        <span class="col-play">Play</span>
      </div>

      <div
        class="table-body"
        bind:this={tableBodyEl}
        onscroll={(e) => library.scrollTop = e.currentTarget.scrollTop}
      >
        <div class="table-spacer" style="height: {tableTotalSize}px">
          {#each tableVirtualRows as vItem (vItem.key)}
            {@const game = displayed[vItem.index]}
            {#if game}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              {@const ctl = playControl(game)}
              <div
                class="table-row"
                style="height: {TABLE_ROW_HEIGHT}px; transform: translateY({vItem.start}px)"
                onclick={() => onOpenDetail(game.id)}
                oncontextmenu={(e) => onRowContextMenu(e, game)}
                onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onOpenDetail(game.id); } }}
                role="button"
                tabindex="0"
              >
                <span class="col-cover">
                  {#if game.hasCover && coverBase && !library.failedCovers.has(game.id)}
                    <img
                      class="cover-thumb"
                      src={coverSrc(game.id)}
                      alt="{game.title} cover"
                      loading="lazy"
                      onerror={() => markFailed(game.id)}
                    />
                  {:else if game.hasCover && coverBase}
                    <span class="cover-placeholder" title="Cover unavailable — retries after the next sync or cover fetch">
                      <span class="cover-icon">🖼</span>
                    </span>
                  {:else}
                    <span class="cover-placeholder" title="No cover — use Covers in the sidebar to fetch one">
                      <span class="cover-icon">🎮</span>
                    </span>
                  {/if}
                </span>
                <span class="col-title game-title">
                  {game.title}
                  {#if hasUpdate(game)}
                    <span class="update-dot" title="Update available: {game.latestVersion}">●</span>
                  {/if}
                </span>
                <span class="col-engine">
                  {#if game.engine}
                    <span class="engine-badge" style="--ec: {engineColor(game.engine)}">
                      {game.engine}
                    </span>
                  {:else}
                    <span class="text-muted">—</span>
                  {/if}
                </span>
                <span class="col-version">
                  {#if hasUpdate(game)}
                    <span class="version-old">{game.version}</span>
                    <span class="version-new" title="Latest: {game.latestVersion}">→ {game.latestVersion}</span>
                  {:else}
                    {game.version || '—'}
                  {/if}
                </span>
                <span class="col-size">{game.sizeLabel || '—'}</span>
                <span class="col-status">
                  <span class="status-badge status-{game.status || 'unknown'}">
                    {statusLabel(game.status)}
                  </span>
                </span>
                <span class="col-play">
                  <button
                    class="row-play"
                    class:state-launching={ctl.state === 'launching'}
                    class:state-playing={ctl.state === 'playing'}
                    class:state-error={ctl.state === 'error'}
                    disabled={ctl.state === 'launching'}
                    title={ctl.msg || 'Play'}
                    onclick={(e) => handlePlay(e, game)}
                  >
                    {#if ctl.state === 'launching'}
                      <span class="play-spinner small"></span>
                    {:else if ctl.state === 'playing'}
                      ⏸
                    {:else if ctl.state === 'error'}
                      ↻
                    {:else}
                      ▶ Play
                    {/if}
                  </button>
                </span>
              </div>
            {/if}
          {/each}
        </div>
      </div>
    {/if}

    <!-- Context Menu -->
    {#if contextMenu}
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="context-menu-overlay" onclick={closeContextMenu} oncontextmenu={(e) => e.preventDefault()}>
        <div class="context-menu" style="left: {contextMenu.x}px; top: {contextMenu.y}px" onclick={(e) => e.stopPropagation()}>
          {#if contextMenuView === 'main'}
            <button class="ctx-item" onclick={() => contextMenuView = 'status'}>
              <span>Set Status</span>
              <span class="ctx-arrow">▶</span>
            </button>
            <button class="ctx-item" onclick={handleRename}>Rename</button>
            <div class="ctx-divider"></div>
            <button class="ctx-item ctx-danger" onclick={handleRemove}>Remove</button>
          {:else}
            <button class="ctx-item" onclick={() => contextMenuView = 'main'}>
              <span class="ctx-back-arrow">◀</span>
              <span>Status</span>
            </button>
            <div class="ctx-divider"></div>
            {#each GAME_STATUSES as s}
              <button class="ctx-item" onclick={() => handleStatus(s)}>
                <span class="ctx-dot ctx-dot-{s}"></span>
                {statusLabel(s)}
              </button>
            {/each}
          {/if}
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .game-list {
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  /* ── Empty State ───────────────────────────────────── */
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    gap: 8px;
    color: var(--text-muted);
  }
  .empty.small { height: 100%; padding: 24px; }
  .empty-icon { font-size: 40px; opacity: 0.5; }
  .empty-title { font-size: 16px; font-weight: 600; color: var(--text-secondary); }
  .empty-desc { font-size: 13px; }

  /* ── Quick view tabs + layout toggle (P0-4 / P0-1) ── */
  .quick-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 12px 0;
    background: var(--bg-secondary);
    flex-shrink: 0;
  }

  .quick-tabs {
    display: flex;
    gap: 2px;
    background: var(--bg-tertiary);
    padding: 3px;
    border-radius: 9px;
  }

  .quick-tab {
    padding: 5px 14px;
    border: none;
    border-radius: 7px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.12s;
    white-space: nowrap;
  }
  .quick-tab:hover { color: var(--text-primary); }
  .quick-tab.active {
    background: var(--accent);
    color: #fff;
    font-weight: 600;
  }

  .view-toggle {
    display: flex;
    gap: 2px;
  }
  .vbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.12s;
  }
  .vbtn + .vbtn { border-left: none; border-radius: 6px 0 0 6px; }
  .vbtn:first-of-type { border-radius: 6px 0 0 6px; }
  .vbtn:last-of-type { border-radius: 0 6px 6px 0; }
  .vbtn:hover { color: var(--text-primary); background: var(--bg-hover); }
  .vbtn.active {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    border-color: var(--accent-dim);
  }
  .vbtn-icon { font-size: 12px; line-height: 1; }

  /* ── Filter / secondary filter bar ──────────────────── */
  .filter-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-secondary);
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .search-wrapper {
    position: relative;
    flex: 1;
    min-width: 180px;
    max-width: 320px;
  }

  .search-icon {
    position: absolute;
    left: 8px;
    top: 50%;
    transform: translateY(-50%);
    font-size: 12px;
    opacity: 0.5;
    pointer-events: none;
  }

  .search-input {
    width: 100%;
    padding: 5px 10px 5px 28px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
  }
  .search-input:focus { border-color: var(--accent); }

  .select-arrow {
    position: relative;
    display: inline-block;
  }
  .select-arrow::after {
    content: '';
    position: absolute;
    right: 8px;
    top: 50%;
    transform: translateY(-50%);
    width: 0;
    height: 0;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 5px solid var(--text-muted);
    pointer-events: none;
  }
  .filter-select {
    padding: 5px 22px 5px 8px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 12px;
    outline: none;
    cursor: pointer;
    -webkit-appearance: none;
    -moz-appearance: none;
    appearance: none;
  }
  .filter-select option {
    background: var(--bg-primary);
    color: var(--text-primary);
  }
  .sort-select { min-width: 132px; }

  .status-chips {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }

  .chip {
    padding: 3px 8px;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 11px;
    cursor: pointer;
    transition: all 0.12s;
  }
  .chip:hover { background: var(--bg-hover); color: var(--text-primary); }
  .chip-active {
    background: var(--accent);
    color: #fff;
    border-color: var(--accent);
  }

  .searching-indicator {
    padding: 4px 12px;
    font-size: 11px;
    color: var(--text-muted);
    background: var(--bg-tertiary);
    border-bottom: 1px solid var(--border);
  }

  /* ── Cover grid (P0-1) ──────────────────────────────── */
  /* Windowed (perf follow-up): .grid-spacer is sized to the full,
     un-rendered content height so the scrollbar stays accurate; only the
     rows within the viewport (+ overscan) exist in the DOM, each absolutely
     positioned via transform: translateY() to its virtual offset. Padding
     lives on the row itself (not the scroll container) because an
     absolutely-positioned element's containing block is its ancestor's
     padding box, not content box — container padding would otherwise be
     ignored for positioning. */
  .grid-scroll {
    flex: 1;
    overflow-y: auto;
    position: relative;
  }

  .grid-spacer {
    position: relative;
    width: 100%;
  }

  .grid-row {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: grid;
    gap: 18px 16px;
    padding: 0 16px;
    box-sizing: border-box;
  }

  .card {
    cursor: pointer;
    border-radius: 12px;
    outline: none;
    transition: background 0.1s;
    padding: 6px;
    margin: -6px;
  }
  .card:hover { background: var(--bg-hover); }
  .card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .cover-frame {
    position: relative;
    border-radius: 10px;
    overflow: hidden;
    aspect-ratio: 16 / 9;
    background: var(--bg-tertiary);
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.35);
  }
  .cover-frame::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(to top, rgba(0, 0, 0, 0.65), transparent 55%);
    opacity: 0;
    transition: opacity 0.15s;
    pointer-events: none;
  }
  .card:hover .cover-frame::after,
  .card:focus-visible .cover-frame::after,
  .cover-frame:has(.card-play.state-playing)::after,
  .cover-frame:has(.card-play.state-error)::after {
    opacity: 1;
  }

  .cover-img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .cover-ph {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    background: var(--bg-tertiary);
  }
  .cover-ph-icon { font-size: 28px; opacity: 0.3; }

  /* Play-state badges on the cover (update / not installed) */
  .card-badge {
    position: absolute;
    top: 8px;
    padding: 2px 8px;
    border-radius: 10px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.02em;
    pointer-events: none;
    z-index: 2;
  }
  .badge-update {
    left: 8px;
    background: var(--warning);
    color: #1a1a10;
  }
  .badge-virtual {
    right: 8px;
    background: color-mix(in srgb, var(--text-muted) 85%, transparent);
    color: #fff;
  }

  /* Hover Play (Steam/itch card pattern) */
  .card-play {
    position: absolute;
    left: 50%;
    top: 56%;
    transform: translate(-50%, -50%);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 18px;
    border: none;
    border-radius: 8px;
    background: var(--accent);
    color: #fff;
    font-size: 13px;
    font-weight: 700;
    cursor: pointer;
    z-index: 3;
    box-shadow: 0 4px 18px rgba(0, 0, 0, 0.5);
    opacity: 0;
    visibility: hidden;
    transition: opacity 0.15s, transform 0.15s, background 0.12s;
  }
  .card:hover .card-play,
  .card:focus-visible .card-play,
  .card-play.state-playing,
  .card-play.state-error {
    opacity: 1;
    visibility: visible;
  }
  .card:hover .card-play { transform: translate(-50%, -50%) scale(1.04); }
  .card-play:hover { background: var(--accent-hover); }
  .card-play:disabled { opacity: 0.85; cursor: default; }
  .card-play.state-launching { background: var(--accent-dim); }
  .card-play.state-playing {
    background: var(--success);
    color: #07140b;
    opacity: 1;
    visibility: visible;
    animation: pulse-success 1.2s ease-in-out infinite;
  }
  .card-play.state-error { background: var(--danger); opacity: 1; visibility: visible; }

  @keyframes pulse-success {
    0%, 100% { box-shadow: 0 4px 18px rgba(74, 222, 128, 0.35); }
    50%      { box-shadow: 0 4px 26px rgba(74, 222, 128, 0.7); }
  }

  .play-spinner {
    width: 12px;
    height: 12px;
    border: 2px solid rgba(255, 255, 255, 0.5);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .play-spinner.small { width: 10px; height: 10px; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .card-title {
    margin-top: 8px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
    line-height: 1.25;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    min-height: 2.5em;
  }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 4px;
    min-height: 18px;
  }
  .card-engine {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
  }
  .card-last-played {
    font-size: 10px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  /* ── List / table view ─────────────────────────────── */
  .table-header {
    display: grid;
    grid-template-columns: 80px 1fr 110px 130px 80px 100px 64px;
    gap: 8px;
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    border-bottom: 1px solid var(--border);
    background: var(--bg-tertiary);
    flex-shrink: 0;
  }

  .col-sortable {
    display: flex;
    align-items: center;
    gap: 4px;
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    text-transform: inherit;
    letter-spacing: inherit;
    cursor: pointer;
    padding: 0;
    text-align: left;
  }
  .col-sortable:hover { color: var(--text-primary); }

  .sort-arrow {
    font-size: 8px;
    opacity: 0.6;
  }

  .table-body {
    flex: 1;
    overflow-y: auto;
    position: relative;
  }

  .table-spacer {
    position: relative;
    width: 100%;
  }

  /* Windowed (perf follow-up): rows are uniform height, so each is just
     absolutely positioned at its virtual offset — see .grid-row above for
     why the spacer/transform pattern is needed instead of a plain list. */
  .table-row {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: grid;
    grid-template-columns: 80px 1fr 110px 130px 80px 100px 64px;
    gap: 8px;
    padding: 4px 12px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background 0.08s;
    align-items: center;
    box-sizing: border-box;
  }
  .table-row:hover { background: var(--bg-hover); }
  .table-row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .game-title {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .update-dot {
    display: inline-block;
    margin-left: 4px;
    color: var(--warning);
    font-size: 10px;
    vertical-align: super;
  }

  .engine-badge {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
  }

  .version-old {
    text-decoration: line-through;
    opacity: 0.7;
    margin-right: 4px;
  }
  .version-new {
    color: var(--warning);
    font-weight: 600;
    font-size: 12px;
  }

  .col-size { font-size: 12px; color: var(--text-secondary); }

  .col-play {
    display: flex;
    align-items: center;
    justify-content: flex-start;
  }
  .row-play {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border: 1px solid var(--accent-dim);
    border-radius: 6px;
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    color: var(--accent);
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.12s;
    white-space: nowrap;
  }
  .row-play:hover { background: var(--accent); color: #fff; border-color: var(--accent); }
  .row-play:disabled { opacity: 0.7; cursor: default; }
  .row-play.state-playing { background: var(--success); color: #07140b; border-color: var(--success); }
  .row-play.state-error { background: var(--danger); color: #fff; border-color: var(--danger); }

  /* ── Cover Column ───────────────────────────────────── */
  .col-cover {
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 10px;
    color: var(--text-muted);
  }

  .cover-thumb {
    display: block;
    width: 72px;
    height: 40px;
    object-fit: cover;
    border-radius: 3px;
    background: var(--bg-secondary);
    flex-shrink: 0;
  }

  .cover-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 72px;
    height: 40px;
    border-radius: 3px;
    background: var(--bg-tertiary);
    flex-shrink: 0;
  }

  .cover-icon {
    font-size: 16px;
    opacity: 0.3;
  }

  .cover-spinner {
    width: 14px;
    height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  .status-badge {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 500;
  }
  .status-active { background: color-mix(in srgb, var(--success) 15%, transparent); color: var(--success); }
  .status-completed { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); }
  .status-abandoned { background: color-mix(in srgb, var(--text-muted) 15%, transparent); color: var(--text-muted); }
  .status-on_hold { background: color-mix(in srgb, var(--warning) 15%, transparent); color: var(--warning); }
  .status-unknown { background: color-mix(in srgb, var(--text-muted) 10%, transparent); color: var(--text-muted); }

  .text-muted { color: var(--text-muted); }

  /* ── Context Menu ─────────────────────────── */
  .context-menu-overlay {
    position: fixed;
    inset: 0;
    z-index: 1000;
  }
  .context-menu {
    position: fixed;
    z-index: 1001;
    min-width: 180px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 4px;
    box-shadow: 0 8px 32px rgba(0,0,0,0.5);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .ctx-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    text-align: left;
    white-space: nowrap;
  }
  .ctx-item:hover { background: var(--bg-hover); }
  .ctx-item.ctx-danger { color: var(--danger); }
  .ctx-item.ctx-danger:hover { background: color-mix(in srgb, var(--danger) 12%, transparent); }
  .ctx-arrow { margin-left: auto; font-size: 10px; opacity: 0.5; }
  .ctx-back-arrow { font-size: 12px; opacity: 0.7; }
  .ctx-divider {
    height: 1px;
    background: var(--border);
    margin: 3px 4px;
  }
  .ctx-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .ctx-dot-active { background: var(--success); }
  .ctx-dot-completed { background: var(--accent); }
  .ctx-dot-abandoned { background: var(--text-muted); }
  .ctx-dot-on_hold { background: var(--warning); }
  .ctx-dot-unknown { background: var(--text-muted); opacity: 0.5; }
</style>