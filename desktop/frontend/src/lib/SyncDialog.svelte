<script>
  // Presentational only — sync state and event subscriptions live in
  // App.svelte so they survive tab switches. This view just renders them.

  let {
    cookieStatus = '',        // 'available' | 'not_found' | ''
    syncing = false,
    progress = {current: 0, total: 0, title: '', phase: ''},
    gameResults = [],         // {id, title, status, version}
    result = null,            // {associated, updated, skipped, errors} or null
    syncError = '',
    onSync = () => {},
  } = $props()

  // Full sync: bypasses the 24h cooldown; every associated game's thread is
  // scraped, which also refreshes download links for the Downloads tab.
  let force = $state(false)

  // ── Derived ─────────────────────────────────────────────────
  let progressPct = $derived.by(() => {
    if (!progress.total) return 0
    return Math.round((progress.current / progress.total) * 100)
  })

  let phaseLabel = $derived.by(() => {
    if (!progress.phase) return 'Starting…'
    if (progress.phase === 'associating') {
      return `Associating games… (${progress.current}/${progress.total})`
    }
    if (progress.phase === 'checking-updates') {
      return `Checking for updates… (${progress.current}/${progress.total})`
    }
    return 'Synchronizing…'
  })

  // Sync is cookie-free via F95Zone's public endpoints — cookies only add
  // the direct-scrape fallback layer. The button is never gated on them.
  let canSync = $derived(!syncing)
</script>

<div class="sync-dialog">
  <div class="sync-header">
    <h2>F95Zone Sync</h2>
    <p class="sync-subtitle">
      Auto-associate games with their F95Zone entries and check for updates.
    </p>
  </div>

  <!-- ── Cookie Status ─────────────────────────────────────── -->
  {#if cookieStatus === 'available'}
    <div class="cookie-status cookie-ok">
      <span class="cookie-icon">✓</span>
      <div class="cookie-body">
        <p class="cookie-title">F95Zone cookies available</p>
        <p class="cookie-detail">Sync can connect to F95Zone to look up games.</p>
      </div>
    </div>
  {:else if cookieStatus === 'not_found'}
    <div class="cookie-status cookie-missing">
      <span class="cookie-icon">⚠</span>
      <div class="cookie-body">
        <p class="cookie-title">No F95Zone cookies found</p>
        <p class="cookie-detail">
          Sync runs cookie-free through F95Zone's public endpoints, so it works
          without a browser session. Logging in at <strong>f95zone.to</strong>
          and restarting adds the cookie fallback for games the public
          endpoints can't resolve.
        </p>
      </div>
    </div>
  {:else}
    <div class="cookie-status cookie-loading">
      <div class="spinner"></div>
      <p class="status-text">Checking cookie status…</p>
    </div>
  {/if}

  <!-- ── Start Sync Button ─────────────────────────────────── -->
  <div class="action-bar">
    <label class="sync-force" title="Bypasses the 24h cooldown and scrapes every game thread — also refreshes download links for the Downloads tab.">
      <input type="checkbox" bind:checked={force} disabled={!canSync} />
      Full sync (ignore 24h cooldown)
    </label>
    <button
      class="btn btn-primary"
      onclick={() => onSync(force)}
      disabled={!canSync}
    >
      {#if syncing}
        Syncing…
      {:else}
        Start Sync
      {/if}
    </button>
  </div>

  <!-- ── Sync Progress ─────────────────────────────────────── -->
  {#if syncing}
    <div class="progress-section">
      <div class="progress-bar-bg">
        <div class="progress-bar-fill" style="width: {progressPct}%"></div>
      </div>
      <p class="progress-label">{phaseLabel}</p>
    </div>

    <!-- Per-game progress list -->
    {#if gameResults.length > 0}
      <div class="games-progress">
        {#each gameResults as game}
          <div class="game-row">
            {#if game.status === 'error'}
              <span class="game-icon game-icon-error">✗</span>
            {:else}
              <span class="game-icon game-icon-done">✓</span>
            {/if}
            <span class="game-title">{game.title}</span>
            <span class="game-status">
              {#if game.version}
                {game.status} — {game.version}
              {:else}
                {game.status}
              {/if}
            </span>
          </div>
        {/each}
      </div>
    {/if}
  {/if}

  <!-- ── Completion Result ─────────────────────────────────── -->
  {#if result}
    <div class="result-section">
      <div class="result-header">
        <span class="result-icon">✓</span>
        <div class="result-body">
          <p class="result-title">Sync Complete</p>
          <p class="result-summary">
            {result.associated} associated, {result.updated} updated
            {#if result.skipped > 0}
              , {result.skipped} skipped (checked recently)
            {/if}
            {#if result.noMatch > 0}
              , {result.noMatch} no match found
            {/if}
            {#if result.errors?.length > 0}
              , {result.errors.length} error{result.errors.length !== 1 ? 's' : ''}
            {/if}
          </p>
        </div>
      </div>
      {#if result.errors?.length > 0}
        <div class="result-errors">
          <p class="error-title">Errors:</p>
          {#each result.errors as err}
            <p class="error-line">{err}</p>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <!-- ── Sync Error ────────────────────────────────────────── -->
  {#if syncError}
    <div class="error-section">
      <p class="error-title">Sync failed:</p>
      <p class="error-line">{syncError}</p>
    </div>
  {/if}
</div>

<style>
  .sync-dialog {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 720px;
    margin: 0 auto;
    width: 100%;
  }

  .sync-header {
    margin-bottom: 24px;
  }
  .sync-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .sync-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }

  /* ── Action Bar ────────────────────── */
  .action-bar {
    margin-bottom: 16px;
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .sync-force {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--text-secondary);
    cursor: pointer;
    user-select: none;
  }
  .sync-force input {
    accent-color: var(--accent, #7c5cff);
  }

  /* ── Cookie Status ─────────────────── */
  .cookie-status {
    margin: 16px 0;
    padding: 16px;
    border-radius: 8px;
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .cookie-icon {
    font-size: 18px;
    font-weight: 700;
    flex-shrink: 0;
    line-height: 1.4;
  }
  .cookie-body {
    flex: 1;
    min-width: 0;
  }
  .cookie-title {
    font-size: 15px;
    font-weight: 600;
    margin: 0 0 4px;
  }
  .cookie-detail {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
    line-height: 1.5;
  }
  .cookie-detail strong {
    color: var(--text-primary);
  }

  .cookie-ok {
    border: 1px solid var(--success);
    background: color-mix(in srgb, var(--success) 10%, transparent);
  }
  .cookie-ok .cookie-icon,
  .cookie-ok .cookie-title {
    color: var(--success);
  }

  .cookie-missing {
    border: 1px solid var(--warning);
    background: color-mix(in srgb, var(--warning) 10%, transparent);
  }
  .cookie-missing .cookie-icon,
  .cookie-missing .cookie-title {
    color: var(--warning);
  }

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

  /* ── Game Progress List ────────────── */
  .games-progress {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 12px 0 0;
    max-height: 320px;
    overflow-y: auto;
    padding-right: 4px;
  }
  .game-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
  }
  .game-icon {
    font-size: 13px;
    font-weight: 700;
    flex-shrink: 0;
    width: 18px;
    text-align: center;
  }
  .game-icon-done {
    color: var(--success);
  }
  .game-icon-error {
    color: var(--danger);
  }
  .game-title {
    flex: 1;
    font-size: 13px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .game-status {
    font-size: 11px;
    color: var(--text-muted);
    flex-shrink: 0;
    font-family: var(--font-mono);
  }

  /* ── Result ──────────────────────────── */
  .result-section {
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--success);
    border-radius: 8px;
    background: color-mix(in srgb, var(--success) 8%, transparent);
  }
  .result-header {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .result-icon {
    font-size: 18px;
    font-weight: 700;
    color: var(--success);
    flex-shrink: 0;
    line-height: 1.4;
  }
  .result-body {
    flex: 1;
    min-width: 0;
  }
  .result-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--success);
    margin: 0 0 4px;
  }
  .result-summary {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0;
  }
  .result-errors {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }

  /* ── Errors ───────────────────────────── */
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
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
</style>
