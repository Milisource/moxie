<script>
  import {onMount, onDestroy} from 'svelte'
  import {EventsOn} from '../../wailsjs/runtime/runtime'
  import {ScanDirectory} from '../../wailsjs/go/main/App'
  import ScanPaths from './ScanPaths.svelte'

  let {
    onGamesUpdated = () => {},
  } = $props()

  // ── State ──────────────────────────────────────────────────
  let scanning = $state(false)
  let currentPath = $state('')

  // Progress
  let progress = $state({ dirsExamined: 0, gamesFound: 0, phase: '' })
  let showProgress = $state(false)

  // Results
  let lastResult = $state(null)   // { gamesFound, inserted, updated, errors } or null
  let scanError = $state('')

  // Unsubscribe fns
  let unsubProgress = null
  let unsubComplete = null
  let unsubError = null

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

  <ScanPaths onScan={handleScan} {scanning} {currentPath} />

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
        <span class="result-breakdown">
          ({lastResult.inserted} new, {lastResult.updated} updated)
        </span>
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
  .result-breakdown {
    font-size: 13px;
    font-weight: 400;
    color: var(--text-secondary);
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
