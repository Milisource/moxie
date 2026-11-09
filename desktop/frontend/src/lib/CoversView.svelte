<script>
  import {onMount} from 'svelte'
  import Button from './Button.svelte'
  import {GetCoverArtSettings} from '../../wailsjs/go/main/App'
  import {initials} from './initials.js'

  // Presentational only — cover-fetch state and the covers:* event
  // subscriptions live in the app shell (App.svelte) so an in-flight run
  // survives tab switches, exactly like sync/scan/update state. This view
  // just renders the state and calls onFetch.
  let {
    gameCount = null,
    games = [],
    fetching = false,
    progress = {current: 0, total: 0, title: '', phase: ''},
    result = null,
    coverError = '',
    upgrading = false,
    upgradeResult = null,
    onFetch = () => {},
    onUpgrade = () => {},
    onNavigate = () => {},
  } = $props()

  let busy = $derived(fetching || upgrading)
  let missing = $derived(games.filter(g => !g.hasCover))
  let total = $derived(games.length || gameCount || 0)
  let covered = $derived(Math.max(0, total - missing.length))
  let coveragePct = $derived(total ? Math.round((covered / total) * 100) : 0)

  // Sources are configured in Settings; read them so the panel reflects the
  // real configuration rather than a hard-coded list.
  let sources = $state({steam: true, vndb: false, sgdbKeySet: false})
  onMount(async () => {
    try { sources = await GetCoverArtSettings() } catch (e) { /* keep defaults */ }
  })

  let progressPct = $derived.by(() => {
    if (!progress.total) return 0
    return Math.round((progress.current / progress.total) * 100)
  })

  let phaseLabel = $derived.by(() => {
    if (upgrading) {
      return progress.total
        ? `Searching Steam / SteamGridDB / VNDB… (${progress.current}/${progress.total})`
        : 'Starting…'
    }
    if (!progress.phase) return 'Starting…'
    if (progress.phase === 'resolving') {
      return `Finding covers… (${progress.current}/${progress.total})`
    }
    if (progress.phase === 'downloading') {
      return `Downloading covers… (${progress.current}/${progress.total})`
    }
    return 'Fetching covers…'
  })

  let lastRun = $derived.by(() => {
    if (result) {
      if (result.total === 0) return 'Nothing to do — every game already has a cover.'
      return `${result.fetched} fetched · ${result.failed} failed · ${result.skipped} skipped`
    }
    if (upgradeResult) return `${upgradeResult.replaced} replaced · ${upgradeResult.failed} failed`
    return 'Not run this session'
  })
</script>

<div class="covers-view">
  <div class="covers-header">
    <div class="covers-heading">
      <h2>Cover art</h2>
      <p class="covers-subtitle">
        Missing covers are fetched from the stored URL first, then looked up on
        F95Zone, Steam, SteamGridDB and VNDB.
      </p>
    </div>
    <div class="header-actions">
      <Button onclick={onUpgrade} disabled={busy}>
        {upgrading ? 'Upgrading…' : 'Upgrade to portrait'}
      </Button>
      <Button variant="primary" onclick={onFetch} disabled={busy}>
        {fetching ? 'Fetching…' : 'Fetch missing covers'}
      </Button>
    </div>
  </div>

  <!-- Coverage -->
  <div class="coverage-strip">
    <div class="coverage-stat">
      <span class="coverage-value">{covered} / {total}</span>
      <span class="coverage-label">covers cached</span>
    </div>
    <div class="coverage-bar" role="progressbar" aria-valuenow={coveragePct} aria-valuemin="0" aria-valuemax="100">
      <div class="coverage-fill" style="width: {coveragePct}%"></div>
    </div>
    <span class="coverage-pct">{coveragePct}%</span>
    {#if missing.length > 0}
      <span class="missing-tag">{missing.length} missing</span>
    {/if}
    <span class="last-run">{lastRun}</span>
  </div>

  <!-- Active run -->
  {#if busy}
    <div class="progress-section">
      <div class="progress-bar-bg">
        <div class="progress-bar-fill" style="width: {progressPct}%"></div>
      </div>
      <p class="progress-label">{phaseLabel}</p>
      {#if progress.title}
        <p class="progress-current">{progress.title}</p>
      {/if}
    </div>
  {/if}

  <div class="covers-body">
    <section class="missing-panel">
      <div class="panel-head">
        <h3>Missing covers</h3>
        <span class="count">{missing.length}</span>
      </div>

      {#if missing.length === 0}
        <div class="missing-empty">
          <p class="missing-empty-title">Every game has a cover</p>
          <p class="missing-empty-hint">New covers appear here when a game is added without art.</p>
        </div>
      {:else}
        <div class="missing-grid">
          {#each missing.slice(0, 8) as game (game.id)}
            <div class="missing-card">
              <div class="missing-art">
                <span>{initials(game.title)}</span>
                <span class="missing-no-cover">No cover</span>
              </div>
              <p class="missing-title" title={game.title}>{game.title}</p>
              <p class="missing-engine">{game.engine || 'Unknown engine'}</p>
            </div>
          {/each}
        </div>
        {#if missing.length > 8}
          <p class="more-missing">and {missing.length - 8} more — fetch to fill them all.</p>
        {/if}
      {/if}
    </section>

    <aside class="sources-panel">
      <h3>Sources</h3>
      <div class="source-row">
        <span class="source-dot" class:on={sources.steam}></span>
        <span class="source-name">Steam</span>
        <span class="source-state">{sources.steam ? 'On' : 'Off'}</span>
      </div>
      <div class="source-row">
        <span class="source-dot" class:on={sources.vndb}></span>
        <span class="source-name">VNDB</span>
        <span class="source-state">{sources.vndb ? 'On' : 'Off'}</span>
      </div>
      <div class="source-row">
        <span class="source-dot" class:on={sources.sgdbKeySet}></span>
        <span class="source-name">SteamGridDB</span>
        <span class="source-state">{sources.sgdbKeySet ? 'Key set' : 'No key'}</span>
      </div>
      <div class="source-row">
        <span class="source-dot on"></span>
        <span class="source-name">F95Zone</span>
        <span class="source-state">Always</span>
      </div>
      <button class="link-btn" onclick={() => onNavigate('settings')}>Manage sources</button>
      <p class="source-note">Exact title match · portrait at least 600px · locked covers are skipped.</p>
    </aside>
  </div>

  {#if upgradeResult}
    <div class="result-section">
      <p class="result-title">Portrait upgrade complete</p>
      <p class="result-summary">
        {upgradeResult.replaced} replaced, {upgradeResult.failed} failed
        ({upgradeResult.checked} checked)
      </p>
      {#each upgradeResult.errors || [] as e}
        <p class="upgrade-err">{e}</p>
      {/each}
    </div>
  {/if}

  {#if coverError}
    <div class="error-section">
      <p class="error-title">Cover fetch failed:</p>
      <p class="error-line">{coverError}</p>
    </div>
  {/if}
</div>

<style>
  .covers-view {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 18px;
    padding: var(--space-7) var(--gutter);
    overflow-y: auto;
  }

  .covers-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 24px;
  }
  .covers-heading { min-width: 0; }
  .covers-header h2 { margin: 0 0 6px; font-size: var(--text-3xl); font-weight: 600; line-height: 1.15; letter-spacing: -0.01em; }
  .covers-subtitle {
    margin: 0;
    max-width: 66ch;
    color: var(--text-secondary);
    font-size: var(--text-sm);
    line-height: 1.5;
  }
  .header-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }

  .coverage-strip {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 16px 18px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
  }
  .coverage-stat { display: flex; flex-direction: column; gap: 2px; flex-shrink: 0; }
  .coverage-value { font-family: var(--font-mono); font-size: var(--text-2xl); font-weight: 600; color: var(--text-primary); }
  .coverage-label { font-size: var(--text-xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: var(--tracking-label); }
  .coverage-bar { flex: 1; height: 8px; background: var(--bg-tertiary); overflow: hidden; }
  .coverage-fill { height: 100%; background: var(--accent); transition: width 0.3s ease; }
  .coverage-pct { font-family: var(--font-mono); font-size: var(--text-sm); color: var(--text-secondary); }
  .missing-tag {
    padding: 3px 8px;
    border: 1px solid color-mix(in srgb, var(--warning) 50%, transparent);
    color: var(--warning);
    font-size: var(--text-xs);
    font-weight: 600;
    white-space: nowrap;
  }
  .last-run { font-size: var(--text-xs); color: var(--text-muted); white-space: nowrap; }

  .covers-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 320px;
    gap: 24px;
    align-items: start;
  }
  .missing-panel, .sources-panel {
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    padding: 18px;
  }
  .panel-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
  .panel-head h3, .sources-panel h3 { margin: 0; font-size: var(--text-lg); font-weight: 600; }
  .count {
    min-width: 24px;
    padding: 2px 7px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    text-align: center;
  }

  .missing-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
  .missing-card { min-width: 0; }
  .missing-art {
    position: relative;
    aspect-ratio: 3 / 4;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    border: 1px dashed var(--rule-strong);
    background: var(--bg-tertiary);
    color: var(--text-muted);
  }
  .missing-art > span:first-child { font-family: var(--font-display); font-size: var(--text-2xl); font-weight: 600; opacity: 0.55; }
  .missing-no-cover { font-size: var(--text-xs); }
  .missing-title {
    margin: 6px 0 0;
    color: var(--text-primary);
    font-size: var(--text-sm);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .missing-engine { margin: 2px 0 0; color: var(--text-muted); font-size: var(--text-xs); }
  .missing-empty { padding: 24px 0; text-align: center; }
  .missing-empty-title { margin: 0; font-weight: 600; color: var(--text-secondary); }
  .missing-empty-hint { margin: 4px 0 0; font-size: var(--text-sm); color: var(--text-muted); }
  .more-missing { margin: 12px 0 0; font-size: var(--text-xs); color: var(--text-muted); }

  .source-row {
    display: grid;
    grid-template-columns: 8px minmax(0, 1fr) auto;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
    border-bottom: 1px solid var(--border);
  }
  .source-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--text-muted); }
  .source-dot.on { background: var(--success); }
  .source-name { font-size: var(--text-sm); color: var(--text-secondary); }
  .source-state { font-size: var(--text-xs); color: var(--text-muted); font-family: var(--font-mono); }
  .link-btn {
    margin-top: 12px;
    padding: 0;
    border: none;
    background: none;
    color: var(--accent);
    font-size: var(--text-sm);
    font-weight: 600;
    cursor: pointer;
  }
  .source-note { margin: 14px 0 0; font-size: var(--text-xs); color: var(--text-muted); line-height: 1.5; }

  .progress-section { padding: 14px 16px; border: 1px solid var(--border); background: var(--bg-secondary); }
  .progress-bar-bg { height: 6px; background: var(--bg-tertiary); overflow: hidden; margin-bottom: 8px; }
  .progress-bar-fill { height: 100%; background: var(--accent); transition: width 0.2s ease; }
  .progress-label { margin: 0; font-size: var(--text-sm); color: var(--text-secondary); }
  .progress-current { margin: 4px 0 0; font-size: var(--text-xs); color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .result-section { padding: 14px 16px; border: 1px solid var(--success); background: color-mix(in srgb, var(--success) 8%, transparent); }
  .result-title { margin: 0 0 4px; font-weight: 600; color: var(--success); }
  .result-summary { margin: 0; font-size: var(--text-sm); color: var(--text-secondary); }
  .upgrade-err { margin: 4px 0 0; font-size: var(--text-xs); font-family: var(--font-mono); color: var(--text-muted); }

  .error-section { padding: 12px 16px; border: 1px solid var(--danger); background: color-mix(in srgb, var(--danger) 8%, transparent); }
  .error-title { margin: 0 0 4px; font-weight: 600; color: var(--danger); }
  .error-line { margin: 0; font-size: var(--text-sm); color: var(--danger); word-break: break-word; }

  @media (max-width: 1100px) {
    .covers-body { grid-template-columns: minmax(0, 1fr); }
    .missing-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  }
  @media (max-width: 760px) {
    .covers-header { flex-direction: column; }
    .missing-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
