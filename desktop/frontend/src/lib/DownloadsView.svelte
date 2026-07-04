<script>
  import {onMount} from 'svelte'
  import {
    GetGamesWithDownloadLinks,
    GetAllDownloadLinks,
    OpenDownloadURL,
  } from '../../wailsjs/go/main/App'

  // ── State ──────────────────────────────────────────────────
  let games = $state([])        // games with download links
  let allLinks = $state([])     // all download links with game info
  let loading = $state(true)
  let error = $state('')
  let searchQuery = $state('')
  let expandedGames = $state(new Set())
  let openingLinks = $state(new Set())   // link IDs currently being opened

  async function loadData() {
    loading = true
    error = ''
    try {
      const [g, links] = await Promise.all([
        GetGamesWithDownloadLinks(),
        GetAllDownloadLinks(),
      ])
      games = g
      allLinks = links
    } catch (e) {
      error = String(e)
      games = []
      allLinks = []
    }
    loading = false
  }

  function toggleGame(id) {
    const next = new Set(expandedGames)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    expandedGames = next
  }

  async function handleOpenLink(linkId) {
    if (openingLinks.has(linkId)) return
    openingLinks = new Set([...openingLinks, linkId])
    try {
      await OpenDownloadURL(linkId)
    } catch (e) {
      console.error('Failed to open download URL:', e)
    } finally {
      const next = new Set(openingLinks)
      next.delete(linkId)
      openingLinks = next
    }
  }

  // ── Derived ─────────────────────────────────────────────────

  // Filter games by search query
  let filteredGames = $derived.by(() => {
    if (!searchQuery.trim()) return games
    const q = searchQuery.toLowerCase().trim()
    return games.filter(g => g.title.toLowerCase().includes(q))
  })

  // Count of links per game
  function linksCount(gameId) {
    return allLinks.filter(l => l.gameId === gameId).length
  }

  // Determine host badge color
  function hostColor(host) {
    const map = {
      'pixeldrain': '#4fc3f7',
      'mega': '#d32f2f',
      'mediafire': '#1976d2',
      'google': '#34a853',
      'drive': '#34a853',
      'anonfiles': '#f57c00',
      'sendspace': '#7c4dff',
      'zippyshare': '#ff8f00',
      'dropbox': '#0061ff',
      '1fichier': '#e65100',
      'filemonster': '#00897b',
      'rapidgator': '#ff1744',
      'uploadhaven': '#00bcd4',
      'katfile': '#ff6f00',
      'doodrive': '#00c853',
      'dood': '#00c853',
      'workupload': '#9c27b0',
    }
    const lower = (host || '').toLowerCase()
    for (const [key, color] of Object.entries(map)) {
      if (lower.includes(key)) return color
    }
    return '#9090a0'
  }

  // Total link count
  let totalLinks = $derived(allLinks.length)

  // Dead link count
  let deadLinks = $derived(allLinks.filter(l => l.isDead).length)

  onMount(loadData)
</script>

<div class="downloads-view">
  <!-- ── Header ────────────────────────────────────────────── -->
  <div class="downloads-header">
    <h2>Downloads</h2>
    <p class="downloads-subtitle">
      Browse and open download links for your games.
    </p>
  </div>

  <!-- ── Summary Stats ────────────────────────────────────── -->
  {#if !loading && !error}
    <div class="stats-bar">
      <span class="stat-item">
        <span class="stat-value">{games.length}</span>
        <span class="stat-label">games with links</span>
      </span>
      <span class="stat-divider"></span>
      <span class="stat-item">
        <span class="stat-value">{totalLinks}</span>
        <span class="stat-label">total links</span>
      </span>
      {#if deadLinks > 0}
        <span class="stat-divider"></span>
        <span class="stat-item stat-dead">
          <span class="stat-value">{deadLinks}</span>
          <span class="stat-label">dead links</span>
        </span>
      {/if}
    </div>
  {/if}

  <!-- ── Search ─────────────────────────────────────────────── -->
  {#if !loading && games.length > 0}
    <div class="search-bar">
      <span class="search-icon">🔍</span>
      <input
        type="text"
        class="search-input"
        placeholder="Search games with download links…"
        bind:value={searchQuery}
      />
      {#if searchQuery}
        <button class="search-clear" onclick={() => searchQuery = ''}>✕</button>
      {/if}
    </div>
  {/if}

  <!-- ── Error ─────────────────────────────────────────────── -->
  {#if error}
    <div class="error-section">
      <p class="error-title">Failed to load downloads:</p>
      <p class="error-line">{error}</p>
      <button class="btn btn-sm btn-outline" onclick={loadData}>Retry</button>
    </div>
  {/if}

  <!-- ── Loading ───────────────────────────────────────────── -->
  {#if loading}
    <div class="status-section status-loading">
      <div class="spinner"></div>
      <p class="status-text">Loading download links…</p>
    </div>
  {:else if filteredGames.length > 0}
    <!-- ── Game List ─────────────────────────────────────────── -->
    <div class="game-list">
      {#each filteredGames as game (game.id)}
        {@const linkCount = allLinks.filter(l => l.gameId === game.id).length}
        {@const deadCount = allLinks.filter(l => l.gameId === game.id && l.isDead).length}

        <div class="game-card">
          <button
            class="game-header"
            onclick={() => toggleGame(game.id)}
          >
            <span class="game-expand-icon">{expandedGames.has(game.id) ? '▾' : '▸'}</span>
            <span class="game-title">{game.title}</span>
            <span class="game-link-count">{linkCount} link{linkCount !== 1 ? 's' : ''}</span>
            {#if deadCount > 0}
              <span class="dead-badge">{deadCount} dead</span>
            {/if}
          </button>

          {#if expandedGames.has(game.id)}
            {@const links = allLinks.filter(l => l.gameId === game.id)}
            <div class="links-section">
              {#if links.length > 0}
                <div class="links-header">
                  <span class="lh-host">Host</span>
                  <span class="lh-name">File</span>
                  <span class="lh-platform">Platform</span>
                  <span class="lh-action">Action</span>
                </div>
                <div class="links-body">
                  {#each links as link (link.id)}
                    <div class="link-row" class:link-dead={link.isDead}>
                      <span class="link-host">
                        <span class="host-badge" style="--hc: {hostColor(link.host)}">
                          {link.host || 'unknown'}
                        </span>
                      </span>
                      <span class="link-name" title={link.name || link.url}>
                        {link.name || link.url}
                      </span>
                      <span class="link-platform">
                        {#if link.platform && link.platform !== 'unknown' && link.platform !== 'all'}
                          <span class="platform-badge">{link.platform}</span>
                        {:else if link.platform === 'all'}
                          <span class="platform-badge platform-all">all</span>
                        {:else}
                          <span class="text-muted">—</span>
                        {/if}
                      </span>
                      <span class="link-action">
                        {#if link.isDead}
                          <span class="dead-label">Dead</span>
                        {:else}
                          <button
                            class="btn btn-sm btn-accent"
                            onclick={() => handleOpenLink(link.id)}
                            disabled={openingLinks.has(link.id)}
                          >
                            {#if openingLinks.has(link.id)}
                              Opening…
                            {:else}
                              Open URL
                            {/if}
                          </button>
                        {/if}
                      </span>
                    </div>
                  {/each}
                </div>
              {:else}
                <p class="links-empty">No download links for this game.</p>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {:else if !loading && !error}
    <!-- ── Empty State ───────────────────────────────────────── -->
    <div class="status-section status-empty">
      {#if searchQuery}
        <span class="status-icon">🔍</span>
        <div class="status-body">
          <p class="status-title">No matches</p>
          <p class="status-detail">
            No games match "{searchQuery}". Try a different search term.
          </p>
          <button class="btn btn-sm btn-outline" onclick={() => searchQuery = ''}>Clear Search</button>
        </div>
      {:else}
        <span class="status-icon">↓</span>
        <div class="status-body">
          <p class="status-title">No download links yet</p>
          <p class="status-detail">
            Download links appear here after you sync your games with F95Zone.
            Go to <strong>Sync</strong> in the sidebar to auto-associate games
            and scrape their download links.
          </p>
          <button class="btn btn-sm btn-accent" onclick={loadData}>Refresh</button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .downloads-view {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 800px;
    margin: 0 auto;
    width: 100%;
  }

  .downloads-header {
    margin-bottom: 20px;
  }
  .downloads-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .downloads-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Stats Bar ──────────────────────── */
  .stats-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
    padding: 12px 16px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
  }
  .stat-item {
    display: flex;
    align-items: baseline;
    gap: 6px;
  }
  .stat-value {
    font-size: 18px;
    font-weight: 700;
    color: var(--accent);
    font-family: var(--font-mono);
  }
  .stat-label {
    font-size: 12px;
    color: var(--text-muted);
  }
  .stat-dead .stat-value {
    color: var(--danger);
  }
  .stat-divider {
    width: 1px;
    height: 20px;
    background: var(--border);
    flex-shrink: 0;
  }

  /* ── Search ──────────────────────────── */
  .search-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 16px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
  }
  .search-bar:focus-within {
    border-color: var(--accent);
  }
  .search-icon {
    font-size: 14px;
    flex-shrink: 0;
  }
  .search-input {
    flex: 1;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
    min-width: 0;
  }
  .search-input::placeholder {
    color: var(--text-muted);
  }
  .search-clear {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 12px;
    cursor: pointer;
    padding: 2px;
    flex-shrink: 0;
  }
  .search-clear:hover {
    color: var(--text-primary);
  }

  /* ── Shared Status Sections ────────── */
  .status-section {
    margin: 16px 0;
    padding: 16px;
    border-radius: 8px;
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .status-icon {
    font-size: 18px;
    font-weight: 700;
    flex-shrink: 0;
    line-height: 1.4;
  }
  .status-body {
    flex: 1;
    min-width: 0;
  }
  .status-title {
    font-size: 15px;
    font-weight: 600;
    margin: 0 0 4px;
  }
  .status-detail {
    font-size: 13px;
    margin: 0 0 10px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .status-detail strong {
    color: var(--text-primary);
  }

  .status-loading {
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    align-items: center;
  }
  .status-loading .status-text {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }

  .status-empty {
    border: 1px solid var(--border);
    background: var(--bg-secondary);
  }
  .status-empty .status-icon {
    color: var(--text-muted);
  }
  .status-empty .status-title {
    color: var(--text-primary);
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
  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* ── Game List ──────────────────────── */
  .game-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .game-card {
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
    background: var(--bg-secondary);
    transition: border-color 0.12s;
  }
  .game-card:focus-within,
  .game-card:hover {
    border-color: var(--accent-dim);
  }

  .game-header {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 10px 12px;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    text-align: left;
    transition: background 0.12s;
  }
  .game-header:hover {
    background: var(--bg-hover);
  }

  .game-expand-icon {
    font-size: 10px;
    color: var(--text-muted);
    width: 12px;
    text-align: center;
    flex-shrink: 0;
  }

  .game-title {
    flex: 1;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .game-link-count {
    font-size: 11px;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .dead-badge {
    padding: 1px 6px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger);
    flex-shrink: 0;
  }

  /* ── Links Section ──────────────────── */
  .links-section {
    border-top: 1px solid var(--border);
    background: var(--bg-primary);
  }

  .links-header {
    display: grid;
    grid-template-columns: 100px 1fr 80px 90px;
    gap: 8px;
    padding: 6px 12px;
    font-size: 10px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    background: var(--bg-tertiary);
  }

  .links-body {
    display: flex;
    flex-direction: column;
  }

  .link-row {
    display: grid;
    grid-template-columns: 100px 1fr 80px 90px;
    gap: 8px;
    padding: 7px 12px;
    font-size: 12px;
    border-bottom: 1px solid var(--border);
    align-items: center;
    transition: background 0.08s;
  }
  .link-row:last-child {
    border-bottom: none;
  }
  .link-row:hover {
    background: var(--bg-hover);
  }
  .link-row.link-dead {
    opacity: 0.6;
  }

  .host-badge {
    display: inline-block;
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    background: color-mix(in srgb, var(--hc) 15%, transparent);
    color: var(--hc);
    text-transform: capitalize;
  }

  .link-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .link-dead .link-name {
    text-decoration: line-through;
    color: var(--text-muted);
  }

  .platform-badge {
    padding: 1px 6px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent);
    text-transform: capitalize;
  }
  .platform-all {
    background: color-mix(in srgb, var(--text-muted) 12%, transparent);
    color: var(--text-muted);
  }

  .link-action {
    display: flex;
    justify-content: flex-end;
  }

  .dead-label {
    font-size: 10px;
    font-weight: 600;
    color: var(--danger);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .links-empty {
    padding: 16px;
    text-align: center;
    font-size: 12px;
    color: var(--text-muted);
    margin: 0;
  }

  /* ── Buttons ────────────────────────── */
  .btn {
    padding: 7px 16px;
    border: none;
    border-radius: 6px;
    font-size: 13px;
    cursor: pointer;
    white-space: nowrap;
    transition: all 0.12s;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .btn-sm {
    padding: 4px 10px;
    font-size: 11px;
  }

  .btn-accent {
    background: var(--accent);
    color: #fff;
  }
  .btn-accent:hover:not(:disabled) {
    background: var(--accent-hover);
  }

  .btn-outline {
    background: transparent;
    color: var(--text-primary);
    border: 1px solid var(--border);
  }
  .btn-outline:hover:not(:disabled) {
    background: var(--bg-hover);
  }

  /* ── Error ───────────────────────────── */
  .error-section {
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--danger);
    border-radius: 8px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .error-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--danger);
    margin: 0 0 4px;
  }
  .error-line {
    font-size: 12px;
    color: var(--text-secondary);
    margin: 0 0 10px;
    font-family: var(--font-mono);
  }

  .text-muted {
    color: var(--text-muted);
  }
</style>
