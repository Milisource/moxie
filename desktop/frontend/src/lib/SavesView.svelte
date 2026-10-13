<script>
  import {onMount} from 'svelte'
  import {GetSavesOverview} from '../../wailsjs/go/main/App'
  import Button from './Button.svelte'
  import Spinner from './Spinner.svelte'

  // Cross-game save-set overview. Per-game work (apply/snapshot/restore/
  // import/pin) lives in the Saves section of GameDetail; a row here opens
  // that game. Refetched on every mount — the view is destroyed on tab
  // switch, and the summary is cheap, so no viewState cache is needed.
  let {onOpenDetail = () => {}} = $props()

  let games = $state([])
  let loading = $state(true)
  let error = $state('')

  async function load() {
    loading = true
    error = ''
    try {
      games = (await GetSavesOverview()) || []
    } catch (e) {
      error = String(e).replace(/^Error:\s*/, '')
      games = []
    }
    loading = false
  }

  onMount(load)

  let totalSets = $derived(games.reduce((sum, g) => sum + (g.sets || 0), 0))
  let activeGames = $derived(games.filter((g) => g.hasActive).length)
</script>

<div class="saves-view">
  <!-- ── Header ────────────────────────────────────────────── -->
  <div class="saves-header">
    <div class="saves-header-top">
      <h2>Saves</h2>
      <Button size="sm" onclick={load} disabled={loading}>
        {loading ? 'Refreshing…' : 'Refresh'}
      </Button>
    </div>
    <p class="saves-subtitle">
      Save sets you've snapshotted or imported, per game. Open a game to apply,
      snapshot, restore, import, or pin its live save location.
    </p>
  </div>

  <!-- ── Summary Stats ────────────────────────────────────── -->
  {#if !loading && !error && games.length > 0}
    <div class="stats-bar">
      <span class="stat-item">
        <span class="stat-value">{games.length}</span>
        <span class="stat-label">game{games.length !== 1 ? 's' : ''} with saves</span>
      </span>
      <span class="stat-divider"></span>
      <span class="stat-item">
        <span class="stat-value">{totalSets}</span>
        <span class="stat-label">save set{totalSets !== 1 ? 's' : ''}</span>
      </span>
      {#if activeGames > 0}
        <span class="stat-divider"></span>
        <span class="stat-item">
          <span class="stat-value">{activeGames}</span>
          <span class="stat-label">active</span>
        </span>
      {/if}
    </div>
  {/if}

  <!-- ── Error ─────────────────────────────────────────────── -->
  {#if error}
    <div class="error-section">
      <p class="error-title">Failed to load saves:</p>
      <p class="error-line">{error}</p>
      <Button size="sm" onclick={load}>Retry</Button>
    </div>
  {/if}

  <!-- ── Loading / Empty / List ───────────────────────────── -->
  {#if loading}
    <div class="status-section">
      <Spinner />
      <p class="status-text">Loading save sets…</p>
    </div>
  {:else if games.length === 0 && !error}
    <div class="empty">
      <p>No save sets yet — open a game to snapshot or import saves.</p>
    </div>
  {:else if games.length > 0}
    <div class="saves-list">
      <div class="list-header">
        <span class="lh-title">Game</span>
        <span class="lh-sets">Sets</span>
        <span class="lh-active">Active</span>
        <span class="lh-path">Live save location</span>
      </div>
      {#each games as g (g.gameId)}
        <button class="save-row" onclick={() => onOpenDetail(g.gameId)}>
          <span class="row-title" title={g.title}>{g.title}</span>
          <span class="row-sets">{g.sets} set{g.sets !== 1 ? 's' : ''}</span>
          <span class="row-active">
            {#if g.hasActive}
              <span class="active-badge">active</span>
            {:else}
              <span class="row-none">—</span>
            {/if}
          </span>
          <span class="row-path mono" title={g.livePath || ''}>{g.livePath || 'not resolved'}</span>
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .saves-view {
    flex: 1;
    overflow: auto;
    padding: var(--space-8) var(--gutter);
    width: 100%;
  }

  .saves-header { margin-bottom: 20px; }
  .saves-header-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .saves-header h2 { font-size: var(--text-2xl); font-weight: 700; margin: 0 0 4px; }
  .saves-subtitle {
    font-size: var(--text-base);
    color: var(--text-secondary);
    margin: 0;
    max-width: 640px;
    line-height: 1.5;
  }

  /* ── Stats Bar ──────────────────────── */
  .stats-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
    padding: 12px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
  }
  .stat-item { display: flex; align-items: baseline; gap: 6px; }
  .stat-value {
    font-size: var(--text-xl);
    font-weight: 700;
    color: var(--accent);
    font-family: var(--font-mono);
  }
  .stat-label { font-size: var(--text-sm); color: var(--text-muted); }
  .stat-divider {
    width: 1px;
    height: 20px;
    background: var(--border);
    flex-shrink: 0;
  }

  /* ── List ───────────────────────────── */
  .saves-list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    overflow: hidden;
  }
  .list-header,
  .save-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 90px 90px minmax(0, 1.4fr);
    align-items: center;
    gap: 12px;
    padding: 9px 14px;
  }
  .list-header {
    border-bottom: 1px solid var(--border);
    background: var(--bg-secondary);
  }
  .list-header span {
    font-family: var(--font-display);
    font-size: var(--text-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    color: var(--text-muted);
  }
  .save-row {
    border: none;
    border-bottom: 1px solid var(--border);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-base);
    text-align: left;
    cursor: pointer;
    transition: background 0.1s;
  }
  .save-row:last-child { border-bottom: none; }
  .save-row:hover { background: var(--bg-hover); }

  .row-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
  .row-sets { color: var(--text-secondary); font-family: var(--font-mono); font-size: var(--text-sm); }
  .row-active { display: flex; }
  .row-none { color: var(--text-muted); }
  .row-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--text-xs);
    color: var(--text-muted);
  }

  .active-badge {
    padding: 1px 7px;
    border-radius: var(--radius-1);
    font-size: var(--text-2xs);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    background: color-mix(in srgb, var(--success) 16%, transparent);
    color: var(--success);
  }

  /* ── Loading / empty ────────────────── */
  .status-section {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 48px 24px;
    color: var(--text-muted);
  }
  .status-text { margin: 0; font-size: var(--text-base); }

  .empty {
    padding: 40px;
    text-align: center;
    color: var(--text-muted);
    font-size: var(--text-base);
    border: 1px dashed var(--border);
    border-radius: var(--radius-1);
  }

  /* ── Error ──────────────────────────── */
  .error-section {
    padding: 12px 16px;
    margin-bottom: 16px;
    background: color-mix(in srgb, var(--danger) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent);
    border-radius: var(--radius-1);
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
  .error-title { margin: 0; font-weight: 600; color: var(--danger); font-size: var(--text-base); }
  .error-line {
    margin: 0;
    font-size: var(--text-sm);
    color: var(--danger);
    font-family: var(--font-mono);
    word-break: break-word;
  }

  .mono { font-family: var(--font-mono); }
</style>
