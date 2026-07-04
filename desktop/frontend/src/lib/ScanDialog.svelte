<script>
  import {onMount, onDestroy} from 'svelte'
  import {EventsOn} from '../../wailsjs/runtime/runtime'
  import {
    GetScanPaths,
    AddScanPath,
    RemoveScanPath,
    ScanDirectory,
    GetGames,
  } from '../../wailsjs/go/main/App'

  let {
    onGamesUpdated = () => {},
  } = $props()

  // ── State ──────────────────────────────────────────────────
  let scanPaths = $state([])
  let newPath = $state('')
  let scanning = $state(false)
  let currentPath = $state('')

  // Progress
  let progress = $state({ dirsExamined: 0, gamesFound: 0, phase: '' })
  let showProgress = $state(false)

  // Results
  let lastResult = $state(null)   // { gamesFound, games, errors } or null
  let scanError = $state('')

  // Unsubscribe fns
  let unsubProgress = null
  let unsubComplete = null
  let unsubError = null

  async function loadPaths() {
    try {
      scanPaths = await GetScanPaths()
    } catch (e) {
      console.error('Failed to load scan paths:', e)
    }
  }

  async function handleAdd() {
    const path = newPath.trim()
    if (!path) return
    try {
      await AddScanPath(path)
      scanPaths = await GetScanPaths()
      newPath = ''
    } catch (e) {
      console.error('Failed to add path:', e)
    }
  }

  async function handleRemove(path) {
    try {
      await RemoveScanPath(path)
      scanPaths = await GetScanPaths()
    } catch (e) {
      console.error('Failed to remove path:', e)
    }
  }

  async function handleScan(path) {
    scanning = true
    currentPath = path
    showProgress = true
    lastResult = null
    scanError = ''
    progress = { dirsExamined: 0, gamesFound: 0, phase: '' }

    try {
      await ScanDirectory(path)
    } catch (e) {
      scanError = String(e)
      scanning = false
    }
  }

  function reloadAfterScan() {
    // Refresh game list after scan completes
    onGamesUpdated()
    scanning = false
  }

  onMount(() => {
    loadPaths()

    // Listen for Wails events from the Go backend
    unsubProgress = EventsOn('scan:progress', (data) => {
      progress = data
    })

    unsubComplete = EventsOn('scan:complete', (data) => {
      lastResult = data
      reloadAfterScan()
    })

    unsubError = EventsOn('scan:error', (data) => {
      scanError = data.error || 'Unknown error'
      scanning = false
    })
  })

  onDestroy(() => {
    if (unsubProgress) unsubProgress()
    if (unsubComplete) unsubComplete()
    if (unsubError) unsubError()
  })

  // ── Derived ─────────────────────────────────────────────────
  let progressPct = $derived.by(() => {
    if (!progress.dirsExamined) return 0
    const estimate = progress.phase === 'walk'
      ? Math.min(progress.dirsExamined / 200, 0.95) * 100
      : 100
    return Math.round(Math.min(estimate, 99))
  })

  let phaseLabel = $derived.by(() => {
    if (!progress.phase) return 'Starting…'
    return progress.phase === 'walk'
      ? `Scanning directories… (${progress.dirsExamined} examined, ${progress.gamesFound} found)`
      : `Detecting engines… (${progress.gamesFound} processed)`
  })
</script>

<div class="scan-dialog">
  <div class="scan-header">
    <h2>Scan Directories</h2>
    <p class="scan-subtitle">Add game folders to scan for new titles.</p>
  </div>

  <!-- Add path -->
  <div class="add-path">
    <input
      type="text"
      class="path-input"
      placeholder="/path/to/games"
      bind:value={newPath}
      onkeydown={(e) => e.key === 'Enter' && handleAdd()}
    />
    <button class="btn btn-primary" onclick={handleAdd} disabled={!newPath.trim()}>
      Add Path
    </button>
  </div>

  <!-- Saved paths -->
  {#if scanPaths.length > 0}
    <div class="paths-list">
      {#each scanPaths as path}
        <div class="path-row">
          <span class="path-label" title={path}>{path}</span>
          <div class="path-actions">
            <button
              class="btn btn-scan"
              onclick={() => handleScan(path)}
              disabled={scanning}
            >
              {scanning && currentPath === path ? 'Scanning…' : 'Scan'}
            </button>
            <button
              class="btn btn-remove"
              onclick={() => handleRemove(path)}
              disabled={scanning}
            >
              ✕
            </button>
          </div>
        </div>
      {/each}
    </div>
  {:else}
    <div class="empty-paths">
      <p>No scan paths configured. Add a directory above to get started.</p>
    </div>
  {/if}

  <!-- ── Scan Progress ──────────────────────────────────── -->
  {#if showProgress && scanning}
    <div class="progress-section">
      <div class="progress-bar-bg">
        <div class="progress-bar-fill" style="width: {progressPct}%"></div>
      </div>
      <p class="progress-label">{phaseLabel}</p>
    </div>
  {/if}

  <!-- ── Scan Result ────────────────────────────────────── -->
  {#if lastResult}
    <div class="result-section">
      <h3 class="result-title">
        Found {lastResult.gamesFound} game{lastResult.gamesFound !== 1 ? 's' : ''}
      </h3>
      {#if lastResult.errors?.length > 0}
        <div class="result-errors">
          <p class="error-title">{lastResult.errors.length} error{lastResult.errors.length !== 1 ? 's' : ''}:</p>
          {#each lastResult.errors as err}
            <p class="error-line">{err}</p>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <!-- ── Scan Error ─────────────────────────────────────── -->
  {#if scanError}
    <div class="error-section">
      <p class="error-title">Scan failed:</p>
      <p class="error-line">{scanError}</p>
    </div>
  {/if}
</div>

<style>
  .scan-dialog {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 720px;
    margin: 0 auto;
    width: 100%;
  }

  .scan-header {
    margin-bottom: 24px;
  }
  .scan-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .scan-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Add Path ──────────────────────── */
  .add-path {
    display: flex;
    gap: 8px;
    margin-bottom: 20px;
  }
  .path-input {
    flex: 1;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
  }
  .path-input:focus { border-color: var(--accent); }

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
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }

  .btn-scan {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--accent);
    padding: 4px 12px;
    font-size: 12px;
  }
  .btn-scan:hover:not(:disabled) { background: var(--accent); color: #fff; }

  .btn-remove {
    background: transparent;
    color: var(--text-muted);
    padding: 4px 8px;
    font-size: 12px;
  }
  .btn-remove:hover:not(:disabled) { color: var(--danger); background: color-mix(in srgb, var(--danger) 10%, transparent); }

  /* ── Paths List ────────────────────── */
  .paths-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 20px;
  }
  .path-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-secondary);
    gap: 8px;
  }
  .path-label {
    flex: 1;
    font-size: 13px;
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }
  .path-actions {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  .empty-paths {
    text-align: center;
    padding: 40px 0;
    color: var(--text-muted);
    font-size: 13px;
  }

  /* ── Progress ──────────────────────── */
  .progress-section {
    margin: 20px 0;
    padding: 16px;
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

  /* ── Result ──────────────────────────── */
  .result-section {
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--success);
    border-radius: 8px;
    background: color-mix(in srgb, var(--success) 8%, transparent);
  }
  .result-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--success);
    margin: 0 0 8px;
  }
  .result-errors { margin-top: 8px; }

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
    margin: 0 0 2px;
    font-family: var(--font-mono);
  }
</style>
