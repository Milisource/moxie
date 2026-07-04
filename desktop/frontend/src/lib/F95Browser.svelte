<script>
  import {onMount} from 'svelte'
  import {
    GetCookieStatus,
    SearchF95Zone,
    GetThreadPreview,
    AddGameFromF95Zone,
  } from '../../wailsjs/go/main/App'
  import {engineColor, engineStyle} from './engineColors.js'

  // ── State ──────────────────────────────────────────────────
  let query = $state('')
  let results = $state([])
  let loading = $state(false)
  let searched = $state(false)
  let error = $state('')

  // Preview state
  let selectedResult = $state(null)
  let previewing = $state(false)
  let preview = $state(null)
  let previewError = $state('')

  // Add-to-library state
  let adding = $state(false)
  let addResult = $state(null)     // { success: boolean, gameId?: number, error?: string }

  // Cookie status
  let cookieStatus = $state('')    // 'available' | 'not_found' | ''

  // Debounce timer
  let debounceTimer = null

  // ── Cookie check on mount ──────────────────────────────────
  onMount(async () => {
    try {
      cookieStatus = await GetCookieStatus()
    } catch (e) {
      console.error('Failed to get cookie status:', e)
    }
  })

  // ── Derived ─────────────────────────────────────────────────
  let canSearch = $derived(query.trim().length >= 2 && cookieStatus === 'available' && !loading)

  // Engine display colors — imported from engineColors.js

  // ── Search with debounce ───────────────────────────────────
  function handleSearchInput(e) {
    query = e.target.value
    clearTimeout(debounceTimer)

    if (query.trim().length < 2) {
      searched = false
      return
    }

    debounceTimer = setTimeout(() => {
      doSearch()
    }, 300)
  }

  async function doSearch() {
    const q = query.trim()
    if (q.length < 2) return

    loading = true
    error = ''
    searched = true
    results = []
    selectedResult = null
    preview = null
    previewing = false
    addResult = null

    try {
      results = await SearchF95Zone(q)
    } catch (e) {
      error = String(e)
    }
    loading = false
  }

  // ── Preview ────────────────────────────────────────────────
  async function handlePreview(result) {
    selectedResult = result
    previewing = true
    preview = null
    previewError = ''
    addResult = null

    try {
      preview = await GetThreadPreview(result.url)
    } catch (e) {
      previewError = String(e)
    }
  }

  function closePreview() {
    previewing = false
    preview = null
    selectedResult = null
    addResult = null
  }

  // ── Add to Library ─────────────────────────────────────────
  async function handleAddToLibrary() {
    if (!preview) return

    adding = true
    addResult = null

    try {
      const gameId = await AddGameFromF95Zone(
        selectedResult.url,
        preview.title,
        preview.prefix || '',
      )
      addResult = { success: true, gameId }
    } catch (e) {
      addResult = { success: false, error: String(e) }
    }
    adding = false
  }

  // ── Overview truncation ────────────────────────────────────
  let expandedOverview = $state(false)

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
      <span class="cookie-icon">⚠</span>
      <div class="cookie-body">
        <p class="cookie-title">Log into F95Zone in your browser first</p>
        <p class="cookie-detail">
          The browser needs your F95Zone session cookies to search and browse.
          Log in at <strong>f95zone.to</strong> in your browser, then restart this app.
        </p>
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
      value={query}
      oninput={handleSearchInput}
      onkeydown={(e) => e.key === 'Enter' && doSearch()}
    />
    <button
      class="btn btn-primary"
      onclick={doSearch}
      disabled={!canSearch}
    >
      {#if loading}
        <span class="spinner-small"></span>
      {:else}
        Search
      {/if}
    </button>
  </div>

  <!-- ── Content Area: Results + Preview ──────────────────── -->
  <div class="browser-content" class:has-preview={previewing}>
    <!-- ── Results Section ──────────────────────────────── -->
    <div class="results-section">
      {#if error}
        <div class="error-section">
          <p class="error-title">Search failed:</p>
          <p class="error-line">{error}</p>
        </div>
      {:else if loading}
        <div class="loading-state">
          <div class="spinner-lg"></div>
          <p>Searching F95Zone…</p>
        </div>
      {:else if searched && results.length === 0}
        <div class="empty-state">
          <p class="empty-icon">🔍</p>
          <p class="empty-title">No results found</p>
          <p class="empty-detail">Try a different search term.</p>
        </div>
      {:else if !searched}
        <div class="empty-state">
          <p class="empty-icon">🌐</p>
          <p class="empty-title">Search F95Zone</p>
          <p class="empty-detail">Enter at least 2 characters to start searching.</p>
        </div>
      {:else}
        <div class="results-grid">
          {#each results as result}
            <button
              class="result-card"
              class:selected={selectedResult?.url === result.url}
              onclick={() => handlePreview(result)}
            >
              <div class="result-thumb">
                {#if result.thumbnailUrl}
                  <img src={result.thumbnailUrl} alt={result.title} />
                {:else}
                  <div class="result-thumb-placeholder">
                    <span class="placeholder-icon">🎮</span>
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
    </div>

    <!-- ── Preview Section ───────────────────────────────── -->
    {#if previewing}
      <div class="preview-section">
        <button class="preview-close" onclick={closePreview}>✕</button>

        {#if previewError}
          <div class="error-section">
            <p class="error-title">Preview failed:</p>
            <p class="error-line">{previewError}</p>
          </div>
        {:else if !preview}
          <div class="loading-state">
            <div class="spinner-lg"></div>
            <p>Loading thread preview…</p>
          </div>
        {:else}
          <!-- Cover Art -->
          <div class="preview-cover">
            {#if preview.coverUrl}
              <img src={preview.coverUrl} alt={preview.title} />
            {:else}
              <div class="preview-cover-placeholder">
                <span>🎮</span>
                <span>{preview.title}</span>
              </div>
            {/if}
          </div>

          <!-- Title & Meta -->
          <div class="preview-meta">
            <h3 class="preview-title">{preview.title}</h3>
            <div class="preview-badges">
              {#if preview.prefix}
                <span
                  class="engine-badge"
                  style={engineStyle(preview.prefix)}
                >
                  {preview.prefix}
                </span>
              {/if}
              {#if preview.status && preview.status !== 'unknown'}
                <span class="status-badge" class:status-active={preview.status === 'active'}
                  class:status-completed={preview.status === 'completed'}
                  class:status-on-hold={preview.status === 'on_hold'}
                  class:status-abandoned={preview.status === 'abandoned'}>
                  {preview.status}
                </span>
              {/if}
            </div>

            {#if preview.developer}
              <p class="preview-developer"><strong>Developer:</strong> {preview.developer}</p>
            {/if}
            {#if preview.version}
              <p class="preview-version"><strong>Version:</strong> {preview.version}</p>
            {/if}

            <!-- Tags -->
            {#if preview.tags?.length > 0}
              <div class="preview-tags">
                {#each preview.tags as tag}
                  <span class="tag">{tag}</span>
                {/each}
              </div>
            {/if}
          </div>

          <!-- Overview -->
          {#if preview.overview}
            <div class="preview-overview">
              <h4>Overview</h4>
              <p>
                {#if expandedOverview || preview.overview.length <= 300}
                  {preview.overview}
                {:else}
                  {truncate(preview.overview)}
                {/if}
              </p>
              {#if preview.overview.length > 300}
                <button class="btn-link" onclick={() => expandedOverview = !expandedOverview}>
                  {expandedOverview ? 'Show less' : 'Show more'}
                </button>
              {/if}
            </div>
          {/if}

          <!-- Store Links -->
          {#if preview.storeLinks && Object.keys(preview.storeLinks).length > 0}
            <div class="preview-stores">
              <h4>Store Links</h4>
              <div class="store-links">
                {#each Object.entries(preview.storeLinks) as [name, url]}
                  <a href={url} target="_blank" rel="noopener" class="store-link">
                    {#if name === 'steam'}
                      ◈
                    {:else if name === 'patreon'}
                      ⚡
                    {:else}
                      🔗
                    {/if}
                    {name}
                  </a>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Download Links -->
          {#if preview.downloadLinks?.length > 0}
            <div class="preview-downloads">
              <h4>Download Links</h4>
              <div class="download-list">
                {#each preview.downloadLinks as dl}
                  <a href={dl.url} target="_blank" rel="noopener" class="download-link">
                    <span class="dl-host">{dl.host}</span>
                    <span class="dl-name">{dl.name || 'Link'}</span>
                    {#if dl.platform}
                      <span class="dl-platform">{dl.platform}</span>
                    {/if}
                  </a>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Add to Library Button -->
          <div class="preview-actions">
            {#if addResult?.success}
              <div class="add-success">
                <span>✓</span>
                <span>Game added to library (ID: {addResult.gameId})</span>
              </div>
            {:else if addResult?.error}
              <div class="add-error">
                <span>✗</span>
                <span>{addResult.error}</span>
              </div>
            {:else}
              <button
                class="btn btn-primary add-btn"
                onclick={handleAddToLibrary}
                disabled={adding}
              >
                {#if adding}
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
    padding: 32px;
    max-width: 960px;
    margin: 0 auto;
    width: 100%;
    display: flex;
    flex-direction: column;
  }

  .browser-header {
    margin-bottom: 20px;
  }
  .browser-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .browser-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Cookie Status ─────────────────── */
  .cookie-status {
    margin: 0 0 16px;
    padding: 12px 16px;
    border-radius: 8px;
    display: flex;
    gap: 10px;
    align-items: flex-start;
  }
  .cookie-icon {
    font-size: 16px;
    font-weight: 700;
    flex-shrink: 0;
    line-height: 1.4;
  }
  .cookie-body { flex: 1; min-width: 0; }
  .cookie-title { font-size: 14px; font-weight: 600; margin: 0 0 2px; }
  .cookie-detail { font-size: 12px; color: var(--text-secondary); margin: 0; line-height: 1.5; }
  .cookie-detail strong { color: var(--text-primary); }
  .cookie-text { font-size: 13px; font-weight: 500; }

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

  .cookie-loading {
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    align-items: center;
  }
  .cookie-loading .status-text {
    font-size: 13px;
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
    border-radius: 8px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 14px;
    outline: none;
    transition: border-color 0.12s;
  }
  .search-input:focus { border-color: var(--accent); }
  .search-input::placeholder { color: var(--text-muted); }

  /* ── Buttons ────────────────────────── */
  .btn {
    padding: 8px 18px;
    border: none;
    border-radius: 6px;
    font-size: 13px;
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
    color: #fff;
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }

  .btn-link {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 12px;
    padding: 4px 0;
  }
  .btn-link:hover { text-decoration: underline; }

  .add-btn {
    width: 100%;
    padding: 10px 18px;
    font-size: 14px;
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
    border-radius: 8px;
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
    font-size: 32px;
    opacity: 0.4;
  }

  .result-info {
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .result-title {
    font-size: 12px;
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
    font-size: 10px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }

  /* ── Engine Badge ──────────────────── */
  .engine-badge {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 600;
    line-height: 1.5;
  }

  /* ── Status Badge ──────────────────── */
  .status-badge {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 10px;
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
    font-size: 13px;
  }
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 48px 0;
    text-align: center;
  }
  .empty-icon { font-size: 40px; opacity: 0.5; margin-bottom: 8px; }
  .empty-title { font-size: 16px; font-weight: 600; color: var(--text-primary); }
  .empty-detail { font-size: 13px; color: var(--text-muted); }

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
    border-radius: 8px;
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
    font-size: 18px;
    cursor: pointer;
    padding: 4px 8px;
    border-radius: 4px;
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
    border-radius: 6px;
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
    font-size: 14px;
    color: var(--text-muted);
  }
  .preview-cover-placeholder span:first-child {
    font-size: 40px;
    opacity: 0.5;
  }

  /* ── Preview Meta ──────────────────── */
  .preview-meta {
    margin-bottom: 16px;
  }
  .preview-title {
    font-size: 18px;
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
  .preview-developer,
  .preview-version {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0 0 4px;
  }
  .preview-developer strong,
  .preview-version strong {
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
    border-radius: 4px;
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    font-size: 11px;
    font-weight: 500;
  }

  /* ── Overview ─────────────────────── */
  .preview-overview {
    margin-bottom: 16px;
  }
  .preview-overview h4 {
    font-size: 13px;
    font-weight: 600;
    margin: 0 0 6px;
    color: var(--text-primary);
  }
  .preview-overview p {
    font-size: 13px;
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
    font-size: 13px;
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
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--accent);
    font-size: 12px;
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
    font-size: 13px;
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
    border-radius: 6px;
    background: var(--bg-primary);
    text-decoration: none;
    transition: all 0.12s;
  }
  .download-link:hover {
    background: var(--bg-hover);
    border-color: var(--accent);
  }
  .dl-host {
    font-size: 11px;
    font-weight: 600;
    color: var(--accent);
    text-transform: capitalize;
    flex-shrink: 0;
    min-width: 70px;
  }
  .dl-name {
    flex: 1;
    font-size: 12px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dl-platform {
    font-size: 10px;
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
    border-radius: 8px;
    background: color-mix(in srgb, var(--success) 10%, transparent);
    color: var(--success);
    font-size: 13px;
    font-weight: 500;
  }
  .add-success span:first-child {
    font-size: 18px;
    font-weight: 700;
  }
  .add-error {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px 14px;
    border: 1px solid var(--danger);
    border-radius: 8px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    color: var(--danger);
    font-size: 13px;
  }
  .add-error span:first-child {
    font-size: 16px;
    font-weight: 700;
    flex-shrink: 0;
  }
</style>
