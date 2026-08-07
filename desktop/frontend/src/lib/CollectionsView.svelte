<script>
  import {onMount} from 'svelte'
  import {
    GetCollections,
    CreateCollection,
    DeleteCollection,
    GetCollectionGames,
  } from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'

  let {onOpenDetail = () => {}, onCollectionsChanged = () => {}} = $props()

  let collections = $state([])
  let loading = $state(true)
  let error = $state('')

  let newName = $state('')
  let creating = $state(false)

  // Currently expanded collection -> its games.
  let selectedId = $state(null)
  let games = $state([])
  let gamesLoading = $state(false)

  async function loadCollections() {
    loading = true
    try {
      collections = (await GetCollections()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
    loading = false
  }

  async function handleCreate() {
    const name = newName.trim()
    if (!name || creating) return
    creating = true
    try {
      await CreateCollection(name)
      newName = ''
      await loadCollections()
      onCollectionsChanged()
      error = ''
    } catch (e) {
      error = String(e)
    }
    creating = false
  }

  async function handleDelete(c) {
    if (!confirm(`Delete the collection "${c.name}"? The games themselves are not removed.`)) return
    try {
      await DeleteCollection(c.id)
      if (selectedId === c.id) {
        selectedId = null
        games = []
      }
      await loadCollections()
      onCollectionsChanged()
    } catch (e) {
      error = String(e)
    }
  }

  async function handleSelect(c) {
    if (selectedId === c.id) {
      selectedId = null
      games = []
      return
    }
    selectedId = c.id
    gamesLoading = true
    try {
      games = (await GetCollectionGames(c.id)) || []
      error = ''
    } catch (e) {
      error = String(e)
      games = []
    }
    gamesLoading = false
  }

  onMount(loadCollections)
</script>

<div class="collections-view">
  <div class="collections-header">
    <h2>Collections</h2>
    <p class="collections-subtitle">Group games however you like. A game can belong to several collections.</p>
  </div>

  <div class="add-row">
    <input
      type="text"
      class="name-input"
      placeholder="New collection name"
      bind:value={newName}
      onkeydown={(e) => e.key === 'Enter' && handleCreate()}
    />
    <button class="btn btn-primary" onclick={handleCreate} disabled={!newName.trim() || creating}>
      {creating ? 'Creating…' : 'Create'}
    </button>
  </div>

  {#if error}
    <p class="error-line">{error}</p>
  {/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if collections.length === 0}
    <div class="empty">
      <p>No collections yet. Create one above, then add games from their detail page.</p>
    </div>
  {:else}
    <div class="coll-list">
      {#each collections as c}
        <div class="coll-block">
          <div class="coll-row" class:expanded={selectedId === c.id}>
            <button class="coll-main" onclick={() => handleSelect(c)}>
              <span class="coll-caret">{selectedId === c.id ? '▾' : '▸'}</span>
              <span class="coll-name">{c.name}</span>
              <span class="coll-count">{c.gameCount} game{c.gameCount !== 1 ? 's' : ''}</span>
            </button>
            <button class="btn btn-remove" title="Delete collection" onclick={() => handleDelete(c)}>✕</button>
          </div>

          {#if selectedId === c.id}
            <div class="coll-games">
              {#if gamesLoading}
                <p class="muted">Loading games…</p>
              {:else if games.length === 0}
                <p class="muted">No games in this collection yet.</p>
              {:else}
                {#each games as g}
                  <button class="game-row" onclick={() => onOpenDetail(g.id)}>
                    <span class="game-title">{g.title}</span>
                    <span class="engine-chip" style="--chip: {engineColor(g.engine)}">{g.engine || '—'}</span>
                    <span class="game-version">{g.version || '—'}</span>
                    <span class="game-size">{g.sizeLabel}</span>
                  </button>
                {/each}
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .collections-view {
    flex: 1;
    overflow: auto;
    padding: 32px;
    max-width: 860px;
    margin: 0 auto;
    width: 100%;
  }

  .collections-header { margin-bottom: 20px; }
  .collections-header h2 { font-size: 20px; font-weight: 700; margin: 0 0 4px; }
  .collections-subtitle { font-size: 13px; color: var(--text-secondary); margin: 0; }

  .add-row { display: flex; gap: 8px; margin-bottom: 16px; }
  .name-input {
    flex: 1;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
  }
  .name-input:focus { border-color: var(--accent); }

  .btn {
    padding: 7px 16px;
    border: none;
    border-radius: 6px;
    font-size: 13px;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-primary { background: var(--accent); color: #fff; }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
  .btn-remove {
    background: transparent;
    color: var(--text-muted);
    padding: 6px 10px;
    font-size: 12px;
  }
  .btn-remove:hover {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }

  .coll-list { display: flex; flex-direction: column; gap: 6px; }

  .coll-row {
    display: flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-secondary);
    padding-right: 6px;
  }
  .coll-row.expanded { border-bottom-left-radius: 0; border-bottom-right-radius: 0; }

  .coll-main {
    flex: 1;
    display: grid;
    grid-template-columns: 16px 1fr auto;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: transparent;
    border: none;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    text-align: left;
  }
  .coll-main:hover { background: var(--bg-hover); }
  .coll-caret { color: var(--text-muted); font-size: 11px; }
  .coll-name { font-weight: 600; }
  .coll-count { font-size: 12px; color: var(--text-muted); }

  .coll-games {
    border: 1px solid var(--border);
    border-top: none;
    border-bottom-left-radius: 6px;
    border-bottom-right-radius: 6px;
    padding: 8px;
    background: var(--bg-primary);
  }

  .game-row {
    width: 100%;
    display: grid;
    grid-template-columns: 1fr 90px 100px 80px;
    align-items: center;
    gap: 8px;
    padding: 7px 10px;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    text-align: left;
  }
  .game-row:hover { background: var(--bg-hover); }
  .game-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .game-version, .game-size { font-size: 12px; color: var(--text-muted); font-family: var(--font-mono); }

  .engine-chip {
    justify-self: start;
    padding: 2px 8px;
    border-radius: 10px;
    font-size: 11px;
    font-weight: 600;
    background: color-mix(in srgb, var(--chip) 18%, transparent);
    color: var(--chip);
  }

  .empty {
    padding: 40px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
    border: 1px dashed var(--border);
    border-radius: 8px;
  }
  .muted { color: var(--text-muted); font-size: 13px; padding: 8px 10px; margin: 0; }
  .error-line { color: var(--danger); font-size: 12px; margin: 0 0 12px; font-family: var(--font-mono); }
</style>
