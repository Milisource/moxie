<script>
  import {onMount} from 'svelte'
  import {
    GetUpdatableGames,
    SyncSingleGame,
    CheckForUpdate,
    GetVersion,
  } from '../../wailsjs/go/main/App'

  let {onNavigate = () => {}} = $props()

  // ── State ──────────────────────────────────────────────────
  let games = $state([])
  let loading = $state(true)
  let updating = $state(new Set())     // set of game IDs currently being synced
  let updateAllRunning = $state(false)
  let error = $state('')

  // App update state (optional compact section)
  let appVersion = $state('')
  let appUpdateInfo = $state(null)
  let appChecking = $state(false)
  let appError = $state('')

  async function loadGames() {
    loading = true
    error = ''
    try {
      games = await GetUpdatableGames()
    } catch (e) {
      error = String(e)
      games = []
    }
    loading = false
  }

  async function handleUpdateGame(id) {
    if (updating.has(id)) return
    updating = new Set([...updating, id])
    try {
      await SyncSingleGame(id)
      // Reload to reflect any version changes
      await loadGames()
    } catch (e) {
      console.error('Update failed for game', id, e)
    } finally {
      const next = new Set(updating)
      next.delete(id)
      updating = next
    }
  }

  async function handleUpdateAll() {
    if (updateAllRunning || games.length === 0) return
    updateAllRunning = true
    error = ''
    try {
      for (const game of games) {
        updating = new Set([...updating, game.id])
        try {
          await SyncSingleGame(game.id)
        } catch (e) {
          console.error('Update failed for', game.title, e)
        }
        const next = new Set(updating)
        next.delete(game.id)
        updating = next
      }
      await loadGames()
    } finally {
      updateAllRunning = false
    }
  }

  async function handleCheckAppUpdate() {
    appChecking = true
    appError = ''
    appUpdateInfo = null
    try {
      appUpdateInfo = await CheckForUpdate()
    } catch (e) {
      appError = String(e)
    }
    appChecking = false
  }

  onMount(async () => {
    try {
      appVersion = await GetVersion()
    } catch (e) {
      /* ignore */
    }
    await loadGames()
  })

  // ── Derived ─────────────────────────────────────────────────
  let isUpdatingAny = $derived(updating.size > 0 || updateAllRunning)
  let count = $derived(games.length)

  // ── Engine color (reused from GameList) ─────────────────────
  function engineColor(engine) {
    const map = {
      "ren'py": '#ff6b9d',
      unity: '#6b9dff',
      'rpg maker': '#9d6bff',
      rpgmakermv: '#9d6bff',
      rpgmakermz: '#9d6bff',
      rpgmakervxace: '#9d6bff',
      html: '#ff9d6b',
      'wolf rpg': '#6bff9d',
      flash: '#ff6b6b',
      unreal: '#6bfffb',
      godot: '#9dff6b',
      electron: '#6bc9ff',
      'nw.js': '#6bc9ff',
    }
    for (const [key, color] of Object.entries(map)) {
      if (engine?.toLowerCase().includes(key)) return color
    }
    return '#9090a0'
  }
</script>

<div class="updates-view">
  <!-- ── Header ────────────────────────────────────────────── -->
  <div class="updates-header">
    <h2>Game Updates</h2>
    <p class="updates-subtitle">
      Games where a newer version is available on F95Zone.
    </p>
  </div>

  <!-- ── Error ─────────────────────────────────────────────── -->
  {#if error}
    <div class="error-section">
      <p class="error-title">Failed to load updates:</p>
      <p class="error-line">{error}</p>
    </div>
  {/if}

  <!-- ── Loading ───────────────────────────────────────────── -->
  {#if loading}
    <div class="status-section status-loading">
      <div class="spinner"></div>
      <p class="status-text">Loading games with updates…</p>
    </div>
  {:else}
    <!-- ── Action Bar ────────────────────────────────────────── -->
    {#if count > 0}
      <div class="action-bar">
        <button
          class="btn btn-primary"
          onclick={handleUpdateAll}
          disabled={isUpdatingAny}
        >
          {#if updateAllRunning}
            Updating all…
          {:else}
            Update All ({count})
          {/if}
        </button>
        <button
          class="btn btn-outline"
          onclick={() => onNavigate('sync')}
          disabled={isUpdatingAny}
        >
          Sync Now
        </button>
      </div>
    {/if}

    <!-- ── Update Progress Summary ────────────────────────── -->
    {#if isUpdatingAny}
      <div class="progress-section">
        <div class="progress-bar-bg">
          <div
            class="progress-bar-fill"
            style="width: {Math.round(
              (updating.size / Math.max(count, 1)) * 100
            )}%"
          ></div>
        </div>
        <p class="progress-label">
          Updating {updating.size} of {count} game{count !== 1 ? 's' : ''}…
        </p>
      </div>
    {/if}

    <!-- ── Game List ─────────────────────────────────────────── -->
    {#if count > 0}
      <div class="table-header">
        <span class="col-title">Title</span>
        <span class="col-engine">Engine</span>
        <span class="col-version">Version</span>
        <span class="col-action">Action</span>
      </div>

      <div class="table-body">
        {#each games as game (game.id)}
          <div class="table-row" class:row-updating={updating.has(game.id)}>
            <span class="col-title game-title">{game.title}</span>

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
              <span class="version-current">{game.version || '?'}</span>
              <span class="version-arrow">→</span>
              <span class="version-latest">{game.latestVersion}</span>
            </span>

            <span class="col-action">
              {#if updating.has(game.id)}
                <span class="updating-label">Updating…</span>
              {:else}
                <button
                  class="btn btn-sm btn-accent"
                  onclick={() => handleUpdateGame(game.id)}
                  disabled={isUpdatingAny}
                >
                  Update
                </button>
              {/if}
            </span>
          </div>
        {/each}
      </div>
    {:else}
      <!-- ── Up-to-Date State ───────────────────────────────── -->
      <div class="status-section status-success">
        <span class="status-icon">✓</span>
        <div class="status-body">
          <p class="status-title">All games up to date!</p>
          <p class="status-detail">
            Every game in your library is on the latest known version.
            New updates are discovered when you sync with F95Zone.
          </p>
        </div>
      </div>

      <div class="action-bar">
        <button
          class="btn btn-outline"
          onclick={() => onNavigate('sync')}
        >
          Sync with F95Zone
        </button>
      </div>
    {/if}

    <!-- ── App Update Section ───────────────────────────────── -->
    <div class="app-update-section">
      <details class="app-update-details">
        <summary class="app-update-summary">
          <span class="app-update-icon">⟳</span>
          <span>App Update Checker</span>
          <span class="version-tag">{appVersion || '…'}</span>
        </summary>

        <div class="app-update-body">
          <p class="app-update-desc">
            Check if a newer version of the Moxie desktop app is available.
          </p>

          <button
            class="btn btn-sm btn-outline"
            onclick={handleCheckAppUpdate}
            disabled={appChecking}
          >
            {#if appChecking}
              Checking…
            {:else}
              Check for App Update
            {/if}
          </button>

          {#if appUpdateInfo && !appUpdateInfo.hasUpdate}
            <div class="inline-status inline-ok">
              <span>✓</span>
              <span>Moxie is up to date ({appVersion}).</span>
            </div>
          {/if}

          {#if appUpdateInfo && appUpdateInfo.hasUpdate}
            <div class="inline-status inline-update">
              <span>⟳</span>
              <span>
                Update available:
                <code class="version-tag">{appUpdateInfo.currentVersion}</code>
                → <code class="version-tag">{appUpdateInfo.latestVersion}</code>
              </span>
              {#if appUpdateInfo.releaseUrl}
                <a
                  href={appUpdateInfo.releaseUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  class="release-link"
                >
                  View Release →
                </a>
              {/if}
            </div>
          {/if}

          {#if appError}
            <div class="inline-status inline-error">
              <span>✕</span>
              <span>{appError}</span>
            </div>
          {/if}
        </div>
      </details>
    </div>
  {/if}
</div>

<style>
  .updates-view {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 720px;
    margin: 0 auto;
    width: 100%;
  }

  .updates-header {
    margin-bottom: 24px;
  }
  .updates-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .updates-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Action Bar ────────────────────── */
  .action-bar {
    display: flex;
    gap: 8px;
    margin-bottom: 16px;
    flex-wrap: wrap;
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
    margin: 0;
    color: var(--text-secondary);
    line-height: 1.5;
  }

  .status-success {
    border: 1px solid var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .status-success .status-icon,
  .status-success .status-title {
    color: var(--success);
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
    to {
      transform: rotate(360deg);
    }
  }

  /* ── Progress ──────────────────────── */
  .progress-section {
    margin: 0 0 16px;
    padding: 12px 16px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
  }
  .progress-bar-bg {
    height: 6px;
    border-radius: 3px;
    background: var(--bg-tertiary);
    overflow: hidden;
    margin-bottom: 8px;
  }
  .progress-bar-fill {
    height: 100%;
    border-radius: 3px;
    background: var(--accent);
    transition: width 0.3s ease;
  }
  .progress-label {
    font-size: 12px;
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Table ──────────────────────────── */
  .table-header {
    display: grid;
    grid-template-columns: 1fr 110px 150px 110px;
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

  .table-body {
    display: flex;
    flex-direction: column;
  }

  .table-row {
    display: grid;
    grid-template-columns: 1fr 110px 150px 110px;
    gap: 8px;
    padding: 10px 12px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
    align-items: center;
    transition: background 0.08s;
  }
  .table-row:hover {
    background: var(--bg-hover);
  }
  .table-row.row-updating {
    opacity: 0.6;
  }

  .game-title {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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

  .col-version {
    font-family: var(--font-mono);
    font-size: 12px;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .version-current {
    color: var(--text-muted);
    text-decoration: line-through;
  }
  .version-arrow {
    color: var(--text-secondary);
    font-size: 11px;
  }
  .version-latest {
    color: var(--warning);
    font-weight: 600;
  }

  .text-muted {
    color: var(--text-muted);
  }

  /* ── Action column ──────────────────── */
  .col-action {
    display: flex;
    align-items: center;
    justify-content: flex-end;
  }

  .updating-label {
    font-size: 11px;
    color: var(--text-muted);
    font-style: italic;
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

  .btn-primary {
    background: var(--accent);
    color: #fff;
  }
  .btn-primary:hover:not(:disabled) {
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

  .btn-sm {
    padding: 4px 10px;
    font-size: 12px;
  }

  .btn-accent {
    background: var(--accent);
    color: #fff;
  }
  .btn-accent:hover:not(:disabled) {
    background: var(--accent-hover);
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
    margin: 0;
    font-family: var(--font-mono);
  }

  /* ── App Update Section ─────────────── */
  .app-update-section {
    margin-top: 32px;
    padding-top: 24px;
    border-top: 1px solid var(--border);
  }

  .app-update-details {
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
  }

  .app-update-summary {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    cursor: pointer;
    background: var(--bg-secondary);
    user-select: none;
    list-style: none;
  }
  .app-update-summary::-webkit-details-marker {
    display: none;
  }
  .app-update-summary::before {
    content: '▶';
    font-size: 10px;
    color: var(--text-muted);
    transition: transform 0.15s;
  }
  details[open] .app-update-summary::before {
    transform: rotate(90deg);
  }

  .app-update-icon {
    font-size: 14px;
  }
  .version-tag {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    padding: 1px 6px;
    border-radius: 4px;
  }

  .app-update-body {
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--bg-primary);
  }

  .app-update-desc {
    font-size: 12px;
    color: var(--text-secondary);
    margin: 0;
  }

  .inline-status {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    padding: 8px 12px;
    border-radius: 6px;
    flex-wrap: wrap;
  }
  .inline-ok {
    border: 1px solid var(--success);
    background: color-mix(in srgb, var(--success) 8%, transparent);
    color: var(--success);
  }
  .inline-update {
    border: 1px solid var(--warning);
    background: color-mix(in srgb, var(--warning) 8%, transparent);
    color: var(--warning);
  }
  .inline-error {
    border: 1px solid var(--danger);
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    color: var(--danger);
  }

  .release-link {
    color: var(--accent);
    text-decoration: none;
    font-weight: 500;
  }
  .release-link:hover {
    text-decoration: underline;
  }
</style>
