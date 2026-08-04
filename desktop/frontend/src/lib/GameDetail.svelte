<script>
  import {onMount} from 'svelte'
  import {GetGameDetail, PlayGame, RemoveGame, SetGameStatus, RenameGame, SetGameWinePrefix, SyncSingleGame, EditGame} from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'

  let {gameId = null, onBack = () => {}, onUpdate = () => {}} = $props()

  let detail = $state(null)
  let loading = $state(true)
  let error = $state('')
  let showFullOverview = $state(false)
  let launchStatus = $state({msg: '', error: ''})

  // ── Inline rename ───────────────────────────────
  let showRenameInput = $state(false)
  let renameTitle = $state('')

  // ── Inline field editing ────────────────────────
  let editExePath = $state({active: false, value: ''})
  let editVersion = $state({active: false, value: ''})
  let editNotes = $state({active: false, value: ''})
  let editWinePrefix = $state({active: false, value: ''})

  // ── Toggle sections ─────────────────────────────
  let showDownloads = $state(false)
  let showPlayHistory = $state(false)

  async function loadDetail() {
    if (!gameId) return
    loading = true
    error = ''
    try {
      detail = await GetGameDetail(gameId)
    } catch (e) {
      error = String(e)
    }
    loading = false
  }

  function statusLabel(s) {
    const labels = {active: 'Active', completed: 'Completed', abandoned: 'Abandoned', on_hold: 'On Hold', unknown: 'Unknown'}
    return labels[s] || s || 'Unknown'
  }

  // ── Engine colors — imported from shared module ───────────────
  // See engineColors.js for the canonical palette matching TUI styles

  function formatDuration(s) {
    if (!s) return ''
    const m = Math.floor(s / 60)
    const sec = s % 60
    if (m === 0) return `${sec}s`
    return `${m}m ${sec}s`
  }

  // ── Stock engine list for dropdown ──────────────
  const engines = ['ren\'py', 'unity', 'rpgmakermv', 'rpgmakermz', 'rpgmakervxace', 'html', 'wolf rpg', 'flash', 'unreal', 'godot', 'electron', 'nw.js']

  // ── Action handlers ─────────────────────────────
  async function handleStatusChange(e) {
    const newStatus = e.target.value
    if (!gameId || newStatus === detail.status) return
    try {
      await SetGameStatus(gameId, newStatus)
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to update status:', err)
    }
  }

  async function handleEngineChange(e) {
    const newEngine = e.target.value
    if (!gameId || newEngine === detail.engine) return
    try {
      await EditGame(gameId, {engine: newEngine, version: '', exePath: '', notes: ''})
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to update engine:', err)
    }
  }

  function handleRenameStart() {
    renameTitle = detail.title
    showRenameInput = true
  }

  async function handleRenameSave() {
    const trimmed = renameTitle.trim()
    if (!trimmed || trimmed === detail.title) {
      showRenameInput = false
      return
    }
    try {
      await RenameGame(gameId, trimmed)
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to rename:', err)
    }
    showRenameInput = false
  }

  function startExePathEdit() {
    editExePath = {active: true, value: detail.exePath || ''}
  }

  async function saveExePath() {
    try {
      await EditGame(gameId, {engine: '', version: '', exePath: editExePath.value, notes: ''})
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to save exe path:', err)
    }
    editExePath = {active: false, value: ''}
  }

  function startWinePrefixEdit() {
    editWinePrefix = {active: true, value: detail.winePrefix || ''}
  }

  async function saveWinePrefix() {
    try {
      await SetGameWinePrefix(gameId, editWinePrefix.value)
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to save wine prefix:', err)
    }
    editWinePrefix = {active: false, value: ''}
  }

  function startVersionEdit() {
    editVersion = {active: true, value: detail.version || ''}
  }

  async function saveVersion() {
    try {
      await EditGame(gameId, {engine: '', version: editVersion.value, exePath: '', notes: ''})
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to save version:', err)
    }
    editVersion = {active: false, value: ''}
  }

  function startNotesEdit() {
    editNotes = {active: true, value: detail.notes || ''}
  }

  async function saveNotes() {
    try {
      await EditGame(gameId, {engine: '', version: '', exePath: '', notes: editNotes.value})
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to save notes:', err)
    }
    editNotes = {active: false, value: ''}
  }

  async function handleRemove() {
    if (!window.confirm(`Are you sure you want to remove "${detail.title}" from your library?`)) return
    try {
      await RemoveGame(gameId, false)
      onBack()
      onUpdate()
    } catch (err) {
      console.error('Failed to remove:', err)
    }
  }

  async function handleSync() {
    try {
      await SyncSingleGame(gameId)
      await loadDetail()
      onUpdate()
    } catch (err) {
      console.error('Failed to sync:', err)
    }
  }

  async function handlePlay() {
    launchStatus = {msg: '', error: ''}
    try {
      const msg = await PlayGame(gameId)
      launchStatus = {msg, error: ''}
      await loadDetail()
      onUpdate()
    } catch (err) {
      launchStatus = {msg: '', error: String(err).replace(/^Error:\s*/, '')}
    }
  }

  onMount(loadDetail)
</script>

<div class="detail">
  <!-- Back Button -->
  <div class="detail-header">
    <button class="back-btn" onclick={onBack}>← Back to Library</button>
  </div>

  {#if loading}
    <div class="loading-state"><div class="spinner"></div><p>Loading game details…</p></div>
  {:else if error}
    <div class="error-state"><p>Error: {error}</p><button class="back-btn" onclick={onBack}>Go Back</button></div>
  {:else if detail}
    <div class="detail-content">
      <!-- Title + Badges -->
      <div class="title-section">
        <div class="title-main">
          {#if showRenameInput}
            <!-- svelte-ignore a11y_autofocus -->
            <input
              class="rename-input"
              type="text"
              bind:value={renameTitle}
              onkeydown={(e) => {
                if (e.key === 'Enter') handleRenameSave()
                if (e.key === 'Escape') showRenameInput = false
              }}
              autofocus
            />
            <button class="btn btn-sm btn-primary" onclick={handleRenameSave}>Save</button>
            <button class="btn btn-sm" onclick={() => showRenameInput = false}>Cancel</button>
          {:else}
            <h1>{detail.title}</h1>
            {#if detail.latestVersion && detail.version && detail.latestVersion !== detail.version}
              <span class="update-badge" title="Update: {detail.version} → {detail.latestVersion}">Update Available</span>
            {/if}
          {/if}
        </div>
        <div class="badges">
          {#if detail.engine}
            <span class="engine-badge" style="--ec: {engineColor(detail.engine)}">{detail.engine}</span>
          {/if}
          <span class="status-badge status-{detail.status || 'unknown'}">{statusLabel(detail.status)}</span>
        </div>
      </div>

      <!-- Two-column layout -->
      <div class="detail-grid">
        <!-- Left: Cover -->
        <div class="cover-section">
          {#if detail.coverUrl}
            <img
              class="cover-img"
              src={detail.coverUrl}
              alt="{detail.title} cover"
              loading="lazy"
            />
          {:else}
            <div class="cover-placeholder">
              <span class="cover-icon">◆</span>
              <span>No Cover</span>
            </div>
          {/if}
        </div>

        <!-- Right: Metadata -->
        <div class="meta-section">
          {#if detail.developer}
            <div class="meta-row">
              <span class="meta-label">Developer</span>
              <span class="meta-value">{detail.developer}</span>
            </div>
          {/if}

          {#if detail.engine}
            <div class="meta-row">
              <span class="meta-label">Engine</span>
              <span class="meta-value">
                <span class="select-arrow">
                  <select class="field-select" onchange={handleEngineChange}>
                    {#each engines as eng}
                      <option value={eng} selected={detail.engine === eng}>{eng}</option>
                    {/each}
                  </select>
                </span>
              </span>
            </div>
          {/if}

          {#if detail.version || detail.latestVersion}
            <div class="meta-row">
              <span class="meta-label">Version</span>
              <span class="meta-value">
                {#if editVersion.active}
                  <input
                    class="inline-edit-input"
                    type="text"
                    bind:value={editVersion.value}
                    onkeydown={(e) => { if (e.key === 'Enter') saveVersion(); if (e.key === 'Escape') editVersion = {active: false, value: ''}; }}
                  />
                  <button class="btn btn-xs btn-primary" onclick={saveVersion}>Save</button>
                  <button class="btn btn-xs" onclick={() => editVersion = {active: false, value: ''}}>Cancel</button>
                {:else}
                  <span class="editable-field" onclick={startVersionEdit} title="Click to edit">
                    {detail.version || 'unknown'}
                    {#if detail.latestVersion && detail.latestVersion !== detail.version}
                      <span class="version-update">→ {detail.latestVersion}</span>
                    {/if}
                  </span>
                {/if}
              </span>
            </div>
          {/if}

          {#if detail.status}
            <div class="meta-row">
              <span class="meta-label">Status</span>
              <span class="meta-value">
                <span class="select-arrow">
                  <select class="field-select" onchange={handleStatusChange}>
                    {#each ['active', 'completed', 'abandoned', 'on_hold', 'unknown'] as s}
                      <option value={s} selected={detail.status === s}>{statusLabel(s)}</option>
                    {/each}
                  </select>
                </span>
              </span>
            </div>
          {/if}

          {#if detail.exePath !== undefined}
            <div class="meta-row">
              <span class="meta-label">Executable</span>
              <span class="meta-value">
                {#if editExePath.active}
                  <input
                    class="inline-edit-input"
                    type="text"
                    bind:value={editExePath.value}
                    placeholder="/path/to/game.exe"
                    onkeydown={(e) => { if (e.key === 'Enter') saveExePath(); if (e.key === 'Escape') editExePath = {active: false, value: ''}; }}
                  />
                  <button class="btn btn-xs btn-primary" onclick={saveExePath}>Save</button>
                  <button class="btn btn-xs" onclick={() => editExePath = {active: false, value: ''}}>Cancel</button>
                {:else}
                  <span class="editable-field mono" onclick={startExePathEdit} title="Click to edit">
                    {detail.exePath || '—'}
                  </span>
                {/if}
              </span>
            </div>
          {/if}

          {#if detail.winePrefix !== undefined}
            <div class="meta-row">
              <span class="meta-label">Wine Prefix</span>
              <span class="meta-value">
                {#if editWinePrefix.active}
                  <input
                    class="inline-edit-input"
                    type="text"
                    bind:value={editWinePrefix.value}
                    placeholder="/path/to/wineprefix"
                    title="Leave empty to clear"
                    onkeydown={(e) => { if (e.key === 'Enter') saveWinePrefix(); if (e.key === 'Escape') editWinePrefix = {active: false, value: ''}; }}
                  />
                  <button class="btn btn-xs btn-primary" onclick={saveWinePrefix}>Save</button>
                  <button class="btn btn-xs" onclick={() => editWinePrefix = {active: false, value: ''}}>Cancel</button>
                {:else}
                  <span class="editable-field mono" onclick={startWinePrefixEdit} title="Click to edit">
                    {detail.winePrefix || '— (system default)'}
                  </span>
                {/if}
              </span>
            </div>
          {/if}

          {#if detail.sizeLabel}
            <div class="meta-row">
              <span class="meta-label">Size</span>
              <span class="meta-value">{detail.sizeLabel}</span>
            </div>
          {/if}

          {#if detail.path}
            <div class="meta-row">
              <span class="meta-label">Path</span>
              <span class="meta-value mono">{detail.path}</span>
            </div>
          {/if}

          {#if detail.steamAppId && detail.steamAppId > 0}
            <div class="meta-row">
              <span class="meta-label">Steam</span>
              <span class="meta-value">
                <a href="https://store.steampowered.com/app/{detail.steamAppId}" target="_blank" rel="noopener">
                  App {detail.steamAppId}
                </a>
              </span>
            </div>
          {/if}

          {#if detail.tags && detail.tags.length}
            <div class="meta-row">
              <span class="meta-label">Tags</span>
              <span class="meta-value tags">
                {#each detail.tags as tag}
                  <span class="tag">{tag}</span>
                {/each}
              </span>
            </div>
          {/if}

          {#if detail.f95Url}
            <div class="meta-row">
              <span class="meta-label">F95Zone</span>
              <span class="meta-value"><a href={detail.f95Url} target="_blank" rel="noopener">{detail.f95Url}</a></span>
            </div>
          {/if}

          {#if detail.storeLinks && Object.keys(detail.storeLinks).length > 0}
            <div class="meta-row">
              <span class="meta-label">Stores</span>
              <span class="meta-value store-links">
                {#each Object.entries(detail.storeLinks) as [store, url]}
                  <a href={url} target="_blank" rel="noopener" class="store-badge">{store}</a>
                {/each}
              </span>
            </div>
          {/if}
        </div>
      </div>

      <!-- Overview -->
      {#if detail.overview}
        <div class="overview-section">
          <h3>Overview</h3>
          <div class="overview-text" class:truncated={!showFullOverview && detail.overview.length > 500}>
            {showFullOverview ? detail.overview : detail.overview.slice(0, 500)}
            {#if detail.overview.length > 500}
              <button class="show-more" onclick={() => showFullOverview = !showFullOverview}>
                {showFullOverview ? 'Show less' : '… Show more'}
              </button>
            {/if}
          </div>
        </div>
      {/if}

      <!-- Notes -->
      {#if detail.notes !== undefined || editNotes.active}
        <div class="section-card notes-section">
          <div class="section-card-header">
            <h3>Notes</h3>
            {#if !editNotes.active}
              <button class="btn btn-xs" onclick={startNotesEdit}>
                {detail.notes ? 'Edit' : 'Add Note'}
              </button>
            {/if}
          </div>
          {#if editNotes.active}
            <textarea
              class="notes-textarea"
              bind:value={editNotes.value}
              placeholder="Add notes about this game..."
              rows="3"
            ></textarea>
            <div class="inline-edit-actions">
              <button class="btn btn-xs btn-primary" onclick={saveNotes}>Save</button>
              <button class="btn btn-xs" onclick={() => editNotes = {active: false, value: ''}}>Cancel</button>
            </div>
          {:else if detail.notes}
            <p class="notes-text">{detail.notes}</p>
          {:else}
            <p class="notes-empty">No notes.</p>
          {/if}
        </div>
      {/if}

      <!-- Download Links -->
      {#if detail.downloadLinks && detail.downloadLinks.length > 0}
        <div class="section-card">
          <div class="section-card-header">
            <h3>Download Links ({detail.downloadLinks.length})</h3>
            <button class="btn btn-xs" onclick={() => showDownloads = !showDownloads}>
              {showDownloads ? 'Hide' : 'Show'}
            </button>
          </div>
          {#if showDownloads}
            <div class="dl-list">
              {#each detail.downloadLinks as link}
                <div class="dl-row">
                  <span class="dl-host">{link.host}</span>
                  <span class="dl-name">{link.name}</span>
                  <span class="dl-platform">{link.platform}</span>
                  {#if link.isDead}
                    <span class="dl-dead">dead</span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}

      <!-- Play History -->
      {#if detail.playHistory && detail.playHistory.length > 0}
        <div class="section-card">
          <div class="section-card-header">
            <h3>Play History ({detail.playHistory.length})</h3>
            <button class="btn btn-xs" onclick={() => showPlayHistory = !showPlayHistory}>
              {showPlayHistory ? 'Hide' : 'Show'}
            </button>
          </div>
          {#if showPlayHistory}
            <div class="ph-list">
              {#each detail.playHistory as entry}
                <div class="ph-row">
                  <span class="ph-date">{new Date(entry.playedAt).toLocaleDateString()}</span>
                  <span class="ph-time">{new Date(entry.playedAt).toLocaleTimeString()}</span>
                  {#if entry.platform}
                    <span class="ph-platform">{entry.platform}</span>
                  {/if}
                  {#if entry.durationS}
                    <span class="ph-duration">{formatDuration(entry.durationS)}</span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}

      <!-- Action Buttons -->
      <div class="action-section">
        {#if launchStatus.msg}
          <div class="launch-notice launch-ok">{launchStatus.msg}</div>
        {/if}
        {#if launchStatus.error}
          <div class="launch-notice launch-error">{launchStatus.error}</div>
        {/if}
        <button class="btn btn-primary btn-play" onclick={handlePlay}>▶ Play</button>
        {#if detail.f95Url}
          <button class="btn btn-primary" onclick={handleSync}>Sync from F95Zone</button>
        {/if}
        <button class="btn" onclick={handleRenameStart}>Rename</button>
        <button class="btn btn-danger" onclick={handleRemove}>Remove Game</button>
      </div>
    </div>
  {/if}
</div>

<style>
  .detail {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: auto;
  }

  .detail-header {
    flex-shrink: 0;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-secondary);
  }

  .back-btn {
    padding: 4px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: transparent;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
  }
  .back-btn:hover { background: var(--bg-hover); }

  .detail-content {
    flex: 1;
    padding: 24px;
    max-width: 960px;
    margin: 0 auto;
    width: 100%;
  }

  /* ── Title ──────────────────────────────── */
  .title-section {
    margin-bottom: 24px;
  }
  .title-main {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 8px;
  }
  .title-main h1 {
    font-size: 24px;
    font-weight: 700;
    margin: 0;
  }
  .update-badge {
    padding: 2px 10px;
    border-radius: 12px;
    background: color-mix(in srgb, var(--warning) 20%, transparent);
    color: var(--warning);
    font-size: 12px;
    font-weight: 600;
  }
  .badges {
    display: flex;
    gap: 8px;
  }
  .engine-badge {
    padding: 2px 10px;
    border-radius: 4px;
    font-size: 12px;
    font-weight: 600;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
  }
  .status-badge {
    padding: 2px 10px;
    border-radius: 4px;
    font-size: 12px;
    font-weight: 500;
  }
  .status-active { background: color-mix(in srgb, var(--success) 15%, transparent); color: var(--success); }
  .status-completed { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); }
  .status-abandoned { background: color-mix(in srgb, var(--text-muted) 15%, transparent); color: var(--text-muted); }
  .status-on_hold { background: color-mix(in srgb, var(--warning) 15%, transparent); color: var(--warning); }
  .status-unknown { background: color-mix(in srgb, var(--text-muted) 10%, transparent); color: var(--text-muted); }

  /* ── Grid ───────────────────────────────── */
  .detail-grid {
    display: grid;
    grid-template-columns: 280px 1fr;
    gap: 24px;
    margin-bottom: 24px;
  }

  .cover-section {
    display: flex;
    justify-content: center;
  }
  .cover-img {
    width: 100%;
    max-width: 260px;
    border-radius: 8px;
    box-shadow: 0 4px 20px rgba(0,0,0,0.3);
  }
  .cover-placeholder {
    width: 100%;
    max-width: 260px;
    aspect-ratio: 3/4;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    background: var(--bg-tertiary);
    border-radius: 8px;
    color: var(--text-muted);
  }
  .cover-icon { font-size: 40px; opacity: 0.4; }

  .meta-section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .meta-row {
    display: flex;
    gap: 8px;
  }
  .meta-label {
    min-width: 90px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    flex-shrink: 0;
  }
  .meta-value {
    font-size: 14px;
    color: var(--text-primary);
  }
  .meta-value a { color: var(--accent); text-decoration: none; }
  .meta-value a:hover { text-decoration: underline; }

  .version-update {
    color: var(--warning);
    font-weight: 600;
  }

  .tags { display: flex; flex-wrap: wrap; gap: 4px; }
  .tag {
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--bg-tertiary);
    font-size: 11px;
    color: var(--text-secondary);
  }

  /* ── Overview ───────────────────────────── */
  .overview-section {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 16px;
  }
  .overview-section h3 {
    font-size: 14px;
    font-weight: 600;
    margin-bottom: 8px;
    color: var(--text-secondary);
  }
  .overview-text {
    font-size: 13px;
    line-height: 1.6;
    color: var(--text-primary);
    white-space: pre-wrap;
  }
  .overview-text.truncated {
    max-height: 200px;
    overflow: hidden;
    position: relative;
  }
  .show-more {
    display: block;
    margin-top: 8px;
    background: none;
    border: none;
    color: var(--accent);
    font-size: 13px;
    cursor: pointer;
    padding: 0;
  }
  .show-more:hover { color: var(--accent-hover); }

  /* ── States ─────────────────────────────── */
  .loading-state, .error-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    flex: 1;
    gap: 12px;
    color: var(--text-muted);
  }
  .spinner {
    width: 24px; height: 24px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── Inline Rename ─────────────────────────── */
  .rename-input {
    flex: 1;
    padding: 6px 10px;
    border: 1px solid var(--accent);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 20px;
    font-weight: 700;
    outline: none;
    min-width: 0;
  }

  /* ── Buttons ───────────────────────────────── */
  .btn {
    padding: 7px 16px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: transparent;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    transition: background 0.12s;
  }
  .btn:hover { background: var(--bg-hover); }
  .btn-sm {
    padding: 4px 10px;
    font-size: 12px;
  }
  .btn-primary {
    background: var(--accent);
    color: #fff;
    border-color: var(--accent);
  }
  .btn-primary:hover { background: var(--accent-hover); }
  .btn-danger {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 30%, transparent);
  }
  .btn-danger:hover { background: color-mix(in srgb, var(--danger) 10%, transparent); }

  /* ── Field Select (Engine, Status) ─────────── */
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
  .field-select {
    padding: 2px 22px 2px 8px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
    cursor: pointer;
    max-width: 180px;
    /* Strip native OS widget — CSS controls appearance fully */
    -webkit-appearance: none;
    -moz-appearance: none;
    appearance: none;
  }
  .field-select:focus { border-color: var(--accent); }
  .field-select:hover { border-color: var(--accent-hover); }
  .field-select option {
    background: var(--bg-primary);
    color: var(--text-primary);
  }

  /* ── Inline Edit Input ─────────────────────── */
  .inline-edit-input {
    padding: 3px 6px;
    border: 1px solid var(--accent);
    border-radius: 4px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
    width: 180px;
  }
  .inline-edit-actions {
    display: flex;
    gap: 4px;
    margin-top: 6px;
  }

  /* ── Editable Field ────────────────────────── */
  .editable-field {
    cursor: pointer;
    border-bottom: 1px dashed var(--text-muted);
    padding-bottom: 1px;
  }
  .editable-field:hover {
    border-bottom-color: var(--accent);
    color: var(--accent);
  }

  /* ── Button xs ─────────────────────────────── */
  .btn-xs {
    padding: 2px 8px;
    font-size: 11px;
    border-radius: 4px;
  }

  /* ── Mono text ─────────────────────────────── */
  .mono {
    font-family: var(--font-mono);
    font-size: 12px;
  }

  /* ── Store Links ───────────────────────────── */
  .store-links { display: flex; flex-wrap: wrap; gap: 4px; }
  .store-badge {
    padding: 1px 7px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent);
    text-decoration: none;
    text-transform: capitalize;
  }
  .store-badge:hover { background: var(--accent); color: #fff; }

  /* ── Section Cards ─────────────────────────── */
  .section-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 16px;
    margin-top: 16px;
  }
  .section-card h3 {
    font-size: 14px;
    font-weight: 600;
    margin: 0;
    color: var(--text-secondary);
  }
  .section-card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  /* ── Notes ─────────────────────────────────── */
  .notes-text {
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-primary);
    white-space: pre-wrap;
    margin: 0;
  }
  .notes-empty {
    font-size: 13px;
    color: var(--text-muted);
    margin: 0;
    font-style: italic;
  }
  .notes-textarea {
    width: 100%;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    font-family: inherit;
    outline: none;
    resize: vertical;
    box-sizing: border-box;
  }
  .notes-textarea:focus { border-color: var(--accent); }

  /* ── Download Links ────────────────────────── */
  .dl-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 240px;
    overflow-y: auto;
  }
  .dl-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    font-size: 12px;
    border-radius: 4px;
  }
  .dl-row:hover { background: var(--bg-hover); }
  .dl-host {
    font-weight: 600;
    color: var(--text-primary);
    min-width: 70px;
  }
  .dl-name {
    flex: 1;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dl-platform {
    font-size: 11px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .dl-dead {
    font-size: 10px;
    color: var(--danger);
    font-weight: 600;
    text-transform: uppercase;
  }

  /* ── Play History ──────────────────────────── */
  .ph-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 240px;
    overflow-y: auto;
  }
  .ph-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    font-size: 12px;
    border-radius: 4px;
  }
  .ph-row:hover { background: var(--bg-hover); }
  .ph-date {
    min-width: 80px;
    color: var(--text-primary);
  }
  .ph-time {
    color: var(--text-muted);
    min-width: 60px;
  }
  .ph-platform {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 11px;
  }
  .ph-duration {
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: 11px;
    margin-left: auto;
  }

  /* ── Action Section ────────────────────────── */
  .action-section {
    display: flex;
    gap: 8px;
    margin-top: 24px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
    flex-wrap: wrap;
    align-items: center;
  }

  .btn-play {
    font-weight: 600;
  }

  .launch-notice {
    width: 100%;
    padding: 8px 12px;
    border-radius: 6px;
    font-size: 13px;
  }
  .launch-ok {
    color: #2ecc71;
    background: color-mix(in srgb, #2ecc71 12%, transparent);
    border: 1px solid color-mix(in srgb, #2ecc71 35%, transparent);
  }
  .launch-error {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 35%, transparent);
  }
</style>
