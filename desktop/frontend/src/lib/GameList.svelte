<script>
  import {onMount, onDestroy, tick} from 'svelte'
  import {DropdownMenu, Popover} from 'bits-ui'
  import {SearchGames, RemoveGame, SetGameStatus, RenameGame, GetCoverBaseURL, PlayGame, GetCollections, AddGameToCollection} from '../../wailsjs/go/main/App'
  import {GAME_STATUSES, statusLabel} from './statuses.js'
  import {library, setViewMode, setDensity, setCardScale, CARD_SCALE_MIN, CARD_SCALE_MAX} from './viewState.svelte.js'
  import {makeCoverHelpers, coverRev} from './cover.js'
  import {createVirtualList} from './virtualList.svelte.js'
  import {createLibraryNav} from './useLibraryNav.svelte.js'
  import {confirmAction} from './confirmDialog.svelte.js'
  import {promptAction} from './promptDialog.svelte.js'
  import {openEditGame} from './editGameDialog.svelte.js'
  import {
    QUICK_VIEWS, SORT_OPTIONS,
    quickViewMatches, sortCompare, toggleSort, sortIcon, onSortSelect,
    lastPlayedDate, relativePlayed,
  } from './useLibrarySort.svelte.js'
  import GameGridCard from './GameGridCard.svelte'
  import GameWideCard from './GameWideCard.svelte'
  import GameTableRow from './GameTableRow.svelte'
  import {activityFor} from './activity.js'
  import {engineColor} from './engineColors.js'

  let {
    games = [],
    loading = false,
    gameStates = {},
    installState = null,
    lastSyncAt = '',
    onOpenDetail = (id) => {},
    onUpdate = () => {},
    onLaunched = (msg) => {},
    onScan = () => {},
    onCollectionsChanged = () => {},
  } = $props()

  let filtersOpen = $state(false)
  let viewOpen = $state(false)

  // Header summary: count + recency, and the filtered subset when a filter is
  // active. The clock is only read at render; a minute-stale "2h ago" is fine.
  function relativeSync(iso) {
    if (!iso) return null
    const mins = Math.floor((Date.now() - new Date(iso).getTime()) / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    return `${Math.floor(hours / 24)}d ago`
  }
  let syncLabel = $derived(relativeSync(lastSyncAt))

  let activeFilterCount = $derived(
    (library.engine && library.engine !== 'All' ? 1 : 0) + (library.status ? 1 : 0)
  )
  let viewLabel = $derived(
    library.viewMode === 'grid' ? 'Grid' : library.viewMode === 'wide' ? 'Wide' : 'List'
  )
  let viewIcon = $derived(
    library.viewMode === 'grid' ? '▦' : library.viewMode === 'wide' ? '▭' : '☰'
  )

  function clearFilters() {
    library.engine = 'All'
    library.status = ''
  }

  function clearAllFilters() {
    library.search = ''
    library.engine = 'All'
    library.status = ''
    library.quickView = 'all'
    doSearch('')
    searchInputEl?.focus()
  }

  // ── Collections, drag-to-collection, bulk select (P2-3) ─────
  // Collections are fetched when the library mounts so the drop tray can open
  // instantly on dragstart (a network/IPC round-trip mid-drag would miss the
  // drop). Failure is silent: drag-to-collection is an accelerator, and the
  // Collections tab remains the authoritative surface.
  let collections = $state([])
  let selectMode = $state(false)
  let selectedIds = $state(new Set())
  let dragGameId = $state(null)
  let bulkBusy = $state(false)
  let bulkCollectionId = $state('')

  async function loadCollections() {
    try {
      collections = (await GetCollections()) || []
    } catch (e) {
      collections = []
    }
  }
  loadCollections()

  let selectedGames = $derived(games.filter(g => selectedIds.has(g.id)))
  let selectedCount = $derived(selectedGames.length)

  // A filtered-away selection must never be bulk-removed or bulk-added
  // invisibly. Any change to the query clears it; the user re-selects what
  // they can actually see.
  $effect(() => {
    library.search
    library.quickView
    library.engine
    library.status
    selectedIds = new Set()
  })

  function toggleSelectMode() {
    selectMode = !selectMode
    if (!selectMode) selectedIds = new Set()
  }

  function toggleSelected(id) {
    const next = new Set(selectedIds)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selectedIds = next
  }

  function selectAllDisplayed() {
    selectedIds = new Set(displayed.map(g => g.id))
  }

  function clearSelection() {
    selectedIds = new Set()
  }

  // Cards stay single-purpose: in select mode the whole card toggles the
  // selection instead of navigating, so there is no invisible mode-dependent
  // second affordance.
  function handleOpen(game) {
    if (selectMode) toggleSelected(game.id)
    else onOpenDetail(game.id)
  }

  function beginDrag(e, game) {
    if (selectMode) return
    dragGameId = game.id
    try {
      e.dataTransfer.effectAllowed = 'copy'
      e.dataTransfer.setData('text/plain', String(game.id))
    } catch (err) { /* some environments restrict setData */ }
  }

  function endDrag() {
    dragGameId = null
  }

  $effect(() => {
    if (dragGameId === null) return
    const clear = () => { dragGameId = null }
    document.addEventListener('dragend', clear)
    document.addEventListener('drop', clear)
    window.addEventListener('blur', clear)
    return () => {
      document.removeEventListener('dragend', clear)
      document.removeEventListener('drop', clear)
      window.removeEventListener('blur', clear)
    }
  })

  async function dropOnCollection(collectionId) {
    const id = dragGameId
    dragGameId = null
    if (id == null) return
    try {
      await AddGameToCollection(id, collectionId)
      const c = collections.find(x => x.id === collectionId)
      onLaunched(`Added to ${c?.name || 'collection'}`)
      onCollectionsChanged()
    } catch (e) {
      onLaunched(`Couldn't add to collection — ${String(e).replace(/^Error:\s*/, '')}`)
    }
  }

  async function bulkAddToCollection() {
    if (!bulkCollectionId || selectedCount === 0 || bulkBusy) return
    bulkBusy = true
    let added = 0
    try {
      for (const g of selectedGames) {
        try {
          await AddGameToCollection(g.id, Number(bulkCollectionId))
          added++
        } catch (e) { /* count what actually landed */ }
      }
      const c = collections.find(x => x.id === Number(bulkCollectionId))
      onLaunched(`Added ${added} game${added === 1 ? '' : 's'} to ${c?.name || 'collection'}`)
      onCollectionsChanged()
    } finally {
      bulkBusy = false
    }
    clearSelection()
  }

  async function bulkRemove() {
    if (selectedCount === 0 || bulkBusy) return
    const n = selectedCount
    const ok = await confirmAction({
      title: `Remove ${n} game${n === 1 ? '' : 's'}?`,
      description: 'They move to Trash and keep their saves and metadata until purged.',
      confirmLabel: 'Remove',
      danger: true,
    })
    if (!ok) return
    bulkBusy = true
    try {
      for (const g of selectedGames) {
        try { await RemoveGame(g.id, false) } catch (e) { /* keep going */ }
      }
      onLaunched(`Removed ${n} game${n === 1 ? '' : 's'} — restore from Trash`)
      clearSelection()
      selectMode = false
      await onUpdate()
    } finally {
      bulkBusy = false
    }
  }

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

  // Per-tile pipeline badge. Reads the same App-level state the Updates view
  // uses, so a card, a row, and the status bar never disagree.
  function tileActivity(game) {
    return activityFor(game.id, gameStates[game.id], installState)
  }

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

  // Available statuses (shared with the context menu via lib/statuses.js).
  // No "All" chip: an empty selection is the all state, and clicking the
  // active chip clears it.
  const statuses = GAME_STATUSES

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

  // ── Continue playing (P2-1) ─────────────────────────────────
  // A single shelf above the grid, built from the same lastPlayed data the
  // Recently played arrangement uses. It disappears whenever the user is
  // searching or filtering so the shelf never competes with a deliberate
  // query.
  let continuePlaying = $derived.by(() => {
    if (library.quickView !== 'all') return []
    if (library.search.trim().length > 0) return []
    if (library.engine && library.engine !== 'All') return []
    if (library.status) return []
    return games
      .filter(g => g.lastPlayed)
      .sort((a, b) => new Date(b.lastPlayed) - new Date(a.lastPlayed))
      .slice(0, 5)
  })

  function summaryText() {
    if (loading && games.length === 0) return 'Loading library…'
    const count = hasActiveFilter
      ? `${displayed.length} of ${games.length} games`
      : `${games.length} game${games.length === 1 ? '' : 's'}`
    return syncLabel ? `${count} · synced ${syncLabel}` : count
  }
</script>

<div class="game-list" class:density-compact={library.density === 'compact'}>
  <header class="view-header">
    <div class="view-heading">
      <h1 class="view-title">Library</h1>
      <p class="view-summary">{summaryText()}</p>
    </div>
    <button class="ghost-btn" onclick={onScan} title="Scan a folder for games">+ Scan folder</button>
  </header>

  {#if continuePlaying.length > 0}
    <section class="continue" aria-label="Continue playing">
      <div class="continue-head">
        <h2 class="continue-title">Continue playing</h2>
        <button class="continue-all" onclick={() => library.quickView = 'recent'}>Recently played →</button>
      </div>
      <div class="continue-row">
        {#each continuePlaying as game (game.id)}
          <div class="continue-card">
            <button class="continue-open" onclick={() => onOpenDetail(game.id)} title="Open {game.title}">
              <span class="continue-cover">
                {#if game.hasCover && coverBase}
                  <img
                    src={coverSrc(game.id, 'thumb', coverRev(game))}
                    alt=""
                    loading="lazy"
                    decoding="async"
                    onerror={() => markFailed(game.id)}
                  />
                {:else}
                  <span class="continue-ph">{game.title.slice(0, 2).toUpperCase()}</span>
                {/if}
              </span>
              <span class="continue-meta">
                <span class="continue-name" title={game.title}>{game.title}</span>
                <span class="continue-sub">
                  {#if game.engine}
                    <span class="continue-dot" style="--ec: {engineColor(game.engine)}"></span>
                    <span class="continue-engine">{game.engine}</span>
                    <span class="continue-sep">·</span>
                  {/if}
                  {lastPlayedDate(game) ? relativePlayed(lastPlayedDate(game)) : 'recently'}
                </span>
              </span>
            </button>
            <button
              class="continue-play"
              title="Play {game.title}"
              onclick={(e) => handlePlay(e, game)}
            >▶︎</button>
          </div>
        {/each}
      </div>
    </section>
  {/if}

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

    <!-- One toolbar row: quick views · search · Filters · View.
         Layout/density/size live in View; status/engine/sort live in Filters. -->
    <div class="toolbar">
      <div class="quick-tabs" role="group" aria-label="Play state">
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

      <div class="toolbar-right">
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

        <button
          class="tb-btn"
          class:tb-active={selectMode}
          aria-pressed={selectMode}
          onclick={toggleSelectMode}
          title="Select multiple games"
        >Select</button>

        <Popover.Root bind:open={filtersOpen}>
          <Popover.Trigger class="tb-btn" aria-label="Filters">
            Filters
            {#if activeFilterCount}<span class="tb-count">{activeFilterCount}</span>{/if}
            <span class="tb-caret">▾</span>
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content sideOffset={6} align="end">
              <div class="tb-popover">
              <div class="pop-section">
                <span class="label">Status</span>
                <div class="pop-chips">
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

              <div class="pop-section">
                <span class="label">Engine</span>
                <span class="select-arrow">
                  <select class="pop-select" bind:value={library.engine}>
                    {#each engines as eng}
                      <option value={eng}>{eng}</option>
                    {/each}
                  </select>
                </span>
              </div>

              <div class="pop-section">
                <span class="label">Sort</span>
                <span class="select-arrow">
                  <select class="pop-select" aria-label="Arrangement" value={library.sortColumn} onchange={onSortSelect}>
                    {#each SORT_OPTIONS as o}
                      <option value={o.value}>{o.label}</option>
                    {/each}
                  </select>
                </span>
              </div>

              {#if activeFilterCount}
                <button class="pop-clear" onclick={clearFilters}>Clear filters</button>
              {/if}
              </div>
            </Popover.Content>
          </Popover.Portal>
        </Popover.Root>

        <Popover.Root bind:open={viewOpen}>
          <Popover.Trigger class="tb-btn" aria-label="Library layout">
            <span class="tb-view-icon">{viewIcon}</span>{viewLabel}
            <span class="tb-caret">▾</span>
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content sideOffset={6} align="end">
              <div class="tb-popover">
              <div class="pop-section">
                <span class="label">Layout</span>
                <div class="pop-seg" role="group" aria-label="Library layout">
                  <button class="pop-seg-btn" class:active={library.viewMode === 'grid'} aria-pressed={library.viewMode === 'grid'} onclick={() => setViewMode('grid')}>▦ Grid</button>
                  <button class="pop-seg-btn" class:active={library.viewMode === 'wide'} aria-pressed={library.viewMode === 'wide'} onclick={() => setViewMode('wide')}>▭ Wide</button>
                  <button class="pop-seg-btn" class:active={library.viewMode === 'list'} aria-pressed={library.viewMode === 'list'} onclick={() => setViewMode('list')}>☰ List</button>
                </div>
              </div>

              <div class="pop-section">
                <span class="label">Density</span>
                <div class="pop-seg two" role="group" aria-label="Library density">
                  <button class="pop-seg-btn" class:active={library.density === 'comfortable'} aria-pressed={library.density === 'comfortable'} onclick={() => setDensity('comfortable')}>Comfortable</button>
                  <button class="pop-seg-btn" class:active={library.density === 'compact'} aria-pressed={library.density === 'compact'} onclick={() => setDensity('compact')}>Compact</button>
                </div>
              </div>

              {#if library.viewMode !== 'list'}
                <div class="pop-section">
                  <span class="label">Card size</span>
                  <div class="pop-size">
                    <input
                      type="range"
                      min={CARD_SCALE_MIN}
                      max={CARD_SCALE_MAX}
                      step="0.05"
                      value={library.cardScale}
                      oninput={(e) => setCardScale(e.currentTarget.value)}
                      ondblclick={() => setCardScale(1)}
                    />
                    <span class="pop-value">{Math.round(library.cardScale * 100)}%</span>
                  </div>
                </div>
              {/if}
              </div>
            </Popover.Content>
          </Popover.Portal>
        </Popover.Root>
      </div>
    </div>

    {#if displayed.length === 0}
      <div class="empty small">
        <p class="empty-title">{emptyTitle}</p>
        <p class="empty-desc">{emptyHint}</p>
        {#if hasActiveFilter}
          <button class="empty-clear" onclick={clearAllFilters}>Clear search and filters</button>
        {/if}
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
                  activity={tileActivity(game)}
                  isRoving={nav.rovingId === game.id}
                  selectable={selectMode}
                  selected={selectedIds.has(game.id)}
                  onDragStart={(e) => beginDrag(e, game)}
                onDragEnd={endDrag}
                  {coverBase}
                  {coverSrc}
                  {markFailed}
                  onOpenDetail={() => handleOpen(game)}
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
                  activity={tileActivity(game)}
                  isRoving={nav.rovingId === game.id}
                  selectable={selectMode}
                  selected={selectedIds.has(game.id)}
                  onDragStart={(e) => beginDrag(e, game)}
                onDragEnd={endDrag}
                  {coverBase}
                  {coverSrc}
                  {markFailed}
                  onOpenDetail={() => handleOpen(game)}
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
                activity={tileActivity(game)}
                isRoving={nav.rovingId === game.id}
                top={vItem.start}
                height={tableRowHeight}
                selectable={selectMode}
                selected={selectedIds.has(game.id)}
                onDragStart={(e) => beginDrag(e, game)}
                onDragEnd={endDrag}
                {coverBase}
                {coverSrc}
                {markFailed}
                onOpenDetail={() => handleOpen(game)}
                onFocus={() => nav.setFocused(game.id)}
                onContextMenu={(e) => onRowContextMenu(e, game)}
                onPlay={(e) => handlePlay(e, game)}
              />
            {/if}
          {/each}
        </div>
      </div>
    {/if}

    {#if dragGameId !== null}
      <!-- Drop tray: only exists while a card is being dragged, so it never
           competes with the toolbar. -->
      <div
        class="collection-tray"
        role="group"
        aria-label="Add dragged game to a collection"
        ondragover={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy' }}
        ondrop={(e) => {
          e.preventDefault()
          const chip = e.target.closest?.('[data-collection-id]')
          if (chip) dropOnCollection(Number(chip.dataset.collectionId))
        }}
      >
        <span class="tray-label">Add to</span>
        {#each collections as c (c.id)}
          <button
            class="tray-chip"
            data-collection-id={c.id}
            onclick={() => dropOnCollection(c.id)}
          >{c.name}</button>
        {/each}
        {#if collections.length === 0}
          <span class="tray-empty">No collections yet — create one in Collections.</span>
        {/if}
        <button class="tray-cancel" onclick={endDrag} aria-label="Cancel drag">✕</button>
      </div>
    {/if}

    {#if selectMode}
      <div class="bulk-bar" role="region" aria-label="Bulk actions">
        <span class="bulk-count">{selectedCount} selected</span>
        <button class="bulk-link" onclick={selectAllDisplayed}>Select all {displayed.length}</button>
        {#if selectedCount > 0}
          <button class="bulk-link" onclick={clearSelection}>Clear</button>
        {/if}
        <span class="bulk-spacer"></span>
        <span class="select-arrow">
          <select class="bulk-select" bind:value={bulkCollectionId} aria-label="Collection">
            <option value="">Choose collection…</option>
            {#each collections as c (c.id)}
              <option value={String(c.id)}>{c.name}</option>
            {/each}
          </select>
        </span>
        <button
          class="bulk-btn bulk-btn-primary"
          disabled={!bulkCollectionId || selectedCount === 0 || bulkBusy}
          onclick={bulkAddToCollection}
        >Add to collection</button>
        <button
          class="bulk-btn bulk-btn-danger"
          disabled={selectedCount === 0 || bulkBusy}
          onclick={bulkRemove}
        >Remove{selectedCount > 0 ? ` (${selectedCount})` : ''}</button>
        <button class="bulk-link" onclick={toggleSelectMode}>Done</button>
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
    flex: 1;
    min-height: 0;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--text-muted);
  }
  .empty.small { flex: 1; padding: 24px; }
  .empty-icon { font-size: var(--text-4xl); opacity: 0.5; }
  .empty-title { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); }
  .empty-desc { font-size: var(--text-base); }
  .empty-clear {
    margin-top: 4px;
    padding: 6px 12px;
    border: 1px solid var(--rule-strong);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--accent);
    font-size: var(--text-sm);
    font-weight: 600;
    cursor: pointer;
  }
  .empty-clear:hover { background: var(--bg-hover); }

  /* ── Page header ────────────────────────────────────── */
  .view-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding: 18px var(--gutter) 12px;
    background: var(--bg-primary);
    flex-shrink: 0;
  }
  .view-heading { min-width: 0; }
  .view-title {
    font-family: var(--font-display);
    font-size: var(--text-3xl);
    font-weight: 600;
    line-height: 1.15;
    letter-spacing: -0.01em;
    color: var(--text-primary);
  }
  .view-summary {
    margin-top: 3px;
    font-size: var(--text-sm);
    color: var(--text-secondary);
  }
  .ghost-btn {
    flex-shrink: 0;
    padding: 7px 12px;
    border: 1px solid var(--rule-strong);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-sm);
    font-weight: 600;
    cursor: pointer;
  }
  .ghost-btn:hover { background: var(--bg-hover); }

  /* ── Drag tray + bulk bar (P2-3) ────────────────────── */
  .collection-tray {
    position: fixed;
    left: 50%;
    bottom: 40px;
    transform: translateX(-50%);
    z-index: 900;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    max-width: min(760px, 92vw);
    max-height: 40vh;
    overflow-y: auto;
    padding: 10px 12px;
    border: 1px solid var(--accent);
    background: var(--bg-secondary);
    box-shadow: var(--shadow-pop);
  }
  .tray-label {
    font-family: var(--font-display);
    font-size: var(--text-2xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    color: var(--text-muted);
  }
  .tray-chip {
    padding: 5px 10px;
    border: 1px solid var(--rule-strong);
    background: var(--bg-tertiary);
    color: var(--text-primary);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .tray-chip:hover, .tray-chip:focus-visible {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--on-accent);
    outline: none;
  }
  .tray-empty { font-size: var(--text-sm); color: var(--text-muted); }
  .tray-cancel {
    margin-left: 4px;
    padding: 4px 7px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--text-xs);
    cursor: pointer;
  }
  .tray-cancel:hover { color: var(--text-primary); background: var(--bg-hover); }

  .bulk-bar {
    position: fixed;
    left: 50%;
    bottom: 40px;
    transform: translateX(-50%);
    z-index: 880;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    width: min(860px, 94vw);
    padding: 9px 12px;
    border: 1px solid var(--rule-strong);
    background: var(--bg-secondary);
    box-shadow: var(--shadow-pop);
  }
  .bulk-count {
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }
  .bulk-link {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-sm);
    cursor: pointer;
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .bulk-link:hover { color: var(--accent); }
  .bulk-spacer { flex: 1; }
  .bulk-select {
    padding: 5px 8px;
    border: 1px solid var(--border);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-sm);
  }
  .bulk-btn {
    padding: 5px 11px;
    border: 1px solid var(--rule-strong);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-sm);
    font-weight: 600;
    cursor: pointer;
  }
  .bulk-btn:disabled { opacity: 0.45; cursor: not-allowed; }
  .bulk-btn-primary:not(:disabled) { background: var(--accent); border-color: var(--accent); color: var(--on-accent); }
  .bulk-btn-danger:not(:disabled) { border-color: var(--danger); color: var(--danger); }
  .bulk-btn-danger:not(:disabled):hover { background: color-mix(in srgb, var(--danger) 12%, transparent); }

  /* ── Continue playing shelf (P2-1) ─────────────────── */
  .continue {
    flex-shrink: 0;
    padding: 2px var(--gutter) 14px;
    background: var(--bg-primary);
  }
  .continue-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 8px;
  }
  .continue-title {
    margin: 0;
    font-family: var(--font-display);
    font-size: var(--text-2xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    color: var(--text-muted);
  }
  .continue-all {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .continue-all:hover { color: var(--accent); }
  .continue-row {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 8px;
  }
  .continue-card {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    padding: 7px 8px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
  }
  .continue-card:hover { border-color: var(--rule-strong); background: var(--bg-hover); }
  .continue-open {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0;
    border: none;
    background: transparent;
    color: inherit;
    cursor: pointer;
    text-align: left;
  }
  .continue-open:focus-visible { outline: 1px solid var(--accent); outline-offset: 2px; }
  .continue-cover {
    flex: 0 0 34px;
    width: 34px;
    aspect-ratio: 3 / 4;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    background: var(--bg-tertiary);
    outline: 1px solid var(--border);
    outline-offset: -1px;
  }
  .continue-cover img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .continue-ph {
    font-family: var(--font-display);
    font-size: var(--text-2xs);
    font-weight: 700;
    color: var(--text-muted);
  }
  .continue-meta { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .continue-name {
    font-size: var(--text-base);
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .continue-sub {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    font-size: var(--text-xs);
    color: var(--text-muted);
    white-space: nowrap;
  }
  .continue-dot {
    width: 6px;
    height: 6px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--ec, var(--text-muted));
  }
  .continue-engine {
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text-secondary);
  }
  .continue-sep { color: var(--rule-strong); }
  .continue-play {
    flex-shrink: 0;
    width: 26px;
    height: 26px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: 1px solid var(--accent-dim);
    background: transparent;
    color: var(--accent);
    font-size: var(--text-2xs);
    cursor: pointer;
  }
  .continue-play:hover { background: var(--accent); color: var(--on-accent); border-color: var(--accent); }
  @media (max-width: 900px) {
    .continue-row { grid-template-columns: 1fr; }
  }

  /* ── Toolbar: quick views · search · Filters · View ─── */
  .toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 var(--gutter) 10px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-primary);
    flex-shrink: 0;
    flex-wrap: wrap;
  }
  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-left: auto;
    /* Grow into the row's remaining space instead of asking WebKitGTK for the
       group's intrinsic width. WebKit computes that width wrong for this
       nested flex row (~430px for 604px of controls), so the controls were
       painted past the window edge at every window size. flex-end keeps them
       right-aligned; the toolbar still wraps this whole group onto its own
       row when the window is narrow. */
    flex: 1 1 auto;
    min-width: 0;
    justify-content: flex-end;
  }
  .quick-tabs {
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .quick-tab {
    padding: 7px 10px;
    border: none;
    border-radius: 0;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-sm);
    font-weight: 500;
    cursor: pointer;
    box-shadow: inset 0 -2px 0 transparent;
    white-space: nowrap;
  }
  .quick-tab:first-child { padding-left: 0; }
  .quick-tab:hover { color: var(--text-primary); }
  .quick-tab.active {
    color: var(--text-primary);
    font-weight: 600;
    box-shadow: inset 0 -2px 0 var(--accent);
  }

  :global(.tb-btn) {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 32px;
    padding: 0 10px;
    border: 1px solid var(--rule-strong);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-sm);
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  :global(.tb-btn):hover { background: var(--bg-hover); }
  :global(.tb-btn[data-state="open"]) { border-color: var(--accent); }
  :global(.tb-btn).tb-active { border-color: var(--accent); color: var(--accent); }
  .tb-count {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 18px;
    height: 18px;
    padding: 0 4px;
    background: color-mix(in srgb, var(--accent) 18%, transparent);
    color: var(--accent);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
  }
  .tb-caret { color: var(--text-secondary); font-size: 10px; }
  .tb-view-icon { font-size: var(--text-sm); line-height: 1; }

  /* ── Floating filter / layout panels ────────────────── */
  .tb-popover {
    z-index: var(--z-dropdown);
    width: 280px;
    max-width: calc(100vw - 24px);
    padding: 12px;
    background: var(--bg-secondary);
    border: 1px solid var(--rule-strong);
    border-radius: var(--radius-1);
    box-shadow: var(--shadow-pop);
  }
  .pop-section + .pop-section { margin-top: 12px; }
  .pop-section .label { display: block; margin-bottom: 6px; }
  .pop-chips { display: flex; flex-wrap: wrap; gap: 4px; }
  .pop-section .select-arrow { display: block; }
  .pop-select {
    width: 100%;
    padding: 6px 22px 6px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background-color: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-sm);
    outline: none;
  }
  .pop-select:focus { border-color: var(--accent); }
  .pop-seg {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 2px;
    padding: 2px;
    background: var(--bg-tertiary);
  }
  .pop-seg.two { grid-template-columns: repeat(2, 1fr); }
  .pop-seg-btn {
    padding: 5px 8px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-xs);
    font-weight: 500;
    cursor: pointer;
    text-align: center;
  }
  .pop-seg-btn:hover { color: var(--text-primary); }
  .pop-seg-btn.active {
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    font-weight: 600;
  }
  .pop-size { display: flex; align-items: center; gap: 10px; }
  .pop-size input { flex: 1; accent-color: var(--accent); }
  .pop-value {
    min-width: 36px;
    text-align: right;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--text-secondary);
  }
  .pop-clear {
    margin-top: 12px;
    padding: 0;
    border: none;
    background: none;
    color: var(--accent);
    font-size: var(--text-xs);
    font-weight: 600;
    cursor: pointer;
  }

  .search-wrapper {
    position: relative;
    flex: 1 1 380px;
    min-width: 180px;
    max-width: 380px;
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
    background: color-mix(in srgb, var(--accent) 18%, transparent);
    color: var(--accent);
    border-color: var(--accent-dim);
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
