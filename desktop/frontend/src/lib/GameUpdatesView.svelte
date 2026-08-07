<script>
  import {onMount, onDestroy} from 'svelte'
  import {EventsOn} from '../../wailsjs/runtime/runtime'
  import {
    GetUpdatableGames,
    GetVersion,
    CheckForUpdate,
    DownloadGameUpdate,
    DownloadAllUpdates,
    CancelGameUpdate,
  } from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'

  let {onNavigate = () => {}, onUpdateCompleted = () => {}} = $props()

  // ── Game List State ──────────────────────────────────────────
  let games = $state([])           // DesktopGameSummary[]
  let loading = $state(true)
  let error = $state('')

  // ── Per-Game State Machine ───────────────────────────────────
  // Phase: idle | syncing | selecting-link | downloading | extracting | merging | updating-db | done | error
  // Each entry: { phase, percent, speed, bytesDownloaded, totalBytes, filesExtracted, totalFiles, currentFile, error, oldVersion, newVersion }
  /** @type {Record<number, {phase:string,percent:number,speed:number,bytesDownloaded:number,totalBytes:number,filesExtracted:number,totalFiles:number,currentFile:string,error:string,oldVersion:string,newVersion:string}>} */
  let gameStates = $state({})

  // ── Batch State ──────────────────────────────────────────────
  /** @type {{running:boolean,current:number,total:number,currentGameTitle:string,results:Array,error:string}|null} */
  let batchState = $state(null)

  // ── App Update State ─────────────────────────────────────────
  let appVersion = $state('')
  let appUpdateInfo = $state(null)
  let appChecking = $state(false)
  let appError = $state('')

  // ── Unsubscribe functions ────────────────────────────────────
  let unsubPhase = null
  let unsubDownload = null
  let unsubExtract = null
  let unsubError = null
  let unsubComplete = null
  let unsubBatchStart = null
  let unsubBatchProgress = null
  let unsubGameDone = null
  let unsubBatchComplete = null
  let unsubCancelled = null
  let unsubIdle = null

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

  // ── Update Actions ───────────────────────────────────────────
  function initGameState(gameId) {
    gameStates = {
      ...gameStates,
      [gameId]: {phase: 'syncing', percent: 0, speed: 0, bytesDownloaded: 0, totalBytes: 0, filesExtracted: 0, totalFiles: 0, currentFile: '', error: '', oldVersion: '', newVersion: ''},
    }
  }

  async function handleUpdateGame(gameId) {
    const gs = getGS(gameId)
    if (gs.phase !== 'idle' && gs.phase !== 'error') return
    initGameState(gameId)
    try {
      await DownloadGameUpdate(gameId)
    } catch (e) {
      gameStates = {
        ...gameStates,
        [gameId]: {...(gameStates[gameId] || {}), phase: 'error', error: String(e)},
      }
    }
  }

  async function handleRetry(gameId) {
    initGameState(gameId)
    try {
      await DownloadGameUpdate(gameId)
    } catch (e) {
      gameStates = {
        ...gameStates,
        [gameId]: {...(gameStates[gameId] || {}), phase: 'error', error: String(e)},
      }
    }
  }

  async function handleUpdateAll() {
    if (batchState?.running) return

    // Initialize all games as syncing
    const newStates = {...gameStates}
    for (const game of games) {
      newStates[game.id] = {phase: 'syncing', percent: 0, speed: 0, bytesDownloaded: 0, totalBytes: 0, filesExtracted: 0, totalFiles: 0, currentFile: '', error: '', oldVersion: '', newVersion: ''}
    }
    gameStates = newStates
    batchState = {running: true, current: 0, total: games.length, currentGameTitle: '', results: [], error: ''}

    try {
      await DownloadAllUpdates()
    } catch (e) {
      batchState = {...batchState, running: false, error: String(e)}
    }
  }

  // The backend runs at most one update at a time, so retries are queued and
  // pumped one-by-one as each finishes rather than fired off together.
  let retryQueue = $state([])

  function handleRetryFailed() {
    const failed = (batchState?.results || []).filter(r => !r.success)
    if (failed.length === 0) return
    batchState = null
    retryQueue = failed.map(f => f.gameID)
    pumpRetryQueue()
  }

  function pumpRetryQueue() {
    if (retryQueue.length === 0) return
    const [next, ...rest] = retryQueue
    retryQueue = rest
    handleUpdateGame(next)
  }

  async function handleCancel() {
    try {
      retryQueue = []
      await CancelGameUpdate()
    } catch (e) { /* nothing running */ }
  }


  // ── App Update ───────────────────────────────────────────────
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

  // ── Event Setup ──────────────────────────────────────────────
  function setupEvents() {
    unsubPhase = EventsOn('game-update:phase', (data) => {
      gameStates = {
        ...gameStates,
        [data.gameID]: {...(gameStates[data.gameID] || {}), phase: data.phase},
      }
    })

    unsubDownload = EventsOn('game-update:download-progress', (data) => {
      gameStates = {
        ...gameStates,
        [data.gameID]: {
          ...(gameStates[data.gameID] || {}),
          percent: Math.min(data.percent ?? 0, 100),
          speed: data.speedBytesPerSec ?? 0,
          bytesDownloaded: data.bytesDownloaded ?? 0,
          totalBytes: data.totalBytes ?? 0,
        },
      }
    })

    unsubExtract = EventsOn('game-update:extract-progress', (data) => {
      gameStates = {
        ...gameStates,
        [data.gameID]: {
          ...(gameStates[data.gameID] || {}),
          filesExtracted: data.filesExtracted ?? 0,
          totalFiles: data.totalFiles ?? 0,
          currentFile: data.currentFile ?? '',
        },
      }
    })

    unsubError = EventsOn('game-update:error', (data) => {
      gameStates = {
        ...gameStates,
        [data.gameID]: {
          ...(gameStates[data.gameID] || {}),
          phase: 'error',
          error: data.message || 'Unknown error',
        },
      }
    })

    // The backend releases its single-run lock just before this fires, so
    // it is the only safe point to start the next queued retry.
    unsubIdle = EventsOn('game-update:idle', () => {
      pumpRetryQueue()
    })

    unsubCancelled = EventsOn('game-update:cancelled', () => {
      const busy = ['syncing', 'selecting-link', 'downloading', 'extracting', 'merging', 'updating-db']
      const next = {...gameStates}
      for (const [id, gs] of Object.entries(next)) {
        if (busy.includes(gs.phase)) next[id] = {...gs, phase: 'error', error: 'Cancelled'}
      }
      gameStates = next
      if (batchState?.running) batchState = {...batchState, running: false, error: 'Cancelled'}
    })

    unsubComplete = EventsOn('game-update:complete', (data) => {
      gameStates = {
        ...gameStates,
        [data.gameID]: {
          ...(gameStates[data.gameID] || {}),
          phase: 'done',
          oldVersion: data.oldVersion || '',
          newVersion: data.newVersion || '',
        },
      }
      onUpdateCompleted()
    })

    unsubBatchStart = EventsOn('game-update:batch-start', (data) => {
      batchState = {
        running: true,
        current: 0,
        total: data.total || 0,
        currentGameTitle: '',
        results: [],
        error: '',
      }
    })

    unsubBatchProgress = EventsOn('game-update:batch-progress', (data) => {
      if (batchState) {
        batchState = {...batchState, current: data.current ?? 0, currentGameTitle: data.currentGameTitle || ''}
      }
    })

    unsubGameDone = EventsOn('game-update:game-done', (data) => {
      if (batchState) {
        batchState = {
          ...batchState,
          results: [...batchState.results, {
            gameID: data.gameID,
            title: data.title || '',
            success: !!data.success,
            error: data.error || '',
          }],
        }
      }
    })

    unsubBatchComplete = EventsOn('game-update:batch-complete', (data) => {
      if (batchState) {
        batchState = {
          ...batchState,
          running: false,
          succeeded: data.succeeded ?? 0,
          failed: data.failed ?? 0,
        }
      }
    })
  }

  onMount(async () => {
    // Set up event listeners FIRST (before calling update functions)
    setupEvents()

    // Load version
    try {
      appVersion = await GetVersion()
    } catch (e) { /* ignore */ }

    // Load updatable games
    await loadGames()
  })

  onDestroy(() => {
    if (unsubPhase) unsubPhase()
    if (unsubDownload) unsubDownload()
    if (unsubExtract) unsubExtract()
    if (unsubError) unsubError()
    if (unsubComplete) unsubComplete()
    if (unsubBatchStart) unsubBatchStart()
    if (unsubBatchProgress) unsubBatchProgress()
    if (unsubGameDone) unsubGameDone()
    if (unsubBatchComplete) unsubBatchComplete()
    if (unsubCancelled) unsubCancelled()
    if (unsubIdle) unsubIdle()
  })

  // ── Derived ──────────────────────────────────────────────────
  let isUpdatingAny = $derived(
    Object.values(gameStates).some(s => s && s.phase && s.phase !== 'idle' && s.phase !== 'done' && s.phase !== 'error')
  )

  // The backend holds a single-run lock for the whole pipeline.
  let updateInFlight = $derived(isUpdatingAny || !!batchState?.running)

  let count = $derived(games.length)
  let doneCount = $derived(
    Object.values(gameStates).filter(s => s && s.phase === 'done').length
  )
  let allDone = $derived(count > 0 && doneCount === count)
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
              style="width: {batchState.total > 0
                ? Math.round((batchState.current / batchState.total) * 100)
                : 0}%"
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
                onclick={handleRetryFailed}
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
          onclick={handleUpdateAll}
          disabled={isUpdatingAny || batchState?.running}
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
          disabled={isUpdatingAny || batchState?.running}
        >
          Sync Now
        </button>
        {#if updateInFlight}
          <button class="btn btn-outline btn-cancel" onclick={handleCancel}>
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
                  onclick={() => handleUpdateGame(game.id)}
                  disabled={isUpdatingAny || batchState?.running}
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
                  <button
                    class="btn btn-sm btn-warning"
                    onclick={() => handleRetry(game.id)}
                    disabled={isUpdatingAny || batchState?.running}
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
