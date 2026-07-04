<script>
  import {onMount} from 'svelte'
  import {FindDuplicateGames, RemoveGame, RestoreGame, GetGames} from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'

  let {
    onDedupDone = () => {},
  } = $props()

  let groups = $state([])
  let loading = $state(true)
  let error = $state('')
  let resolving = $state({})       // {gameId: true} for in-progress resolves

  async function load() {
    loading = true
    error = ''
    try {
      groups = await FindDuplicateGames()
    } catch (e) {
      error = String(e)
    }
    loading = false
  }

  async function handleRemove(id, groupIdx) {
    if (!window.confirm('Remove this duplicate?')) return
    resolving = {...resolving, [id]: true}
    try {
      await RemoveGame(id, false)
      // Remove from local state
      groups[groupIdx].games = groups[groupIdx].games.filter(g => g.id !== id)
      groups[groupIdx].count--
      groups = groups.filter(g => g.count >= 2)
      groups = [...groups]
    } catch (e) {
      console.error('Failed to remove:', e)
    }
    resolving = {...resolving, [id]: false}
  }

  async function handleKeep(id, groupIdx) {
    // Remove all other games in the group, keep this one
    const others = groups[groupIdx].games.filter(g => g.id !== id)
    if (others.length === 0) return

    const msg = `Keep "${groups[groupIdx].games.find(g => g.id === id)?.title}" and remove ${others.length} duplicate${others.length > 1 ? 's' : ''}?`
    if (!window.confirm(msg)) return

    resolving = {...resolving}
    for (const g of others) {
      resolving[g.id] = true
    }
    resolving = {...resolving}

    try {
      for (const g of others) {
        await RemoveGame(g.id, false)
      }
      groups[groupIdx].games = groups[groupIdx].games.filter(g => g.id === id)
      groups[groupIdx].count = 1
      groups = groups.filter(g => g.count >= 2)
      groups = [...groups]
    } catch (e) {
      console.error('Failed to remove duplicates:', e)
    }

    const clean = {}
    for (const g of others) clean[g.id] = false
    resolving = {...resolving, ...clean}
  }

  // ── Engine colors — imported from shared module ───────────────
  // See engineColors.js for the canonical palette matching TUI styles

  function statusLabel(s) {
    const labels = {active: 'Active', completed: 'Completed', abandoned: 'Abandoned', on_hold: 'On Hold', unknown: 'Unknown'}
    return labels[s] || s || 'Unknown'
  }

  function formatBytes(bytes) {
    if (!bytes) return ''
    const units = ['B', 'KB', 'MB', 'GB']
    let i = 0
    let val = bytes
    while (val >= 1024 && i < units.length - 1) { val /= 1024; i++ }
    return `${val.toFixed(1)} ${units[i]}`
  }

  onMount(load)
</script>

<div class="dedup-dialog">
  <div class="dedup-header">
    <h2>Duplicate Games</h2>
    <p class="dedup-subtitle">
      Games with similar titles detected across different directories.
      Keep one and remove the rest.
    </p>
    <button class="btn btn-sm" onclick={load} disabled={loading}>
      {loading ? 'Scanning…' : '⟳ Refresh'}
    </button>
  </div>

  {#if loading}
    <div class="loading-state"><div class="spinner"></div><p>Scanning for duplicates…</p></div>
  {:else if error}
    <div class="error-section"><p class="error-title">Error:</p><p class="error-line">{error}</p></div>
  {:else if groups.length === 0}
    <div class="empty-state">
      <span class="empty-icon">✓</span>
      <p class="empty-title">No duplicates found</p>
      <p class="empty-desc">All game titles appear to be unique.</p>
    </div>
  {:else}
    <div class="summary-bar">
      Found <strong>{groups.length}</strong> duplicate group{groups.length > 1 ? 's' : ''} totaling
      <strong>{groups.reduce((acc, g) => acc + g.count, 0)}</strong> extra entr{groups.reduce((acc, g) => acc + g.count, 0) > 1 ? 'ies' : 'y'}.
    </div>

    {#each groups as group, groupIdx}
      <div class="dup-group">
        <div class="group-header">
          <div class="group-title-section">
            <h3 class="group-title">{group.title}</h3>
            <span class="group-count">{group.count} copies</span>
          </div>
        </div>

        <div class="group-entries">
          {#each group.games as game, gameIdx}
            <div class="dup-entry" class:dup-entry-alt={gameIdx % 2 === 1}>
              <div class="entry-primary">
                <span class="entry-idx">#{gameIdx + 1}</span>
                <span class="entry-engine" style="--ec: {engineColor(game.engine)}">{game.engine || '—'}</span>
                <span class="entry-version">{game.version || '—'}</span>
                <span class="entry-status status-{game.status || 'unknown'}">{statusLabel(game.status)}</span>
              </div>
              <div class="entry-details">
                <span class="entry-size">{formatBytes(game.sizeBytes)}</span>
                <span class="entry-path" title={game.path}>{game.path}</span>
              </div>
              <div class="entry-actions">
                <button
                  class="btn btn-xs btn-primary"
                  onclick={() => handleKeep(game.id, groupIdx)}
                  disabled={resolving[game.id]}
                  title="Remove all other copies, keep this one"
                >
                  {resolving[game.id] ? '…' : 'Keep'}
                </button>
                <button
                  class="btn btn-xs btn-danger"
                  onclick={() => handleRemove(game.id, groupIdx)}
                  disabled={resolving[game.id]}
                >
                  {resolving[game.id] ? '…' : 'Remove'}
                </button>
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/each}
  {/if}
</div>

<style>
  .dedup-dialog {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 800px;
    margin: 0 auto;
    width: 100%;
  }

  .dedup-header {
    margin-bottom: 20px;
  }
  .dedup-header h2 {
    font-size: 20px;
    font-weight: 700;
    margin: 0 0 4px;
  }
  .dedup-subtitle {
    font-size: 13px;
    color: var(--text-secondary);
    margin: 0 0 12px;
  }

  /* ── States ────────────────────────── */
  .loading-state, .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 60px 0;
    gap: 8px;
    color: var(--text-muted);
  }
  .spinner {
    width: 24px; height: 24px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  .empty-icon { font-size: 32px; opacity: 0.6; }
  .empty-title { font-size: 16px; font-weight: 600; color: var(--text-secondary); }
  .empty-desc { font-size: 13px; }

  .error-section {
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
    font-family: var(--font-mono);
    margin: 0;
  }

  /* ── Summary ───────────────────────── */
  .summary-bar {
    padding: 10px 14px;
    margin-bottom: 16px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
    font-size: 13px;
    color: var(--text-secondary);
  }
  .summary-bar strong { color: var(--text-primary); }

  /* ── Duplicate Group ───────────────── */
  .dup-group {
    margin-bottom: 20px;
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
    background: var(--bg-secondary);
  }

  .group-header {
    padding: 10px 14px;
    background: var(--bg-tertiary);
    border-bottom: 1px solid var(--border);
  }
  .group-title-section {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .group-title {
    font-size: 15px;
    font-weight: 600;
    margin: 0;
    color: var(--text-primary);
  }
  .group-count {
    font-size: 11px;
    padding: 1px 7px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--warning) 20%, transparent);
    color: var(--warning);
    font-weight: 600;
  }

  /* ── Entry Row ─────────────────────── */
  .group-entries {
    display: flex;
    flex-direction: column;
  }

  .dup-entry {
    display: flex;
    align-items: center;
    padding: 10px 14px;
    gap: 12px;
  }
  .dup-entry-alt {
    background: color-mix(in srgb, var(--bg-hover) 50%, transparent);
  }

  .entry-primary {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 260px;
    flex-shrink: 0;
  }
  .entry-idx {
    font-size: 11px;
    color: var(--text-muted);
    font-family: var(--font-mono);
    min-width: 22px;
  }
  .entry-engine {
    font-size: 11px;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
    min-width: 50px;
    text-align: center;
  }
  .entry-version {
    font-size: 12px;
    color: var(--text-secondary);
    font-family: var(--font-mono);
    min-width: 50px;
  }
  .entry-status {
    font-size: 11px;
    font-weight: 500;
    padding: 1px 6px;
    border-radius: 4px;
  }
  :global(.status-active) { background: color-mix(in srgb, var(--success) 15%, transparent); color: var(--success); }
  :global(.status-completed) { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); }
  :global(.status-abandoned) { background: color-mix(in srgb, var(--text-muted) 15%, transparent); color: var(--text-muted); }
  :global(.status-on_hold) { background: color-mix(in srgb, var(--warning) 15%, transparent); color: var(--warning); }
  :global(.status-unknown) { background: color-mix(in srgb, var(--text-muted) 10%, transparent); color: var(--text-muted); }

  .entry-details {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .entry-size {
    font-size: 11px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .entry-path {
    font-size: 11px;
    color: var(--text-muted);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .entry-actions {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  /* ── Shared utility styles ─────────── */
  .btn {
    padding: 7px 16px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: transparent;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    transition: background 0.12s;
    white-space: nowrap;
  }
  .btn:hover { background: var(--bg-hover); }
  .btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-sm { padding: 4px 10px; font-size: 12px; }
  .btn-xs { padding: 2px 8px; font-size: 11px; border-radius: 4px; }
  .btn-primary { background: var(--accent); color: #fff; border-color: var(--accent); }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
  .btn-danger { color: var(--danger); border-color: color-mix(in srgb, var(--danger) 30%, transparent); }
  .btn-danger:hover:not(:disabled) { background: color-mix(in srgb, var(--danger) 10%, transparent); }
</style>
