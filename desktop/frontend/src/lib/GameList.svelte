<script>
  import {SearchGames, RemoveGame, SetGameStatus, RenameGame, GetCachedCover} from '../../wailsjs/go/main/App'

  let {
    games = [],
    onOpenDetail = (id) => {},
    onUpdate = () => {},
  } = $props()

  // ── Cover loading ─────────────────────────────────────────────
  let coverCache = $state(new Map())   // gameId → data URI string
  let coverLoading = $state(new Set()) // gameIds currently loading

  async function loadCover(gameId) {
    if (coverCache.has(gameId) || coverLoading.has(gameId)) return
    coverLoading.add(gameId)
    try {
      const uri = await GetCachedCover(gameId)
      if (uri) {
        coverCache.set(gameId, uri)
        // Trigger reactivity by reassigning
        coverCache = new Map(coverCache)
      }
    } catch (e) {
      console.error('Failed to load cover for', gameId, e)
    } finally {
      coverLoading.delete(gameId)
    }
  }

  // Watch the displayed list and trigger cover loading for visible items
  $effect(() => {
    const list = displayed
    // Load covers for the first batch (visible rows)
    const batch = list.slice(0, 50)
    for (const g of batch) {
      if (g.hasCover) loadCover(g.id)
    }
  })

  // ── Search & Filters ──────────────────────────────────────────
  let searchQuery = $state('')
  let debounceTimer                        // plain var, not reactive
  let searchResults = $state(null)         // null = use full list, array = search results
  let isSearching = $state(false)

  let activeEngine = $state('All')
  let activeStatus = $state('')

  // Extract distinct engines from the game list
  let engines = $derived.by(() => {
    const set = new Set()
    for (const g of games) set.add(g.engine)
    const arr = [...set].filter(Boolean).sort()
    arr.unshift('All')
    return arr
  })

  // Available statuses
  const statuses = ['All', 'active', 'completed', 'abandoned', 'on_hold', 'unknown']

  // ── Sorting ───────────────────────────────────────────────────
  let sortColumn = $state('title')
  let sortDesc = $state(false)

  function toggleSort(col) {
    if (sortColumn === col) {
      sortDesc = !sortDesc
    } else {
      sortColumn = col
      sortDesc = false
    }
  }

  function sortIcon(col) {
    if (sortColumn !== col) return '▽'
    return sortDesc ? '▲' : '▼'
  }

  // ── Derived: filtered + sorted list ──────────────────────────
  let displayed = $derived.by(() => {
    // 1. Use search results if available, else full list
    let list = searchResults ?? games

    // 2. Filter by engine
    if (activeEngine && activeEngine !== 'All') {
      list = list.filter(g => g.engine === activeEngine)
    }

    // 3. Filter by status
    if (activeStatus && activeStatus !== 'All') {
      list = list.filter(g => g.status === activeStatus)
    }

    // 4. Sort
    const sorted = [...list]
    sorted.sort((a, b) => {
      let cmp = 0
      switch (sortColumn) {
        case 'title':
          cmp = (a.title || '').localeCompare(b.title || '', undefined, {numeric: true})
          break
        case 'engine':
          cmp = (a.engine || '').localeCompare(b.engine || '')
          break
        case 'version':
          cmp = (a.version || '').localeCompare(b.version || '')
          break
        case 'status':
          cmp = (a.status || '').localeCompare(b.status || '')
          break
        case 'size':
          cmp = (a.sizeBytes || 0) - (b.sizeBytes || 0)
          break
      }
      return sortDesc ? -cmp : cmp
    })

    return sorted
  })

  // ── Search with debounce ──────────────────────────────────────
  async function doSearch(query) {
    if (!query || query.trim().length < 2) {
      searchResults = null
      isSearching = false
      return
    }
    isSearching = true
    try {
      searchResults = await SearchGames(query.trim())
    } catch (e) {
      console.error('Search failed:', e)
      searchResults = null
    }
    isSearching = false
  }

  function onSearchInput(e) {
    searchQuery = e.target.value
    clearTimeout(debounceTimer)
    debounceTimer = setTimeout(() => doSearch(searchQuery), 300)
  }

  // ── Engine colors (matches TUI) ───────────────────────────────
  function engineColor(engine) {
    const map = {
      'ren\'py': '#ff6b9d',
      'unity': '#6b9dff',
      'rpg maker': '#9d6bff',
      'rpgmakermv': '#9d6bff',
      'rpgmakermz': '#9d6bff',
      'rpgmakervxace': '#9d6bff',
      'html': '#ff9d6b',
      'wolf rpg': '#6bff9d',
      'flash': '#ff6b6b',
      'unreal': '#6bfffb',
      'godot': '#9dff6b',
      'electron': '#6bc9ff',
      'nw.js': '#6bc9ff',
    }
    for (const [key, color] of Object.entries(map)) {
      if (engine?.toLowerCase().includes(key)) return color
    }
    return '#9090a0'
  }

  function statusLabel(s) {
    const labels = {
      active: 'Active',
      completed: 'Completed',
      abandoned: 'Abandoned',
      on_hold: 'On Hold',
      unknown: 'Unknown',
    }
    return labels[s] || s || 'Unknown'
  }

  function hasUpdate(game) {
    return game.latestVersion && game.version &&
           game.latestVersion !== game.version
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
      onUpdate()
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
      onUpdate()
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
      onUpdate()
    } catch (e) {
      console.error('Failed to remove:', e)
    }
  }
</script>

<div class="game-list">
  {#if games.length === 0 && !isSearching}
    <div class="empty">
      <div class="empty-icon">📂</div>
      <p class="empty-title">No games yet</p>
      <p class="empty-desc">Scan a directory to find and add games to your library.</p>
    </div>
  {:else}
    {#if isSearching}
      <div class="searching-indicator">Searching…</div>
    {/if}

    <!-- Filter Bar -->
    <div class="filter-bar">
      <div class="search-wrapper">
        <span class="search-icon">🔍</span>
        <input
          type="text"
          class="search-input"
          placeholder="Search titles, tags, developers…"
          value={searchQuery}
          oninput={onSearchInput}
        />
      </div>

      <span class="select-arrow">
        <select class="filter-select" bind:value={activeEngine}>
          {#each engines as eng}
            <option value={eng}>{eng}</option>
          {/each}
        </select>
      </span>

      <div class="status-chips">
        {#each statuses as s}
          <button
            class="chip"
            class:chip-active={activeStatus === s}
            onclick={() => activeStatus = activeStatus === s ? '' : s}
          >
            {statusLabel(s)}
          </button>
        {/each}
      </div>
    </div>

    <!-- Column Headers -->
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
    </div>

    <!-- Rows -->
    <div class="table-body">
      {#each displayed as game}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div
          class="table-row"
          onclick={() => onOpenDetail(game.id)}
          oncontextmenu={(e) => onRowContextMenu(e, game)}
          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpenDetail(game.id); } }}
          role="button"
          tabindex="0"
        >
          <span class="col-cover">
            {#if game.hasCover && coverCache.has(game.id)}
              <img
                class="cover-thumb"
                src={coverCache.get(game.id)}
                alt="{game.title} cover"
                loading="lazy"
              />
            {:else if game.hasCover}
              <span class="cover-placeholder" title="Loading cover…">
                <span class="cover-spinner"></span>
              </span>
            {:else}
              <span class="cover-placeholder" title="No cover">
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
        </div>
      {:else}
        <div class="empty small">
          <p>No games match your filters.</p>
        </div>
      {/each}
    </div>

    <!-- Context Menu -->
    {#if contextMenu}
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="context-menu-overlay" onclick={closeContextMenu} oncontextmenu={(e) => e.preventDefault()}>
        <div class="context-menu" style="left: {contextMenu.x}px; top: {contextMenu.y}px">
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
            {#each ['active', 'completed', 'abandoned', 'on_hold', 'unknown'] as s}
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
  .empty.small { height: 200px; }
  .empty-icon { font-size: 40px; opacity: 0.5; }
  .empty-title { font-size: 16px; font-weight: 600; color: var(--text-secondary); }
  .empty-desc { font-size: 13px; }

  /* ── Filter Bar ────────────────────────────────────── */
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

  /* ── Table ─────────────────────────────────────────── */
  .table-header {
    display: grid;
    grid-template-columns: 50px 1fr 110px 130px 80px 100px;
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
  }

  .table-row {
    display: grid;
    grid-template-columns: 50px 1fr 110px 130px 80px 100px;
    gap: 8px;
    padding: 4px 12px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background 0.08s;
    align-items: center;
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
    width: 40px;
    height: 56px;
    object-fit: cover;
    border-radius: 3px;
    background: var(--bg-secondary);
    flex-shrink: 0;
  }

  .cover-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 56px;
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

  @keyframes spin {
    to { transform: rotate(360deg); }
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
