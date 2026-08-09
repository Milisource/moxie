<script>
  import {onMount, onDestroy} from 'svelte'
  import {EventsOn} from '../../wailsjs/runtime/runtime'
  import {
    GetVersion,
    CheckForUpdate,
    DownloadUpdate,
    ApplyUpdate,
  } from '../../wailsjs/go/main/App'
  import {safeExternalUrl} from './sanitizeUrl.js'

  // ── State ──────────────────────────────────────────────────
  let version = $state('')
  let updateInfo = $state(null)   // CheckForUpdate result or null
  let checking = $state(false)    // loading during check
  let downloading = $state(false) // loading during download
  let downloadProgress = $state({ downloaded: 0, total: 0 })
  let downloadComplete = $state(false) // download finished, show Apply
  let error = $state('')

  // Unsubscribe fns
  let unsubProgress = null
  let unsubComplete = null
  let unsubError = null

  async function handleCheck() {
    checking = true
    error = ''
    updateInfo = null
    downloadComplete = false
    downloadProgress = { downloaded: 0, total: 0 }

    try {
      const result = await CheckForUpdate()
      // CheckForUpdate never throws on API failure — the failure message is
      // carried in the `error` field. Treat it as an error, not "up to date".
      if (result?.error) {
        error = result.error
        updateInfo = null
      } else {
        updateInfo = result
      }
    } catch (e) {
      error = String(e)
    } finally {
      checking = false
    }
  }

  async function handleDownload() {
    downloading = true
    error = ''
    downloadComplete = false
    downloadProgress = { downloaded: 0, total: 0 }

    try {
      await DownloadUpdate()
    } catch (e) {
      error = String(e)
      downloading = false
    }
  }

  async function handleApply() {
    try {
      await ApplyUpdate()
    } catch (e) {
      error = String(e)
    }
  }

  onMount(async () => {
    // Load current version on mount
    try {
      version = await GetVersion()
    } catch (e) {
      console.error('Failed to get version:', e)
    }

    // Listen for Wails events from the Go backend
    unsubProgress = EventsOn('update:progress', (data) => {
      downloadProgress = data
    })

    unsubComplete = EventsOn('update:complete', () => {
      downloading = false
      downloadComplete = true
    })

    unsubError = EventsOn('update:error', (data) => {
      error = data.error || 'Update failed'
      downloading = false
    })
  })

  onDestroy(() => {
    if (unsubProgress) unsubProgress()
    if (unsubComplete) unsubComplete()
    if (unsubError) unsubError()
  })

  // ── Derived ─────────────────────────────────────────────────
  let downloadPct = $derived.by(() => {
    if (!downloadProgress.total) return 0
    return Math.round((downloadProgress.downloaded / downloadProgress.total) * 100)
  })

  let downloadLabel = $derived.by(() => {
    if (!downloadProgress.total) return 'Preparing download…'
    const downloaded = formatBytes(downloadProgress.downloaded)
    const total = formatBytes(downloadProgress.total)
    return `Downloaded ${downloaded} of ${total}`
  })

  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    const val = (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)
    return `${val} ${units[i]}`
  }
</script>

<div class="update-dialog">
  <div class="update-header">
    <h2>Update Checker</h2>
    <p class="update-subtitle">
      Current version: <code class="version-tag">{version || '…'}</code>
    </p>
  </div>

  <!-- ── Check Button ───────────────────────────────────── -->
  <div class="action-bar">
    <button
      class="btn btn-primary"
      onclick={handleCheck}
      disabled={checking || downloading}
    >
      {#if checking}
        Checking…
      {:else}
        Check for Updates
      {/if}
    </button>
  </div>

  <!-- ── Loading State ───────────────────────────────────── -->
  {#if checking}
    <div class="status-section status-loading">
      <div class="spinner"></div>
      <p class="status-text">Checking for updates…</p>
    </div>
  {/if}

  <!-- ── Up-to-Date ──────────────────────────────────────── -->
  {#if updateInfo && !updateInfo.error && !updateInfo.hasUpdate}
    <div class="status-section status-success">
      <span class="status-icon">✓</span>
      <div class="status-body">
        <p class="status-title">Moxie is up to date</p>
        <p class="status-detail">You're running the latest version ({version}).</p>
      </div>
    </div>
  {/if}

  <!-- ── Update Available ────────────────────────────────── -->
  {#if updateInfo && updateInfo.hasUpdate}
    <div class="status-section status-update">
      <span class="status-icon">⟳</span>
      <div class="status-body">
        <p class="status-title">Update Available</p>
        <p class="status-detail">
          <span class="version-diff">
            <span class="version-old">{updateInfo.currentVersion}</span>
            <span class="version-arrow">→</span>
            <span class="version-new">{updateInfo.latestVersion}</span>
          </span>
        </p>
        {#if safeExternalUrl(updateInfo.releaseUrl)}
          <a
            class="release-link"
            href={safeExternalUrl(updateInfo.releaseUrl)}
            target="_blank"
            rel="noopener noreferrer"
          >
            View Release →
          </a>
        {/if}
      </div>
    </div>

    <!-- Download button (only if not already downloading/complete) -->
    {#if !downloading && !downloadComplete}
      <div class="action-bar">
        <button
          class="btn btn-primary"
          onclick={handleDownload}
          disabled={checking || downloading}
        >
          Download Update
        </button>
      </div>
    {/if}
  {/if}

  <!-- ── Download Progress ───────────────────────────────── -->
  {#if downloading}
    <div class="progress-section">
      <div class="progress-bar-bg">
        <div class="progress-bar-fill" style="width: {downloadPct}%"></div>
      </div>
      <p class="progress-label">{downloadLabel}</p>
    </div>
  {/if}

  <!-- ── Download Complete (Apply) ───────────────────────── -->
  {#if downloadComplete}
    <div class="status-section status-success">
      <span class="status-icon">✓</span>
      <div class="status-body">
        <p class="status-title">Download Complete</p>
        <p class="status-detail">Restart to apply the update.</p>
      </div>
    </div>

    <div class="action-bar">
      <button
        class="btn btn-primary"
        onclick={handleApply}
      >
        Restart &amp; Apply
      </button>
    </div>
  {/if}

  <!-- ── Error ───────────────────────────────────────────── -->
  {#if error}
    <div class="status-section status-error">
      <span class="status-icon">✕</span>
      <div class="status-body">
        <p class="status-title">Error</p>
        <p class="status-detail">{error}</p>
      </div>
    </div>
  {/if}
</div>

<style>
  .update-dialog {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 720px;
    margin: 0 auto;
    width: 100%;
  }

  .update-header {
    margin-bottom: 24px;
  }
  .update-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .update-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }
  .version-tag {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    padding: 1px 6px;
    border-radius: 4px;
  }

  /* ── Action Bar ────────────────────── */
  .action-bar {
    margin-bottom: 16px;
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
  }

  .status-success {
    border: 1px solid var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .status-success .status-icon,
  .status-success .status-title {
    color: var(--success);
  }
  .status-success .status-detail {
    color: var(--text-secondary);
  }

  .status-update {
    border: 1px solid var(--warning);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
  }
  .status-update .status-icon,
  .status-update .status-title {
    color: var(--warning);
  }
  .status-update .status-detail {
    color: var(--text-secondary);
  }

  .status-error {
    border: 1px solid var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }
  .status-error .status-icon,
  .status-error .status-title {
    color: var(--danger);
  }
  .status-error .status-detail {
    color: var(--text-secondary);
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

  /* ── Version Diff ──────────────────── */
  .version-diff {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--font-mono);
    font-size: 14px;
  }
  .version-old {
    color: var(--text-muted);
    text-decoration: line-through;
  }
  .version-arrow {
    color: var(--text-secondary);
  }
  .version-new {
    color: var(--success);
    font-weight: 600;
  }

  .release-link {
    display: inline-block;
    margin-top: 8px;
    font-size: 13px;
    color: var(--accent);
    text-decoration: none;
  }
  .release-link:hover {
    text-decoration: underline;
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

  /* ── Progress ──────────────────────── */
  .progress-section {
    margin: 16px 0;
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
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover, var(--accent)); }
</style>
