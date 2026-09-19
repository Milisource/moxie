<script>
  import {onMount, onDestroy, tick} from 'svelte'
  import {SearchGames, RemoveGame, SetGameStatus, RenameGame, GetCoverBaseURL, PlayGame} from '../../wailsjs/go/main/App'
  import {GAME_STATUSES, statusLabel} from './statuses.js'
  import {library, setViewMode, setDensity} from './viewState.svelte.js'
  import {makeCoverHelpers} from './cover.js'
  import {createVirtualList} from './virtualList.svelte.js'
  import {createLibraryNav} from './useLibraryNav.svelte.js'
  import {
    QUICK_VIEWS, SORT_OPTIONS,
    quickViewMatches, sortCompare, toggleSort, sortIcon, onSortSelect,
  } from './useLibrarySort.svelte.js'
  import GameGridCard from './GameGridCard.svelte'
  import GameTableRow from './GameTableRow.svelte'

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
  // `gameStates` is replaced wholesale on every download/extract progress
  // tick (up to 5x/sec while any update is running), but only phase
  // *transitions* actually change which games are "busy". Without this
  // guard, the 'ready' quick view's `displayed` derived below would
  // re-filter + re-sort the entire library on every percent tick. Track just
  // the busy-id membership and only touch this $state (and thus retrigger
  // `displayed`) when that membership set actually changes.
  const UPDATE_BUSY_PHASES = ['syncing', 'selecting-link', 'downloading', 'extracting', 'merging', 'updating-db']
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

  const {coverSrc, markFailed} = makeCoverHelpers(() => coverBase)

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
    gridVirtualizer.unsubscribe()
    tableVirtualizer.unsubscribe()
  })

  // ── Search & Filters ──────────────────────────────────────────
  // library.search / filters / sort live in viewState so they survive tab
  // switches; only the in-flight request state is local.
  let debounceTimer                        // plain var, not reactive
  let searchResults = $state(null)         // null = use full list, array = search results
  let isSearching = $state(false)
  let tableBodyEl = $state.raw()           // list scroll container, bound in markup
  let gridEl = $state.raw()                // grid scroll container, bound in markup
  let searchInputEl = $state.raw()         // search field, bound in markup

  // Exposed to App.svelte via bind:this so a global "/" / Ctrl+F shortcut
  // (P1 item 8) can jump into this field even when this view is being
  // freshly mounted (the tab switch happens first, then this runs).
  export function focusSearch() {
    searchInputEl?.focus()
    searchInputEl?.select()
  }

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

  // ── Derived: quick-view-filtered + sorted list ───────────────
  // Sort options/comparator/quick-view predicates live in
  // useLibrarySort.svelte.js — see that module for the arrangement rules.
  let displayed = $derived.by(() => {
    // 1. Use search results if available, else full list
    let list = searchResults ?? games

    // 2. Filter by quick view (primary axis)
    if (library.quickView !== 'all') {
      list = list.filter(g => quickViewMatches(library.quickView, g, busyIds))
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
  // Density (P1 item 7): compact trades card/row size for more items
  // per screen. Grid card width and table row height are read by the
  // virtualizers below, so they're reactive to `library.density`, not
  // just CSS — CSS alone wouldn't change how many rows the virtualizer
  // windows in.
  const GRID_GAP = 16
  const GRID_PAD = 32                // 16px horizontal padding, both sides
  let gridCardMin = $derived(library.density === 'compact' ? 132 : 176)
  // title margin + 2-line title + meta margin + meta row
  let gridTextHeight = $derived(library.density === 'compact' ? 6 + 32 + 3 + 16 : 8 + 40 + 4 + 18)
  let gridColumns = $state(1)
  let gridRowHeight = $state(280)

  function updateGridLayout() {
    if (!gridEl) return
    const cardMin = gridCardMin
    const avail = Math.max(gridEl.clientWidth - GRID_PAD, cardMin)
    const cols = Math.max(1, Math.floor((avail + GRID_GAP) / (cardMin + GRID_GAP)))
    const cardWidth = (avail - GRID_GAP * (cols - 1)) / cols
    const coverHeight = cardWidth * 9 / 16
    gridColumns = cols
    gridRowHeight = coverHeight + gridTextHeight + GRID_GAP
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

  // See virtualList.svelte.js for why this isn't a plain reactive $store read.
  const gridVirtualizer = createVirtualList({
    count: 0,
    getScrollElement: () => gridEl,
    estimateSize: () => gridRowHeight,
    overscan: 3,
  })

  $effect(() => {
    const el = gridEl
    const rows = gridRows
    const rowHeight = gridRowHeight
    gridVirtualizer.setOptions({
      count: rows.length,
      getScrollElement: () => el,
      estimateSize: () => rowHeight,
      overscan: 3,
      getItemKey: (i) => rows[i]?.map(g => g.id).join(',') ?? i,
    })
  })

  // Table rows are uniform height, so windowing is simpler — one measurement
  // for the whole list. Comfortable matches the row's rendered height: 40px
  // cover thumb + 4px top/bottom padding + 1px border. Compact drops the
  // thumb/padding for a denser data-table feel (P1 item 7).
  let tableRowHeight = $derived(library.density === 'compact' ? 34 : 49)

  const tableVirtualizer = createVirtualList({
    count: 0,
    getScrollElement: () => tableBodyEl,
    estimateSize: () => tableRowHeight,
    overscan: 8,
  })

  $effect(() => {
    const el = tableBodyEl
    const rows = displayed
    const rowHeight = tableRowHeight
    tableVirtualizer.setOptions({
      count: rows.length,
      getScrollElement: () => el,
      estimateSize: () => rowHeight,
      overscan: 8,
      getItemKey: (i) => rows[i]?.id ?? i,
    })
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

  // ── Context Menu ──────────────────────────────────────────────
  let contextMenu = $state(null)     // {x, y, game} or null
  let contextMenuView = $state('main') // 'main' | 'status'

  function positionContextMenu(x, y, game) {
    const menuW = 200, menuH = 220
    contextMenu = {
      x: Math.min(x, window.innerWidth - menuW),
      y: Math.min(y, window.innerHeight - menuH),
      game,
    }
    contextMenuView = 'main'
  }

  function onRowContextMenu(e, game) {
    e.preventDefault()
    closeContextMenu()
    positionContextMenu(e.clientX, e.clientY, game)
  }

  function closeContextMenu() {
    contextMenu = null
    contextMenuView = 'main'
  }

  // ── Keyboard grid/list navigation + context-menu parity (P2 item 10) ──
  // Roving tabindex + arrow-key nav lives in useLibraryNav.svelte.js — this
  // just wires it to the grid/table DOM and virtualizers it owns.
  const nav = createLibraryNav({
    getDisplayed: () => displayed,
    getViewMode: () => library.viewMode,
    getGridColumns: () => gridColumns,
    getContainer: () => (library.viewMode === 'grid' ? gridEl : tableBodyEl),
    scrollToIndex: (nextIdx) => {
      if (library.viewMode === 'grid') {
        gridVirtualizer.scrollToIndex(Math.floor(nextIdx / gridColumns), {align: 'auto'})
      } else {
        tableVirtualizer.scrollToIndex(nextIdx, {align: 'auto'})
      }
    },
    onOpenContextMenu: (rect, game) => {
      closeContextMenu()
      positionContextMenu(rect.left, rect.bottom + 4, game)
    },
  })

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

  // Zero results already get a curated empty state above; a "few" results
  // (1 or more, but fewer than the full library) had no feedback at all —
  // a search/filter narrowing to 1-2 games looked identical to an unfiltered
  // sparse library, with no cue that a filter was even active. This surfaces
  // the match count whenever something is actually filtering the list.
  let hasActiveFilter = $derived(
    library.search.trim().length >= 2 ||
    library.quickView !== 'all' ||
    (library.engine && library.engine !== 'All') ||
    (library.status && library.status !== 'All')
  )
</script>

<div class="game-list" class:density-compact={library.density === 'compact'}>
  {#if games.length === 0 && !isSearching && !loading}
    <div class="empty">
      <div class="empty-icon">▦</div>
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

      <div class="toggle-group">
        <div class="view-toggle" role="group" aria-label="Library layout">
          <button
            class="vbtn"
            class:active={library.viewMode === 'grid'}
            aria-pressed={library.viewMode === 'grid'}
            title="Grid view"
            onclick={() => setViewMode('grid')}
          ><span class="vbtn-icon">▦</span>Grid</button>
          <button
            class="vbtn"
            class:active={library.viewMode === 'list'}
            aria-pressed={library.viewMode === 'list'}
            title="List view"
            onclick={() => setViewMode('list')}
          ><span class="vbtn-icon">☰</span>List</button>
        </div>

        <div class="view-toggle" role="group" aria-label="Library density">
          <button
            class="vbtn"
            class:active={library.density === 'comfortable'}
            aria-pressed={library.density === 'comfortable'}
            title="Comfortable density"
            onclick={() => setDensity('comfortable')}
          ><span class="vbtn-icon">⊟</span>Comfortable</button>
          <button
            class="vbtn"
            class:active={library.density === 'compact'}
            aria-pressed={library.density === 'compact'}
            title="Compact density"
            onclick={() => setDensity('compact')}
          ><span class="vbtn-icon">≡</span>Compact</button>
        </div>
      </div>
    </div>

    <!-- Secondary filters: search · engine · arrangement · status chips -->
    <div class="filter-bar">
      <div class="search-wrapper">
        <span class="search-icon">⌕</span>
        <input
          type="text"
          class="search-input"
          placeholder="Search titles, tags, developers…"
          bind:this={searchInputEl}
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

      {#if hasActiveFilter && displayed.length > 0}
        <span class="result-count">{displayed.length} of {games.length}</span>
      {/if}
    </div>

    {#if displayed.length === 0}
      <div class="empty small">
        <p class="empty-title">{emptyTitle}</p>
        <p class="empty-desc">{emptyHint}</p>
      </div>
    {:else if library.viewMode === 'grid'}
      <!-- ── Cover grid (default library presentation, P0-1) ── -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="grid-scroll"
        bind:this={gridEl}
        onscroll={(e) => library.gridScrollTop = e.currentTarget.scrollTop}
        onkeydown={nav.onNavKeydown}
      >
        <div class="grid-spacer" style="height: {gridVirtualizer.totalSize}px">
          {#each gridVirtualizer.virtualRows as vRow (vRow.key)}
            <div
              class="grid-row"
              style="grid-template-columns: repeat({gridColumns}, 1fr); height: {gridRowHeight - GRID_GAP}px; transform: translateY({vRow.start + GRID_GAP}px)"
            >
              {#each gridRows[vRow.index] ?? [] as game (game.id)}
                <GameGridCard
                  {game}
                  {playControl}
                  isRoving={nav.rovingId === game.id}
                  {coverBase}
                  {coverSrc}
                  {markFailed}
                  onOpenDetail={() => onOpenDetail(game.id)}
                  onFocus={() => nav.setFocused(game.id)}
                  onContextMenu={(e) => onRowContextMenu(e, game)}
                  onPlay={(e) => handlePlay(e, game)}
                />
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

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="table-body"
        bind:this={tableBodyEl}
        onscroll={(e) => library.scrollTop = e.currentTarget.scrollTop}
        onkeydown={nav.onNavKeydown}
      >
        <div class="table-spacer" style="height: {tableVirtualizer.totalSize}px">
          {#each tableVirtualizer.virtualRows as vItem (vItem.key)}
            {@const game = displayed[vItem.index]}
            {#if game}
              <GameTableRow
                {game}
                {playControl}
                isRoving={nav.rovingId === game.id}
                top={vItem.start}
                height={tableRowHeight}
                {coverBase}
                {coverSrc}
                {markFailed}
                onOpenDetail={() => onOpenDetail(game.id)}
                onFocus={() => nav.setFocused(game.id)}
                onContextMenu={(e) => onRowContextMenu(e, game)}
                onPlay={(e) => handlePlay(e, game)}
              />
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

  .toggle-group {
    display: flex;
    align-items: center;
    gap: var(--space-4);
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

  .result-count {
    margin-left: auto;
    padding-left: 8px;
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
    flex-shrink: 0;
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

  /* ── Density: compact (P1 item 7) ──────────────────────
     Grid-row gap mirrors gridTextHeight in <script> — change one, change
     the other, or the virtualizer windows the wrong number of rows for
     what's actually on screen. Per-card density rules live in
     GameGridCard.svelte; per-row rules live in GameTableRow.svelte. */
  .density-compact .grid-row { gap: 10px 12px; }

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
