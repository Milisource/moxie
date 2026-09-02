<script>
  import {onMount} from 'svelte'
  import {
    GetUpdatableGames,
    GetVersion,
  } from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'

  // Presentational view: the game-update pipeline state (gameStates,
  // batchState) and its event subscriptions live in App.svelte so they
  // survive tab switches — exactly like syncState for the sync view. This
  // component renders that state and calls back into App for actions.

  let {
    gameStates = {},
    batchState = null,
    lastUpdate = 0,
    installRunning = false,
    onNavigate = () => {},
    onUpdateGame = () => {},
    onUpdateAll = () => {},
    onRetryFailed = () => {},
    onProvideFile = () => {},
    onCancel = () => {},
  } = $props()

  // ── Game List State ──────────────────────────────────────────
  let games = $state([])           // DesktopGameSummary[]
  let loading = $state(true)
  let error = $state('')

  // ── App Update State ─────────────────────────────────────────
  // The Moxie app self-update flow lives in Settings (full check → download →
  // apply, backed by App-level state that survives tab switches). This tab
  // only shows the installed version and points there — one canonical UI
  // instead of two checkers that could disagree.
  let appVersion = $state('')

  // ── Utility Formatting ───────────────────────────────────────
  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB']
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
    const val = (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)
    return `${val} ${units[i]}`
  }

  function formatSpeed(bps) {
    if (!bps || bps === 0) return ''
    const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
    const i = Math.min(Math.floor(Math.log(bps) / Math.log(1024)), units.length - 1)
    const val = (bps / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)
    return `${val} ${units[i]}`
  }

  function getGS(gameId) {
    return gameStates[gameId] || {phase: 'idle'}
  }

  function phaseLabel(gs) {
    switch (gs.phase) {
      case 'syncing':        return 'Checking…'
      case 'selecting-link': return 'Selecting link…'
      case 'downloading':    return 'Downloading…'
      case 'extracting':     return gs.totalFiles
        ? `Extracting… (${gs.filesExtracted}/${gs.totalFiles})`
        : 'Extracting…'
      case 'merging':        return 'Applying update…'
      case 'updating-db':    return 'Finalizing…'
      case 'done':           return gs.oldVersion && gs.newVersion
        ? `Updated ${gs.oldVersion} → ${gs.newVersion}`
        : 'Update complete'
      case 'error':          return 'Update failed'
      default:               return ''
    }
  }

  // ── Data Loading ─────────────────────────────────────────────
  let gamesLoadInFlight = false
  async function loadGames() {
    if (gamesLoadInFlight) return
    gamesLoadInFlight = true
    // Keep showing stale rows during a background refresh; only flash the
    // full-screen loader when there is nothing to show yet.
    if (games.length === 0) loading = true
    error = ''
    try {
      games = await GetUpdatableGames()
    } catch (e) {
      error = String(e)
      games = []
    }
    loading = false
    gamesLoadInFlight = false
  }

  // The updatable list is fetched once on mount, but the library changes
  // underneath this tab: an update batch (or a sync that discovered new
  // versions) finishes while the tab is open. App bumps lastUpdate on every
  // pipeline completion — reload whenever it moves so the list (and the
  // "All games up to date!" empty state) never goes stale.
  $effect(() => {
    if (lastUpdate) loadGames()
  })

  // Batch-complete also transitions batchState.running → false; watch it
  // directly to cover the paths that don't bump lastUpdate (e.g. a batch
  // that errors out before it starts).
  let prevBatchRunning = false
  $effect(() => {
    const running = !!batchState?.running
    if (prevBatchRunning && !running) loadGames()
    prevBatchRunning = running
  })

  // ── App Update ───────────────────────────────────────────────
  onMount(async () => {
    // Load version
    try {
      appVersion = await GetVersion()
    } catch (e) { /* ignore */ }

    // Load updatable games
    await loadGames()
  })

  // ── Derived ──────────────────────────────────────────────────
  let isUpdatingAny = $derived(
    Object.values(gameStates).some(s => s && s.phase && s.phase !== 'idle' && s.phase !== 'done' && s.phase !== 'error')
  )

  // The backend holds a single-run lock for the whole pipeline (updates and
  // installs share it), so the view must also reflect an in-flight install
  // started from a game's detail page.
  let updateInFlight = $derived(isUpdatingAny || !!batchState?.running || installRunning)

  let count = $derived(games.length)
  let doneCount = $derived(
    Object.values(gameStates).filter(s => s && s.phase === 'done').length
  )
  let allDone = $derived(count > 0 && doneCount === count)

  // Batch progress bar. The backend's batch-progress `current` counts games
  // that have STARTED, so it equals `total` while the last game is still
  // running — a naive current/total bar would read 100% prematurely. Cap the
  // bar at 97% while the batch runs; only a finished batch (running=false)
  // renders as 100%.
  let batchProgressPct = $derived.by(() => {
    if (!batchState || !batchState.total) return 0
    if (!batchState.running) return 100
    return Math.min(Math.round((batchState.current / batchState.total) * 100), 97)
  })
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
    <!-- ── All Done Message ────────────────────────────────── -->
    {#if count > 0 && allDone}
      <div class="status-section status-success">
        <span class="status-icon">✓</span>
        <div class="status-body">
          <p class="status-title">All games updated!</p>
          <p class="status-detail">
            {doneCount} game{doneCount !== 1 ? 's' : ''} updated successfully.
          </p>
        </div>
      </div>
    {/if}

    <!-- ── Batch Progress ──────────────────────────────────── -->
    {#if batchState && batchState.total > 0}
      <div class="batch-progress-section">
        <!-- Running indicator -->
        {#if batchState.running}
          <div class="batch-progress-bar-bg">
            <div
              class="batch-progress-bar-fill"
              style="width: {batchProgressPct}%"
            ></div>
          </div>
          <p class="batch-progress-label">
            Updating {batchState.current} of {batchState.total} games
          </p>
          {#if batchState.currentGameTitle}
            <p class="batch-current-game">
              <span class="spinner spinner-sm"></span>
              Currently: {batchState.currentGameTitle}
            </p>
          {/if}
        {/if}

        <!-- Per-game results -->
        {#if batchState.results.length > 0}
          <div class="batch-results">
            {#each batchState.results as result}
              <span
                class="batch-result-item"
                class:batch-result-success={result.success}
                class:batch-result-fail={!result.success}
              >
                {result.success ? '✓' : '✗'} {result.title}
              </span>
            {/each}
          </div>
        {/if}

        <!-- Batch complete summary -->
        {#if !batchState.running && batchState.results.length > 0}
          <div class="batch-summary">
            <span class="batch-summary-text">
              <span class="batch-summary-ok">{batchState.succeeded} complete</span>
              {#if batchState.failed > 0}
                , <span class="batch-summary-fail">{batchState.failed} failed</span>
              {/if}
            </span>
            {#if batchState.failed > 0}
              <button
                class="btn btn-sm btn-warning"
                onclick={onRetryFailed}
              >
                Retry Failed ({batchState.failed})
              </button>
            {/if}
          </div>
        {/if}

        {#if batchState.error}
          <div class="batch-error">
            Batch error: {batchState.error}
          </div>
        {/if}
      </div>
    {/if}

    <!-- ── Action Bar ────────────────────────────────────────── -->
    {#if count > 0 && !allDone}
      <div class="action-bar">
        <button
          class="btn btn-primary"
          onclick={() => onUpdateAll(games)}
          disabled={updateInFlight}
        >
          {#if batchState?.running}
            Updating…
          {:else}
            Update All ({count - doneCount})
          {/if}
        </button>
        <button
          class="btn btn-outline"
          onclick={() => onNavigate('sync')}
          disabled={updateInFlight}
        >
          Sync Now
        </button>
        {#if updateInFlight}
          <button
            class="btn btn-outline btn-cancel"
            onclick={onCancel}
            title="Cancel the running update or install"
          >
            Cancel
          </button>
        {/if}
      </div>
    {/if}

    <!-- ── Game List ─────────────────────────────────────────── -->
    {#if count > 0 && !allDone}
      <div class="table-header">
        <span class="col-title">Title</span>
        <span class="col-engine">Engine</span>
        <span class="col-version">Version</span>
        <span class="col-action">Status / Action</span>
      </div>

      <div class="table-body">
        {#each games as game (game.id)}
          {@const gs = getGS(game.id)}
          {@const phase = gs.phase || 'idle'}
          <div
            class="table-row"
            class:row-done={phase === 'done'}
            class:row-error={phase === 'error'}
            class:row-active={phase !== 'idle' && phase !== 'done' && phase !== 'error'}
          >
            <span class="col-title game-title" title={game.title}>
              {game.title}
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
              {#if phase === 'done'}
                {#if gs.oldVersion && gs.newVersion}
                  <span class="version-old">{gs.oldVersion}</span>
                  <span class="version-arrow">→</span>
                  <span class="version-new-done">{gs.newVersion}</span>
                {:else}
                  <span class="version-new-done">{game.latestVersion}</span>
                {/if}
              {:else if phase === 'error'}
                <span class="version-current">{game.version || '?'}</span>
                <span class="version-arrow">→</span>
                <span class="version-latest">{game.latestVersion}</span>
              {:else}
                <span class="version-current">{game.version || '?'}</span>
                <span class="version-arrow">→</span>
                <span class="version-latest">{game.latestVersion}</span>
              {/if}
            </span>

            <span class="col-action">
              {#if phase === 'idle'}
                <button
                  class="btn btn-sm btn-accent"
                  onclick={() => onUpdateGame(game.id)}
                  disabled={updateInFlight}
                >
                  Update
                </button>

              {:else if phase === 'downloading'}
                <div class="cell-download-progress">
                  <div class="cell-progress-bg">
                    <div
                      class="cell-progress-fill"
                      style="width: {gs.percent || 0}%"
                    ></div>
                  </div>
                  <span class="cell-progress-text">
                    {gs.percent || 0}%
                    {#if gs.speed}
                      <span class="cell-speed">— {formatSpeed(gs.speed)}</span>
                    {/if}
                  </span>
                </div>

              {:else if phase === 'extracting'}
                <div class="cell-status-row">
                  <span class="spinner spinner-sm"></span>
                  <span class="cell-status-text">
                    {#if gs.totalFiles}
                      Extracting… ({gs.filesExtracted}/{gs.totalFiles})
                    {:else}
                      Extracting…
                    {/if}
                  </span>
                </div>

              {:else if phase === 'syncing' || phase === 'selecting-link'}
                <div class="cell-status-row">
                  <span class="spinner spinner-sm"></span>
                  <span class="cell-status-text">{phaseLabel(gs)}</span>
                </div>

              {:else if phase === 'merging'}
                <div class="cell-status-row">
                  <span class="spinner spinner-sm"></span>
                  <span class="cell-status-text">Applying update…</span>
                </div>

              {:else if phase === 'updating-db'}
                <div class="cell-status-row">
                  <span class="spinner spinner-sm"></span>
                  <span class="cell-status-text">Finalizing…</span>
                </div>

              {:else if phase === 'done'}
                <div class="cell-status-row cell-status-done">
                  <span class="cell-done-icon">✓</span>
                  <span class="cell-status-text">
                    {#if gs.oldVersion && gs.newVersion}
                      Updated {gs.oldVersion} → {gs.newVersion}
                    {:else}
                      Update complete
                    {/if}
                  </span>
                </div>

              {:else if phase === 'error'}
                <div class="cell-error-column">
                  <span class="cell-error-text" title={gs.error}>
                    {gs.error ? (gs.error.length > 60 ? gs.error.slice(0, 60) + '…' : gs.error) : 'Error'}
                  </span>
                  {#if gs.manualRequired}
                    <div class="cell-manual-row">
                      <span class="cell-manual-hint">
                        Auto-download blocked{gs.manualHost ? ` (${gs.manualHost})` : ''} — download the file in your browser, then point moxie at it.
                      </span>
                      <button
                        class="btn btn-sm btn-primary"
                        onclick={() => onProvideFile(game.id)}
                        disabled={updateInFlight}
                      >
                        Provide file…
                      </button>
                    </div>
                  {/if}
                  <button
                    class="btn btn-sm btn-warning"
                    onclick={() => onUpdateGame(game.id)}
                    disabled={updateInFlight}
                  >
                    Retry
                  </button>
                </div>
              {/if}
            </span>
          </div>
        {/each}
      </div>
    {:else if count === 0}
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
      <div class="app-update-card">
        <div class="app-update-card-body">
          <span class="app-update-icon">⟳</span>
          <div>
            <p class="app-update-card-title">Application update</p>
            <p class="app-update-card-desc">
              Check for a new Moxie desktop release and download it in Settings.
            </p>
          </div>
          <span class="version-tag">{appVersion || '…'}</span>
        </div>
        <button class="btn btn-sm btn-outline" onclick={() => onNavigate('settings')}>
          Open Settings
        </button>
      </div>
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
    align-items: center;
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
  .spinner-sm {
    width: 12px;
    height: 12px;
    border-width: 1.5px;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* ── Batch Progress ─────────────────── */
  .batch-progress-section {
    margin: 0 0 16px;
    padding: 14px 16px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
  }
  .batch-progress-bar-bg {
    height: 6px;
    border-radius: 3px;
    background: var(--bg-tertiary);
    overflow: hidden;
    margin-bottom: 8px;
  }
  .batch-progress-bar-fill {
    height: 100%;
    border-radius: 3px;
    background: var(--accent);
    transition: width 0.3s ease;
  }
  .batch-progress-label {
    font-size: 12px;
    color: var(--text-secondary);
    margin: 0 0 6px;
    font-weight: 600;
  }
  .batch-current-game {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--accent);
    margin: 0 0 8px;
  }
  .batch-results {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    margin: 8px 0;
  }
  .batch-result-item {
    font-size: 11px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 200px;
  }
  .batch-result-success {
    color: var(--success);
  }
  .batch-result-fail {
    color: var(--danger);
  }
  .batch-summary {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }
  .batch-summary-text {
    font-size: 13px;
    font-weight: 600;
  }
  .batch-summary-ok {
    color: var(--success);
  }
  .batch-summary-fail {
    color: var(--danger);
  }
  .batch-error {
    font-size: 12px;
    color: var(--danger);
    margin-top: 8px;
    padding: 6px 8px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    border-radius: 4px;
  }

  /* ── Table ──────────────────────────── */
  .table-header {
    display: grid;
    grid-template-columns: 1fr 110px 150px 1fr;
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
    grid-template-columns: 1fr 110px 150px 1fr;
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
  .table-row.row-done {
    opacity: 0.55;
  }
  .table-row.row-done:hover {
    background: transparent;
  }
  .table-row.row-error {
    background: color-mix(in srgb, var(--danger) 5%, transparent);
  }
  .table-row.row-active {
    background: color-mix(in srgb, var(--accent) 4%, transparent);
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
  .version-new-done {
    color: var(--success);
    font-weight: 600;
  }
  .version-old {
    color: var(--text-muted);
    text-decoration: line-through;
  }

  .text-muted {
    color: var(--text-muted);
  }

  /* ── Action column ──────────────────── */
  .col-action {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    min-width: 0;
    gap: 6px;
  }

  /* ── Cell: Download progress ─────────── */
  .cell-download-progress {
    display: flex;
    flex-direction: column;
    gap: 3px;
    width: 100%;
    min-width: 120px;
  }
  .cell-progress-bg {
    height: 4px;
    border-radius: 2px;
    background: var(--bg-tertiary);
    overflow: hidden;
  }
  .cell-progress-fill {
    height: 100%;
    border-radius: 2px;
    background: var(--accent);
    transition: width 0.25s ease;
  }
  .cell-progress-text {
    font-size: 11px;
    color: var(--accent);
    font-family: var(--font-mono);
    white-space: nowrap;
  }
  .cell-speed {
    color: var(--text-muted);
  }

  /* ── Cell: Status row (spinner + text) ─ */
  .cell-status-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .cell-status-text {
    font-size: 12px;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cell-status-done {
    gap: 4px;
  }
  .cell-done-icon {
    color: var(--success);
    font-weight: 700;
    font-size: 14px;
  }

  /* ── Cell: Error ─────────────────────── */
  .cell-error-column {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 4px;
    max-width: 100%;
  }
  .cell-error-text {
    font-size: 11px;
    color: var(--danger);
    font-family: var(--font-mono);
    text-align: right;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
    white-space: nowrap;
  }
  .cell-manual-row {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 4px;
    max-width: 100%;
  }
  .cell-manual-hint {
    font-size: 11px;
    color: var(--text-secondary);
    text-align: right;
    max-width: 320px;
    line-height: 1.35;
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

  .btn-cancel {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .btn-cancel:hover:not(:disabled) {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
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

  .btn-warning {
    background: var(--warning);
    color: #000;
  }
  .btn-warning:hover:not(:disabled) {
    filter: brightness(1.1);
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

  .app-update-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
    flex-wrap: wrap;
  }

  .app-update-card-body {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    flex: 1;
  }

  .app-update-icon {
    font-size: 16px;
    flex-shrink: 0;
  }

  .app-update-card-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0 0 2px;
  }

  .app-update-card-desc {
    font-size: 12px;
    color: var(--text-secondary);
    margin: 0;
    line-height: 1.4;
  }

  .version-tag {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    padding: 1px 6px;
    border-radius: 4px;
    flex-shrink: 0;
  }

  .btn-sm {
    padding: 4px 10px;
    font-size: 12px;
  }

  .btn-outline {
    background: transparent;
    color: var(--text-primary);
    border: 1px solid var(--border);
    flex-shrink: 0;
  }
  .btn-outline:hover:not(:disabled) {
    background: var(--bg-hover);
  }
</style>
