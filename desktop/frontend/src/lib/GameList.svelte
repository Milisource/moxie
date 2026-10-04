<script>
  import {onMount, onDestroy, tick} from 'svelte'
  import {DropdownMenu} from 'bits-ui'
  import {SearchGames, RemoveGame, SetGameStatus, RenameGame, GetCoverBaseURL, PlayGame} from '../../wailsjs/go/main/App'
  import {GAME_STATUSES, statusLabel} from './statuses.js'
  import {library, setViewMode, setDensity, setCardScale, CARD_SCALE_MIN, CARD_SCALE_MAX} from './viewState.svelte.js'
  import {makeCoverHelpers} from './cover.js'
  import {createVirtualList} from './virtualList.svelte.js'
  import {createLibraryNav} from './useLibraryNav.svelte.js'
  import {confirmAction} from './confirmDialog.svelte.js'
  import {promptAction} from './promptDialog.svelte.js'
  import {openEditGame} from './editGameDialog.svelte.js'
  import {
    QUICK_VIEWS, SORT_OPTIONS,
    quickViewMatches, sortCompare, toggleSort, sortIcon, onSortSelect,
  } from './useLibrarySort.svelte.js'
  import GameGridCard from './GameGridCard.svelte'
  import GameWideCard from './GameWideCard.svelte'
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

  // Timers are tracked so onDestroy can clear them: a revert timer that fires
  // after the component is destroyed would otherwise write to orphaned state.
  const playTimers = new Set()
  function schedulePlayRevert(id, ms) {
    const t = setTimeout(() => {
      playTimers.delete(t)
      playState = {...playState, [id]: {state: 'idle', msg: ''}}
    }, ms)
    playTimers.add(t)
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
      schedulePlayRevert(game.id, PLAY_REVERT_MS)
    } catch (err) {
      const msg = String(err).replace(/^Error:\s*/, '')
      playState = {...playState, [game.id]: {state: 'error', msg}}
      schedulePlayRevert(game.id, PLAY_ERROR_REVERT_MS)
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
  const UPDATE_BUSY_PHASES = ['queued', 'syncing', 'selecting-link', 'downloading', 'extracting', 'merging', 'updating-db']
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
    if (wideEl) wideEl.scrollTop = library.wideScrollTop
  })

  onDestroy(() => {
    clearTimeout(debounceTimer)
    for (const t of playTimers) clearTimeout(t)
    playTimers.clear()
    gridVirtualizer.unsubscribe()
    wideVirtualizer.unsubscribe()
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
  let wideEl = $state.raw()                // wide-card scroll container, bound in markup
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
  // 3:4 aspect ratio off that width — depends on container width. A
  // ResizeObserver on the scroll container recomputes both whenever it
  // changes (window resize, sidebar toggle, view-mode switch).
  // Density (P1 item 7): compact trades card/row size for more items
  // per screen. Grid card width and table row height are read by the
  // virtualizers below, so they're reactive to `library.density`, not
  // just CSS — CSS alone wouldn't change how many rows the virtualizer
  // windows in.
  const GRID_GAP = 16
  // Horizontal padding, both sides — must equal .grid-row's padding-inline
  // (the --gutter clamp(16px, 2vw, 48px)), recomputed from the live width.
  const gridPad = (w) => 2 * Math.min(48, Math.max(16, window.innerWidth * 0.02))
  // Base card width grows with the container (about 176px at a 1440px
  // window, 240px at 2560px) so a 2K screen gets bigger covers instead of
  // just more columns; the toolbar slider scales it, compact shrinks it.
  function baseCardMin(avail) {
    const fluid = Math.min(240, Math.max(176, 176 + (avail - 1190) * 0.057))
    return fluid * library.cardScale * (library.density === 'compact' ? 0.75 : 1)
  }
  // Caption block under the cover. GameGridCard.svelte sets these exact
  // heights in CSS (meta margin + meta row + title margin + 2 title lines);
  // change one, change both, or rows overlap / gap.
  let gridTextHeight = $derived(library.density === 'compact' ? 6 + 16 + 4 + 30 : 8 + 18 + 6 + 34)
  let gridColumns = $state(1)
  let gridRowHeight = $state(280)

  function updateGridLayout() {
    if (!gridEl) return
    const raw = gridEl.clientWidth - gridPad(gridEl.clientWidth)
    const cardMin = baseCardMin(raw)
    const avail = Math.max(raw, cardMin)
    const cols = Math.max(1, Math.floor((avail + GRID_GAP) / (cardMin + GRID_GAP)))
    const cardWidth = (avail - GRID_GAP * (cols - 1)) / cols
    const coverHeight = cardWidth * 4 / 3
    gridColumns = cols
    gridRowHeight = coverHeight + gridTextHeight + GRID_GAP
  }

  $effect(() => {
    if (!gridEl) return
    // Re-run when the size inputs change, not only on resize.
    void library.cardScale, library.density, gridTextHeight
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

  // ── Wide-card grid (F95Zone Latest Alpha style) ───────────────
  // Same windowing model as the cover grid — a fluid column count feeding a
  // uniform row height — but the card is a landscape 16:9 tile with a caption
  // below, so the width floor is wider and the row height is media + caption.
  const WIDE_GAP = 16
  // ~280px card at a 1440px window, capped at 360px on very wide screens; the
  // toolbar Size slider scales it and compact shrinks it.
  function baseWideCardMin(avail) {
    const fluid = Math.min(360, Math.max(280, 280 + (avail - 1190) * 0.06))
    return fluid * library.cardScale * (library.density === 'compact' ? 0.8 : 1)
  }
  // Caption block under the media. GameWideCard.svelte sets these exact heights
  // in CSS (title margin + 2 title lines + stats margin + stats row); change
  // one, change both, or rows overlap / gap.
  let wideTextHeight = $derived(library.density === 'compact' ? 4 + 30 + 3 + 14 : 6 + 34 + 4 + 16)
  let wideColumns = $state(1)
  let wideRowHeight = $state(240)

  function updateWideLayout() {
    if (!wideEl) return
    const raw = wideEl.clientWidth - gridPad(wideEl.clientWidth)
    const cardMin = baseWideCardMin(raw)
    const avail = Math.max(raw, cardMin)
    const cols = Math.max(1, Math.floor((avail + WIDE_GAP) / (cardMin + WIDE_GAP)))
    const cardWidth = (avail - WIDE_GAP * (cols - 1)) / cols
    const mediaHeight = cardWidth * 9 / 16
    wideColumns = cols
    wideRowHeight = mediaHeight + wideTextHeight + WIDE_GAP
  }

  $effect(() => {
    if (!wideEl) return
    // Re-run when the size inputs change, not only on resize.
    void library.cardScale, library.density, wideTextHeight
    updateWideLayout()
    const ro = new ResizeObserver(() => updateWideLayout())
    ro.observe(wideEl)
    return () => ro.disconnect()
  })

  // Chunk the flat `displayed` list into fixed-size rows so the virtualizer
  // only has to window rows, not think about wrapping.
  let wideRows = $derived.by(() => {
    const rows = []
    for (let i = 0; i < displayed.length; i += wideColumns) {
      rows.push(displayed.slice(i, i + wideColumns))
    }
    return rows
  })

  const wideVirtualizer = createVirtualList({
    count: 0,
    getScrollElement: () => wideEl,
    estimateSize: () => wideRowHeight,
    overscan: 3,
  })

  $effect(() => {
    const el = wideEl
    const rows = wideRows
    const rowHeight = wideRowHeight
    wideVirtualizer.setOptions({
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
  // DropdownMenu anchored to a shared hidden 0x0 element (ctxAnchorEl)
  // repositioned before opening — avoids instrumenting every virtualized
  // row with its own trigger, and gets floating-ui collision detection
  // (avoidCollisions) instead of the old hand-rolled menuW/menuH clamp.
  let ctxMenuOpen = $state(false)
  let contextGame = $state(null)
  let ctxAnchorEl = $state(null)

  function openContextMenuAt(x, y, game) {
    contextGame = game
    if (ctxAnchorEl) {
      ctxAnchorEl.style.left = `${x}px`
      ctxAnchorEl.style.top = `${y}px`
    }
    ctxMenuOpen = true
  }

  function onRowContextMenu(e, game) {
    e.preventDefault()
    openContextMenuAt(e.clientX, e.clientY, game)
  }

  // ── Keyboard grid/list navigation + context-menu parity (P2 item 10) ──
  // Roving tabindex + arrow-key nav lives in useLibraryNav.svelte.js — this
  // just wires it to the grid/table DOM and virtualizers it owns.
  const nav = createLibraryNav({
    getDisplayed: () => displayed,
    getViewMode: () => library.viewMode,
    getGridColumns: () => gridColumns,
    getWideColumns: () => wideColumns,
    getContainer: () => (library.viewMode === 'grid' ? gridEl : library.viewMode === 'wide' ? wideEl : tableBodyEl),
    scrollToIndex: (nextIdx) => {
      if (library.viewMode === 'grid') {
        gridVirtualizer.scrollToIndex(Math.floor(nextIdx / gridColumns), {align: 'auto'})
      } else if (library.viewMode === 'wide') {
        wideVirtualizer.scrollToIndex(Math.floor(nextIdx / wideColumns), {align: 'auto'})
      } else {
        tableVirtualizer.scrollToIndex(nextIdx, {align: 'auto'})
      }
    },
    onOpenContextMenu: (rect, game) => {
      openContextMenuAt(rect.left, rect.bottom + 4, game)
    },
  })

  async function handleStatus(status) {
    if (!contextGame) return
    const g = contextGame
    ctxMenuOpen = false
    try {
      await SetGameStatus(g.id, status)
      await onUpdate()
    } catch (e) {
      console.error('Failed to set status:', e)
    }
  }

  // Opens the shared Edit Game dialog. Title edits inside it are
  // metadata-only; the "Rename Folder…" item below still moves the directory.
  async function handleEdit() {
    if (!contextGame) return
    const g = contextGame
    ctxMenuOpen = false
    const saved = await openEditGame(g.id)
    if (saved) await onUpdate()
  }

  async function handleRename() {
    if (!contextGame) return
    const g = contextGame
    ctxMenuOpen = false
    const newTitle = await promptAction({title: 'Rename game folder', label: 'Enter new title:', defaultValue: g.title})
    if (!newTitle || newTitle === g.title) return
    try {
      await RenameGame(g.id, newTitle)
      await onUpdate()
    } catch (e) {
      console.error('Failed to rename:', e)
    }
  }

  async function handleRemove() {
    if (!contextGame) return
    const g = contextGame
    ctxMenuOpen = false
    const ok = await confirmAction({
      title: 'Remove game?',
      description: `Are you sure you want to remove "${g.title}" from your library?`,
      confirmLabel: 'Remove',
      danger: true,
    })
    if (!ok) return
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
            class:active={library.viewMode === 'wide'}
            aria-pressed={library.viewMode === 'wide'}
            title="Wide card view — F95Zone-style landscape tiles"
            onclick={() => setViewMode('wide')}
          ><span class="vbtn-icon">▭</span>Wide</button>
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

        {#if library.viewMode === 'grid' || library.viewMode === 'wide'}
          <label class="size-slider" title="Cover size">
            <span class="label">Size</span>
            <input
              type="range"
              min={CARD_SCALE_MIN}
              max={CARD_SCALE_MAX}
              step="0.05"
              value={library.cardScale}
              oninput={(e) => setCardScale(e.currentTarget.value)}
              ondblclick={() => setCardScale(1)}
            />
          </label>
        {/if}
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
    {:else if library.viewMode === 'wide'}
      <!-- ── Wide cards (F95Zone Latest Alpha style, P2 item) ── -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="grid-scroll"
        bind:this={wideEl}
        onscroll={(e) => library.wideScrollTop = e.currentTarget.scrollTop}
        onkeydown={nav.onNavKeydown}
      >
        <div class="grid-spacer" style="height: {wideVirtualizer.totalSize}px">
          {#each wideVirtualizer.virtualRows as vRow (vRow.key)}
            <div
              class="grid-row"
              style="grid-template-columns: repeat({wideColumns}, 1fr); height: {wideRowHeight - WIDE_GAP}px; transform: translateY({vRow.start + WIDE_GAP}px)"
            >
              {#each wideRows[vRow.index] ?? [] as game (game.id)}
                <GameWideCard
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
        <span class="col-wide">Last played</span>
        <span class="col-wide">Added</span>
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

    <!-- Context Menu — anchored to a shared hidden element repositioned
         before opening (see openContextMenuAt), so neither GameGridCard
         nor GameTableRow needs its own DropdownMenu.Trigger. -->
    <div bind:this={ctxAnchorEl} class="context-menu-anchor"></div>
    <DropdownMenu.Root bind:open={ctxMenuOpen}>
      <DropdownMenu.Portal>
        <DropdownMenu.Content class="context-menu" customAnchor={ctxAnchorEl} side="bottom" align="start">
          <DropdownMenu.Item class="ctx-item" onSelect={handleEdit}>Edit Game…</DropdownMenu.Item>
          <DropdownMenu.Sub>
            <DropdownMenu.SubTrigger class="ctx-item">
              <span>Set Status</span>
              <span class="ctx-arrow">▸</span>
            </DropdownMenu.SubTrigger>
            <DropdownMenu.Portal>
              <DropdownMenu.SubContent class="context-menu">
                {#each GAME_STATUSES as s}
                  <DropdownMenu.Item class="ctx-item" onSelect={() => handleStatus(s)}>
                    <span class="ctx-dot ctx-dot-{s}"></span>
                    {statusLabel(s)}
                  </DropdownMenu.Item>
                {/each}
              </DropdownMenu.SubContent>
            </DropdownMenu.Portal>
          </DropdownMenu.Sub>
          <DropdownMenu.Item class="ctx-item" onSelect={handleRename}>Rename Folder…</DropdownMenu.Item>
          <DropdownMenu.Separator class="ctx-divider" />
          <DropdownMenu.Item class="ctx-item ctx-danger" onSelect={handleRemove}>Remove</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
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
  .empty-icon { font-size: var(--text-4xl); opacity: 0.5; }
  .empty-title { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); }
  .empty-desc { font-size: var(--text-base); }

  /* ── Quick view tabs + layout toggle (P0-4 / P0-1) ── */
  .quick-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 12px 0;
    background: var(--bg-secondary);
    flex-shrink: 0;
    /* Three layout buttons + density + size slider no longer fit beside the
       quick-view tabs on narrow windows; wrap instead of clipping the slider. */
    flex-wrap: wrap;
  }

  .quick-tabs {
    display: flex;
    gap: 2px;
    background: var(--bg-tertiary);
    padding: 3px;
    border-radius: var(--radius-1);
  }

  .quick-tab {
    padding: 5px 14px;
    border: none;
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-sm);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.12s;
    white-space: nowrap;
  }
  .quick-tab:hover { color: var(--text-primary); }
  .quick-tab.active {
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 600;
  }

  .toggle-group {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
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
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--text-xs);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.12s;
  }
  .vbtn + .vbtn { border-left: none; border-radius: var(--radius-1) 0 0 var(--radius-1); }
  .vbtn:first-of-type { border-radius: var(--radius-1) 0 0 var(--radius-1); }
  .vbtn:last-of-type { border-radius: 0 var(--radius-1) var(--radius-1) 0; }
  .vbtn:hover { color: var(--text-primary); background: var(--bg-hover); }
  .vbtn.active {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    border-color: var(--accent-dim);
  }
  .vbtn-icon { font-size: var(--text-sm); line-height: 1; }

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
    font-size: var(--text-sm);
    opacity: 0.5;
    pointer-events: none;
  }

  .search-input {
    width: 100%;
    padding: 5px 10px 5px 28px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-base);
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
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-sm);
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
    font-size: var(--text-xs);
    color: var(--text-muted);
    white-space: nowrap;
    flex-shrink: 0;
  }

  .chip {
    padding: 3px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-xs);
    cursor: pointer;
    transition: all 0.12s;
  }
  .chip:hover { background: var(--bg-hover); color: var(--text-primary); }
  .chip-active {
    background: var(--accent);
    color: var(--on-accent);
    border-color: var(--accent);
  }

  .searching-indicator {
    padding: 4px 12px;
    font-size: var(--text-xs);
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
  /* List columns, shared by .table-header and GameTableRow's .table-row.
     Wide windows get Last played / Added instead of a stretched title. */
  .game-list {
    --table-cols: 56px minmax(220px, 1fr) 110px 180px 90px 110px 72px;
  }
  .game-list :global(.col-wide) { display: none; }
  @media (min-width: 1700px) {
    .game-list {
      --table-cols: 56px minmax(260px, 1fr) 120px 200px 100px 120px 130px 120px 80px;
    }
    .game-list :global(.col-wide) { display: block; }
  }

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
    gap: 0 16px;
    padding: 0 var(--gutter);
    box-sizing: border-box;
  }

  .size-slider {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .size-slider input {
    width: 120px;
    accent-color: var(--accent);
  }

  /* ── Density: compact (P1 item 7) ──────────────────────
     Grid-row gap mirrors gridTextHeight in <script> — change one, change
     the other, or the virtualizer windows the wrong number of rows for
     what's actually on screen. Per-card density rules live in
     GameGridCard.svelte; per-row rules live in GameTableRow.svelte. */
  /* Column gap stays GRID_GAP in both densities (the width math assumes it). */

  /* ── List / table view ─────────────────────────────── */
  .table-header {
    display: grid;
    grid-template-columns: var(--table-cols);
    gap: 8px;
    padding: 6px 12px;
    font-size: var(--text-xs);
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
    font-size: var(--text-2xs);
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
  /* Hidden 0x0 anchor repositioned before opening (see openContextMenuAt in
     the script) — DropdownMenu.Content's customAnchor points at this. It is a
     real element in this template, so it keeps normal scoping. */
  .context-menu-anchor {
    position: fixed;
    width: 0;
    height: 0;
  }
  /* The menu itself is rendered by bits-ui (DropdownMenu.Content/Item/…) and
     portaled to <body>. Svelte 5 does not put this component's scope hash on
     elements a child component renders from a `class` prop, so scoped
     selectors never match the portaled markup — the menu mounted with no
     styles at all (transparent background, no border/padding, min-width
     ignored) and was effectively invisible over the dark theme. These rules
     MUST stay :global or the menu goes invisible again. */
  :global(.context-menu) {
    z-index: 1001;
    min-width: 180px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    padding: 4px;
    box-shadow: var(--shadow-pop);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  :global(.ctx-item) {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    border: none;
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-base);
    cursor: pointer;
    text-align: left;
    white-space: nowrap;
  }
  :global(.ctx-item:hover),
  :global(.ctx-item[data-highlighted]) { background: var(--bg-hover); }
  :global(.ctx-item.ctx-danger) { color: var(--danger); }
  :global(.ctx-item.ctx-danger:hover),
  :global(.ctx-item.ctx-danger[data-highlighted]) { background: color-mix(in srgb, var(--danger) 12%, transparent); }
  :global(.ctx-item[data-disabled]) { opacity: 0.4; cursor: not-allowed; }
  :global(.ctx-arrow) { margin-left: auto; font-size: var(--text-2xs); opacity: 0.5; }
  :global(.ctx-divider) {
    height: 1px;
    background: var(--border);
    margin: 3px 4px;
  }
  :global(.ctx-dot) {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  :global(.ctx-dot-active) { background: var(--success); }
  :global(.ctx-dot-completed) { background: var(--accent); }
  :global(.ctx-dot-abandoned) { background: var(--text-muted); }
  :global(.ctx-dot-on_hold) { background: var(--warning); }
  :global(.ctx-dot-unknown) { background: var(--text-muted); opacity: 0.5; }
</style>
