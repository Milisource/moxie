<script>
  // Presentational only — sync state and event subscriptions live in
  // App.svelte so they survive tab switches. This view just renders them.
  import {onMount} from 'svelte'
  import Button from './Button.svelte'
  import Spinner from './Spinner.svelte'
  import {GetUpdatableCount} from '../../wailsjs/go/main/App'

  let {
    cookieStatus = '',        // 'available' | 'not_found' | ''
    syncing = false,
    progress = {current: 0, total: 0, title: '', phase: ''},
    gameResults = [],         // {id, title, status, version}
    result = null,            // {associated, updated, skipped, errors} or null
    syncError = '',
    gameCount = 0,
    lastSyncAt = '',
    onSync = () => {},
    onCancel = () => {},
  } = $props()

  // App.svelte caps the retained per-game list (last 50) so appends are
  // O(1); slice here too as a defensive render bound.
  let visibleGameResults = $derived(gameResults.slice(-50))

  // Full sync: bypasses the 24h cooldown; every associated game's thread is
  // scraped, which also refreshes download links for the Downloads tab.
  let force = $state(false)

  let updateCount = $state(null)
  onMount(async () => {
    try { updateCount = await GetUpdatableCount() } catch (e) { /* keep unknown */ }
  })

  function relativeSync(iso) {
    if (!iso) return null
    const mins = Math.floor((Date.now() - new Date(iso).getTime()) / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    return `${Math.floor(hours / 24)}d ago`
  }

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

  let canSync = $derived(!syncing)
  let cookieValue = $derived(
    cookieStatus === 'available' ? 'OK' : cookieStatus === 'not_found' ? 'Missing' : 'Checking…'
  )
  let cookieTone = $derived(
    cookieStatus === 'available' ? 'good' : cookieStatus === 'not_found' ? 'warn' : 'muted'
  )
  let syncLabel = $derived(relativeSync(lastSyncAt))
  let updatesTone = $derived(updateCount === null ? 'muted' : updateCount > 0 ? 'warn' : 'good')
</script>

<div class="sync-dialog">
  <div class="sync-header">
    <div class="sync-heading">
      <h2>F95Zone sync</h2>
      <p class="sync-subtitle">Auto-associate games with their F95Zone entries and check for updates.</p>
    </div>
    <div class="header-actions">
      <label class="sync-force" title="Bypasses the 24h cooldown and scrapes every game thread — also refreshes download links for the Downloads tab.">
        <input type="checkbox" bind:checked={force} disabled={!canSync} />
        Full sync
      </label>
      <Button variant="primary" onclick={() => onSync(force)} disabled={!canSync}>
        {syncing ? 'Syncing…' : 'Start sync'}
      </Button>
      {#if syncing}
        <Button variant="danger" onclick={onCancel} title="Stop the running sync">Cancel</Button>
      {/if}
    </div>
  </div>

  <!-- Status -->
  <div class="status-strip">
    <div class="stat">
      <span class="stat-label">Cookies</span>
      <span class="stat-value {cookieTone}">{cookieValue}</span>
      <span class="stat-sub">{cookieStatus === 'not_found' ? 'Cookie-free fallback is active' : 'F95Zone session'}</span>
    </div>
    <div class="stat">
      <span class="stat-label">Last sync</span>
      <span class="stat-value">{syncLabel || 'Never'}</span>
      <span class="stat-sub">{result ? 'Completed this session' : 'From this install'}</span>
    </div>
    <div class="stat">
      <span class="stat-label">Updates</span>
      <span class="stat-value {updatesTone}">{updateCount === null ? '—' : updateCount}</span>
      <span class="stat-sub">{updateCount === null ? 'Checking…' : 'Ready to download'}</span>
    </div>
  </div>

  <!-- Plan -->
  <div class="plan-card">
    <div class="plan-head">
      <h3>What will run</h3>
      <span class="plan-sub">Cookie-free first; browser fallback only when needed.</span>
    </div>
    <div class="plan-row">
      <span class="plan-icon good">✓</span>
      <span class="plan-text">{gameCount} game{gameCount === 1 ? '' : 's'} checked for new versions</span>
      <span class="plan-count">{gameCount}</span>
    </div>
    <div class="plan-row">
      <span class="plan-icon good">✓</span>
      <span class="plan-text">{updateCount === null ? 'Updates checked' : `${updateCount} update${updateCount === 1 ? '' : 's'} ready to download`}</span>
      <span class="plan-count">{updateCount === null ? '—' : updateCount}</span>
    </div>
    <div class="plan-row">
      <span class="plan-icon good">✓</span>
      <span class="plan-text">Unlinked games matched to F95Zone threads</span>
      <span class="plan-count">—</span>
    </div>
    {#if cookieStatus === 'not_found'}
      <p class="cookie-note">
        No F95Zone cookies found. Sync still works through the public endpoints;
        logging in at f95zone.to and restarting adds the fallback.
      </p>
    {:else if cookieStatus === ''}
      <p class="cookie-note"><Spinner size={12} duration={0.6} /> Checking cookie status…</p>
    {/if}
  </div>

  <div class="info-grid">
    <section class="info-card">
      <h3>How sync stays polite</h3>
      <p><span class="info-icon">✓</span>One request at a time</p>
      <p><span class="info-icon">✓</span>24h cooldown between full syncs</p>
      <p><span class="info-icon">✓</span>Browser only for security checks</p>
    </section>
    <section class="info-card">
      <h3>What sync changes</h3>
      <p><span class="info-icon">✓</span>F95Zone associations and versions</p>
      <p><span class="info-icon">✓</span>Download links for new releases</p>
      <p><span class="info-icon muted">—</span>Never launches games or moves installs</p>
    </section>
  </div>

  <!-- Progress -->
  {#if syncing}
    <div class="progress-section">
      <div class="progress-bar-bg" role="progressbar" aria-valuemin="0" aria-valuemax="100">
        <div class="progress-bar-fill" style="width: {progressPct}%"></div>
      </div>
      <p class="progress-label">{phaseLabel}</p>
    </div>

    {#if visibleGameResults.length > 0}
      <div class="games-progress">
        {#each visibleGameResults as game}
          <div class="game-row">
            {#if game.status === 'error'}
              <span class="game-icon game-icon-error">✗</span>
            {:else}
              <span class="game-icon game-icon-done">✓</span>
            {/if}
            <span class="game-title">{game.title}</span>
            <span class="game-status">
              {#if game.version}{game.status} — {game.version}{:else}{game.status}{/if}
            </span>
          </div>
        {/each}
      </div>
    {/if}
  {/if}

  <!-- Result -->
  {#if result}
    <div class="result-section">
      <div class="result-header">
        <span class="result-icon">✓</span>
        <div class="result-body">
          <p class="result-title">Sync complete</p>
          <p class="result-summary">
            {result.associated} associated, {result.updated} updated
            {#if result.skipped > 0}, {result.skipped} skipped (checked recently){/if}
            {#if result.noMatch > 0}, {result.noMatch} no match found{/if}
            {#if result.errors?.length > 0}, {result.errors.length} error{result.errors.length !== 1 ? 's' : ''}{/if}
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
    padding: var(--space-7) var(--gutter);
    width: 100%;
  }

  .sync-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 24px;
    margin-bottom: 18px;
  }
  .sync-heading { min-width: 0; }
  .sync-header h2 { font-size: var(--text-3xl); font-weight: 600; line-height: 1.15; letter-spacing: -0.01em; margin: 0 0 4px; }
  .sync-subtitle { font-size: var(--text-sm); color: var(--text-secondary); margin: 0; }
  .header-actions { display: flex; align-items: center; gap: 10px; flex-shrink: 0; }

  .sync-force {
    display: flex; align-items: center; gap: 8px;
    font-size: var(--text-sm); color: var(--text-secondary);
    cursor: pointer; user-select: none;
  }
  .sync-force input { accent-color: var(--accent); }

  .status-strip {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 14px 16px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
  }
  .stat-label { font-size: var(--text-xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: var(--tracking-label); }
  .stat-value { font-family: var(--font-mono); font-size: var(--text-2xl); font-weight: 600; color: var(--text-primary); line-height: 1.2; }
  .stat-value.good { color: var(--success); }
  .stat-value.warn { color: var(--warning); }
  .stat-value.muted { color: var(--text-muted); }
  .stat-sub { font-size: var(--text-xs); color: var(--text-muted); }

  .plan-card {
    padding: 16px 18px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    margin-bottom: 16px;
  }
  .plan-head { display: flex; align-items: baseline; justify-content: space-between; gap: 16px; margin-bottom: 10px; }
  .plan-head h3 { margin: 0; font-size: var(--text-lg); font-weight: 600; }
  .plan-sub { font-size: var(--text-xs); color: var(--text-muted); }
  .plan-row {
    display: grid;
    grid-template-columns: 18px minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    padding: 9px 0;
    border-top: 1px solid var(--border);
    font-size: var(--text-sm);
    color: var(--text-secondary);
  }
  .plan-icon { font-size: var(--text-xs); text-align: center; }
  .plan-icon.good { color: var(--success); }
  .plan-count { font-family: var(--font-mono); font-size: var(--text-xs); color: var(--text-muted); }
  .cookie-note { display: flex; align-items: center; gap: 8px; margin: 10px 0 0; padding-top: 10px; border-top: 1px solid var(--border); font-size: var(--text-xs); color: var(--text-muted); }

  .info-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-bottom: 16px; }
  .info-card { padding: 16px 18px; border: 1px solid var(--border); background: var(--bg-secondary); }
  .info-card h3 { margin: 0 0 10px; font-size: var(--text-base); font-weight: 600; }
  .info-card p { display: flex; gap: 8px; align-items: baseline; margin: 0 0 6px; font-size: var(--text-xs); color: var(--text-muted); }
  .info-card p:last-child { margin-bottom: 0; }
  .info-icon { color: var(--success); font-weight: 600; }
  .info-icon.muted { color: var(--text-muted); }

  .progress-section { padding: 14px 16px; border: 1px solid var(--border); background: var(--bg-secondary); margin-bottom: 12px; }
  .progress-bar-bg { height: 6px; background: var(--bg-tertiary); overflow: hidden; margin-bottom: 8px; }
  .progress-bar-fill { height: 100%; background: var(--accent); transition: width 0.3s ease; }
  .progress-label { margin: 0; font-size: var(--text-sm); color: var(--text-secondary); }

  .games-progress { display: flex; flex-direction: column; gap: 4px; max-height: 320px; overflow-y: auto; margin-bottom: 12px; }
  .game-row { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border: 1px solid var(--border); background: var(--bg-primary); }
  .game-icon { width: 18px; text-align: center; font-weight: 700; flex-shrink: 0; }
  .game-icon-done { color: var(--success); }
  .game-icon-error { color: var(--danger); }
  .game-title { flex: 1; font-size: var(--text-sm); color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .game-status { font-size: var(--text-xs); color: var(--text-muted); font-family: var(--font-mono); flex-shrink: 0; }

  .result-section { padding: 14px 16px; border: 1px solid var(--success); background: color-mix(in srgb, var(--success) 8%, transparent); margin-bottom: 12px; }
  .result-header { display: flex; gap: 12px; align-items: flex-start; }
  .result-icon { color: var(--success); font-weight: 700; font-size: var(--text-lg); line-height: 1.3; }
  .result-body { flex: 1; min-width: 0; }
  .result-title { margin: 0 0 4px; font-size: var(--text-base); font-weight: 600; color: var(--success); }
  .result-summary { margin: 0; font-size: var(--text-sm); color: var(--text-secondary); }
  .result-errors { margin-top: 10px; padding-top: 10px; border-top: 1px solid var(--border); }

  .error-section { padding: 14px 16px; border: 1px solid var(--danger); background: color-mix(in srgb, var(--danger) 8%, transparent); }
  .error-title { margin: 0 0 4px; font-size: var(--text-sm); font-weight: 600; color: var(--danger); }
  .error-line { margin: 0 0 2px; font-size: var(--text-xs); color: var(--text-secondary); font-family: var(--font-mono); }

  @media (max-width: 1000px) {
    .sync-header { flex-direction: column; }
    .status-strip { grid-template-columns: minmax(0, 1fr); }
    .plan-head { flex-direction: column; gap: 2px; }
  }
</style>
