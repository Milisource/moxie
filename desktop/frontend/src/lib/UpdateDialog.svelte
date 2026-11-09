<script>
  import {safeExternalUrl} from './sanitizeUrl.js'
  import {formatBytes} from './format.js'

  // Presentational only — the app self-update flow (check → download → apply)
  // and its update:* event subscriptions live in App.svelte so the download
  // keeps its UI across tab switches. This component renders that shared state
  // and calls back into the shell for actions.

  let {
    version = '',
    checking = false,
    info = null,                       // CheckForUpdate result or null
    downloading = false,
    downloadProgress = {downloaded: 0, total: 0},
    downloadComplete = false,
    error = '',
    onCheck = () => {},
    onDownload = () => {},
    onApply = () => {},
  } = $props()

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
      onclick={onCheck}
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
  {#if info && !info.error && !info.hasUpdate}
    <div class="status-section status-success">
      <span class="status-icon">✓</span>
      <div class="status-body">
        <p class="status-title">Moxie is up to date</p>
        <p class="status-detail">You're running the latest version ({version}).</p>
      </div>
    </div>
  {/if}

  <!-- ── Update Available ────────────────────────────────── -->
  {#if info && info.hasUpdate}
    <div class="status-section status-update">
      <span class="status-icon">⟳</span>
      <div class="status-body">
        <p class="status-title">Update Available</p>
        <p class="status-detail">
          <span class="version-diff">
            <span class="version-old">{info.currentVersion}</span>
            <span class="version-arrow">→</span>
            <span class="version-new">{info.latestVersion}</span>
          </span>
        </p>
        {#if safeExternalUrl(info.releaseUrl)}
          <a
            class="release-link"
            href={safeExternalUrl(info.releaseUrl)}
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
          onclick={onDownload}
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
        onclick={onApply}
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
    padding: var(--space-8) var(--gutter);
    width: 100%;
    max-width: 1280px;
  }

  .update-header {
    margin-bottom: 24px;
  }
  .update-header h2 {
    font-size: var(--text-2xl);
    font-weight: 700;
    margin: 0 0 4px;
  }
  .update-subtitle {
    font-size: var(--text-base);
    color: var(--text-secondary);
    margin: 0;
  }
  .version-tag {
    font-family: var(--font-mono);
    font-size: var(--text-base);
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    padding: 1px 6px;
    border-radius: var(--radius-1);
  }

  /* ── Action Bar ────────────────────── */
  .action-bar {
    margin-bottom: 16px;
  }

  /* ── Shared Status Sections ────────── */
  .status-section {
    margin: 16px 0;
    padding: 16px;
    border-radius: var(--radius-1);
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .status-icon {
    font-size: var(--text-xl);
    font-weight: 700;
    flex-shrink: 0;
    line-height: 1.4;
  }
  .status-body {
    flex: 1;
    min-width: 0;
  }
  .status-title {
    font-size: var(--text-lg);
    font-weight: 600;
    margin: 0 0 4px;
  }
  .status-detail {
    font-size: var(--text-base);
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
    font-size: var(--text-base);
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Version Diff ──────────────────── */
  .version-diff {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--font-mono);
    font-size: var(--text-md);
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
    font-size: var(--text-base);
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
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
  }
  .progress-bar-bg {
    height: 6px;
    border-radius: var(--radius-1);
    background: var(--bg-tertiary);
    overflow: hidden;
    margin-bottom: 8px;
  }
  .progress-bar-fill {
    height: 100%;
    border-radius: var(--radius-1);
    background: var(--accent);
    transition: width 0.3s ease;
  }
  .progress-label {
    font-size: var(--text-sm);
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Buttons ────────────────────────── */
  .btn {
    padding: 7px 16px;
    border: none;
    border-radius: var(--radius-1);
    font-size: var(--text-base);
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
    color: var(--on-accent);
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover, var(--accent)); }
</style>