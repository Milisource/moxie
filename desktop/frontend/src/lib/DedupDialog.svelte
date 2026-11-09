<script>
  import {onMount} from 'svelte'
  import {FindDuplicateGames, RemoveDuplicate} from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'
  import {statusLabel} from './statuses.js'
  import {formatBytes} from './format.js'
  import Button from './Button.svelte'
  import Spinner from './Spinner.svelte'
  import {confirmAction} from './confirmDialog.svelte.js'

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
    const ok = await confirmAction({title: 'Remove duplicate?', description: 'Remove this duplicate?', confirmLabel: 'Remove', danger: true})
    if (!ok) return
    resolving = {...resolving, [id]: true}
    try {
      await RemoveDuplicate(id)
      // Remove from local state
      groups[groupIdx].games = groups[groupIdx].games.filter(g => g.id !== id)
      groups[groupIdx].count--
      groups = groups.filter(g => g.count >= 2)
      groups = [...groups]
      onDedupDone()
    } catch (e) {
      console.error('Failed to remove:', e)
      // The DB may or may not have been mutated — resync from the source
      // of truth so the UI matches what is actually in the library.
      await load()
    }
    resolving = {...resolving, [id]: false}
  }

  async function handleKeep(id, groupIdx) {
    // Remove all other games in the group, keep this one
    const others = groups[groupIdx].games.filter(g => g.id !== id)
    if (others.length === 0) return

    const msg = `Keep "${groups[groupIdx].games.find(g => g.id === id)?.title}" and remove ${others.length} duplicate${others.length > 1 ? 's' : ''}?`
    const ok = await confirmAction({title: 'Remove duplicates?', description: msg, confirmLabel: 'Remove', danger: true})
    if (!ok) return

    resolving = {...resolving}
    for (const g of others) {
      resolving[g.id] = true
    }
    resolving = {...resolving}

    try {
      for (const g of others) {
        await RemoveDuplicate(g.id)
      }
      groups[groupIdx].games = groups[groupIdx].games.filter(g => g.id === id)
      groups[groupIdx].count = 1
      groups = groups.filter(g => g.count >= 2)
      groups = [...groups]
      onDedupDone()
    } catch (e) {
      console.error('Failed to remove duplicates:', e)
      // A mid-loop failure may have already deleted some games from the DB
      // while the local list was never updated — resync to reflect reality.
      await load()
    }

    const clean = {}
    for (const g of others) clean[g.id] = false
    resolving = {...resolving, ...clean}
  }

  // ── Engine colors — imported from shared module ───────────────
  // See engineColors.js for the canonical palette matching TUI styles

  onMount(load)
</script>

<div class="dedup-dialog">
  <div class="dedup-header">
    <h2>Duplicate Games</h2>
    <p class="dedup-subtitle">
      Games with similar titles detected across different directories.
      Keep one and remove the rest.
    </p>
    <Button size="sm" onclick={load} disabled={loading}>
      {loading ? 'Scanning…' : '⟳ Refresh'}
    </Button>
  </div>

  {#if loading}
    <div class="loading-state"><Spinner /><p>Scanning for duplicates…</p></div>
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
                <Button
                  size="xs"
                  variant="primary"
                  onclick={() => handleKeep(game.id, groupIdx)}
                  disabled={resolving[game.id]}
                  title="Remove all other copies, keep this one"
                >
                  {resolving[game.id] ? '…' : 'Keep'}
                </Button>
                <Button
                  size="xs"
                  variant="danger"
                  onclick={() => handleRemove(game.id, groupIdx)}
                  disabled={resolving[game.id]}
                >
                  {resolving[game.id] ? '…' : 'Remove'}
                </Button>
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
    padding: var(--space-8) var(--gutter);
    width: 100%;
  }

  .dedup-header {
    margin-bottom: 20px;
  }
  .dedup-header h2 {
    font-size: var(--text-2xl);
    font-weight: 700;
    margin: 0 0 4px;
  }
  .dedup-subtitle {
    font-size: var(--text-base);
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
  .empty-icon { font-size: var(--text-3xl); opacity: 0.6; }
  .empty-title { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); }
  .empty-desc { font-size: var(--text-base); }

  .error-section {
    padding: 16px;
    border: 1px solid var(--danger);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .error-title {
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--danger);
    margin: 0 0 4px;
  }
  .error-line {
    font-size: var(--text-sm);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    margin: 0;
  }

  /* ── Summary ───────────────────────── */
  .summary-bar {
    padding: 10px 14px;
    margin-bottom: 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    font-size: var(--text-base);
    color: var(--text-secondary);
  }
  .summary-bar strong { color: var(--text-primary); }

  /* ── Duplicate Group ───────────────── */
  .dup-group {
    margin-bottom: 20px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
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
    font-size: var(--text-lg);
    font-weight: 600;
    margin: 0;
    color: var(--text-primary);
  }
  .group-count {
    font-size: var(--text-xs);
    padding: 1px 7px;
    border-radius: var(--radius-1);
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
    font-size: var(--text-xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
    min-width: 22px;
  }
  .entry-engine {
    font-size: var(--text-xs);
    font-weight: 600;
    padding: 1px 6px;
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
    min-width: 50px;
    text-align: center;
  }
  .entry-version {
    font-size: var(--text-sm);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    min-width: 50px;
  }
  .entry-status {
    font-size: var(--text-xs);
    font-weight: 500;
    padding: 1px 6px;
    border-radius: var(--radius-1);
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
    font-size: var(--text-xs);
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .entry-path {
    font-size: var(--text-xs);
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
</style>
