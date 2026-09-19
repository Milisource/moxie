<script>
  import {onMount} from 'svelte'
  import {
    GetCollections,
    CreateCollection,
    DeleteCollection,
    GetCollectionGames,
    GetGames,
    GetCoverBaseURL,
  } from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'
  import {collectionsView, addSmartCollection, removeSmartCollection, library} from './viewState.svelte.js'
  import {makeCoverHelpers} from './cover.js'

  let {onOpenDetail = () => {}, onCollectionsChanged = () => {}} = $props()

  // How many covers a collage tile shows at most — mirrors the backend's
  // collectionCoverLimit (desktop/app.go), which already caps `coverIds`.
  const COLLAGE_LIMIT = 4

  // ── Collections ───────────────────────────────────────────────
  let collections = $state([])
  let loading = $state(true)
  let error = $state('')

  // Monotonic request id for GetCollectionGames. Rapid A→B clicks: A's slow
  // response must never overwrite B's games (mirrors F95Browser's searchSeq
  // pattern). An instance-level counter is sufficient because `games` is
  // instance-local too — a stale response from a destroyed instance writes to
  // dead state, and the remount's replay (below) bumps its own counter.
  let gamesSeq = 0

  let newName = $state('')
  let creating = $state(false)

  // Currently expanded collection -> its games. collectionsView.selectedId lives in
  // viewState so the expanded collection survives tab switches; the games
  // list itself refetches on mount (handleSelect is replayed below).
  let games = $state([])
  let gamesLoading = $state(false)

  // ── Covers ────────────────────────────────────────────────────
  // Same loopback-HTTP cover server GameList.svelte uses; failedCovers/
  // coverEpoch live in the shared `library` viewState object so a retry
  // triggered from the Library tab (sync/backfill) also clears stale 404s
  // cached here.
  let coverBase = $state('')
  const {coverSrc, markFailed} = makeCoverHelpers(() => coverBase)

  // ── Smart collections (client-side auto-groups, no backend rule storage —
  // see docs/desktop-ui-research.md §7 item 6) ────────────────────
  // Rules match against the full game list, so it's fetched once here
  // rather than reusing GameList's copy (that view is unmounted whenever
  // this tab is active).
  let allGames = $state([])
  let allGamesLoading = $state(true)

  let smartField = $state('engine')
  let smartValue = $state('')

  const smartFieldValues = $derived.by(() => {
    const values = new Set()
    for (const g of allGames) {
      const v = smartField === 'engine' ? g.engine : g.status
      if (v) values.add(v)
    }
    return [...values].sort((a, b) => a.localeCompare(b))
  })

  // Keep the value dropdown pointed at something real when the field changes
  // or the game list finishes loading (initial state, or a value that no
  // longer exists after games are removed).
  $effect(() => {
    if (!smartFieldValues.includes(smartValue)) smartValue = smartFieldValues[0] ?? ''
  })

  function smartMatches(rule) {
    return allGames.filter((g) => (rule.field === 'engine' ? g.engine : g.status) === rule.value)
  }

  function smartCoverIds(matches) {
    const ids = []
    for (const g of matches) {
      if (!g.hasCover) continue
      ids.push(g.id)
      if (ids.length === COLLAGE_LIMIT) break
    }
    return ids
  }

  function handleAddSmart() {
    if (!smartValue) return
    addSmartCollection(smartField, smartValue)
  }

  function handleSelectSmart(rule) {
    collectionsView.selectedSmartId = collectionsView.selectedSmartId === rule.id ? null : rule.id
  }

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

  async function loadAllGames() {
    allGamesLoading = true
    try {
      allGames = (await GetGames()) || []
    } catch (e) {
      // Smart collections just show as empty; the real-collections error
      // line above covers surfacing backend failures.
      allGames = []
    }
    allGamesLoading = false
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
      if (collectionsView.selectedId === c.id) {
        collectionsView.selectedId = null
        games = []
      }
      await loadCollections()
      onCollectionsChanged()
    } catch (e) {
      error = String(e)
    }
  }

  async function loadGamesFor(id) {
    if (id === null || id === undefined) {
      gamesSeq++                    // invalidate any in-flight request
      games = []
      return
    }
    const seq = ++gamesSeq
    gamesLoading = true
    try {
      const res = await GetCollectionGames(id)
      if (seq !== gamesSeq) return   // stale — a newer selection owns the list
      games = res || []
      error = ''
    } catch (e) {
      if (seq !== gamesSeq) return
      error = String(e)
      games = []
    }
    if (seq === gamesSeq) gamesLoading = false
  }

  async function handleSelect(c) {
    if (collectionsView.selectedId === c.id) {
      collectionsView.selectedId = null
      games = []
      return
    }
    collectionsView.selectedId = c.id
    await loadGamesFor(c.id)
  }

  onMount(async () => {
    try {
      coverBase = await GetCoverBaseURL()
    } catch (e) {
      console.error('Failed to get cover base URL', e)
    }
    await Promise.all([loadCollections(), loadAllGames()])
    // Re-expand the collection that was open when the user left this tab.
    if (collectionsView.selectedId !== null) await loadGamesFor(collectionsView.selectedId)
  })
</script>

{#snippet collage(ids, count)}
  <div
    class="collage"
    class:n1={ids.length === 1}
    class:n2={ids.length === 2}
    class:n3={ids.length === 3}
    class:n4={ids.length >= 4}
    class:empty={ids.length === 0}
  >
    {#if ids.length === 0}
      <span class="collage-empty-icon">▭</span>
    {:else}
      {#each ids as id (id)}
        {#if coverBase && !library.failedCovers.has(id)}
          <img class="collage-img" src={coverSrc(id)} alt="" loading="lazy" onerror={() => markFailed(id)} />
        {:else}
          <span class="collage-cell-ph">⊠</span>
        {/if}
      {/each}
      {#if count > ids.length}
        <span class="collage-more">+{count - ids.length}</span>
      {/if}
    {/if}
  </div>
{/snippet}

{#snippet gameThumb(g)}
  {#if g.hasCover && coverBase && !library.failedCovers.has(g.id)}
    <img class="thumb-img" src={coverSrc(g.id)} alt="" loading="lazy" onerror={() => markFailed(g.id)} />
  {:else}
    <span class="thumb-ph">▭</span>
  {/if}
{/snippet}

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
          <div class="coll-row" class:expanded={collectionsView.selectedId === c.id}>
            <button class="coll-main" onclick={() => handleSelect(c)}>
              {@render collage(c.coverIds || [], c.gameCount)}
              <span class="coll-info">
                <span class="coll-name">{c.name}</span>
                <span class="coll-count">{c.gameCount} game{c.gameCount !== 1 ? 's' : ''}</span>
              </span>
              <span class="coll-caret">{collectionsView.selectedId === c.id ? '▾' : '▸'}</span>
            </button>
            <button class="btn btn-remove" title="Delete collection" onclick={() => handleDelete(c)}>✕</button>
          </div>

          {#if collectionsView.selectedId === c.id}
            <div class="coll-games">
              {#if gamesLoading}
                <p class="muted">Loading games…</p>
              {:else if games.length === 0}
                <p class="muted">No games in this collection yet.</p>
              {:else}
                {#each games as g}
                  <button class="game-row" onclick={() => onOpenDetail(g.id)}>
                    <span class="game-thumb">{@render gameThumb(g)}</span>
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

  <!-- ── Smart collections (P1 item 6): auto-grouped by engine/status. ── -->
  <div class="smart-header">
    <h3>Smart collections</h3>
    <p class="collections-subtitle">
      Auto-grouped by engine or status — no games to add or remove, membership updates itself as your library changes.
    </p>
  </div>

  <div class="add-row">
    <select class="smart-select" bind:value={smartField}>
      <option value="engine">Engine</option>
      <option value="status">Status</option>
    </select>
    <select class="smart-select" bind:value={smartValue} disabled={smartFieldValues.length === 0}>
      {#each smartFieldValues as v}
        <option value={v}>{v}</option>
      {/each}
    </select>
    <button class="btn btn-primary" onclick={handleAddSmart} disabled={!smartValue}>+ Add smart collection</button>
  </div>

  {#if allGamesLoading}
    <p class="muted">Loading…</p>
  {:else if collectionsView.smartCollections.length === 0}
    <div class="empty">
      <p>No smart collections yet. Pick an engine or status above and add one.</p>
    </div>
  {:else}
    <div class="coll-list">
      {#each collectionsView.smartCollections as rule (rule.id)}
        {@const matches = smartMatches(rule)}
        <div class="coll-block">
          <div class="coll-row" class:expanded={collectionsView.selectedSmartId === rule.id}>
            <button class="coll-main" onclick={() => handleSelectSmart(rule)}>
              {@render collage(smartCoverIds(matches), matches.length)}
              <span class="coll-info">
                <span class="coll-name">{rule.field === 'engine' ? 'Engine' : 'Status'}: {rule.value}</span>
                <span class="coll-count">{matches.length} game{matches.length !== 1 ? 's' : ''}</span>
              </span>
              <span class="coll-caret">{collectionsView.selectedSmartId === rule.id ? '▾' : '▸'}</span>
            </button>
            <button class="btn btn-remove" title="Remove smart collection" onclick={() => removeSmartCollection(rule.id)}>✕</button>
          </div>

          {#if collectionsView.selectedSmartId === rule.id}
            <div class="coll-games">
              {#if matches.length === 0}
                <p class="muted">No games match this rule right now.</p>
              {:else}
                {#each matches as g}
                  <button class="game-row" onclick={() => onOpenDetail(g.id)}>
                    <span class="game-thumb">{@render gameThumb(g)}</span>
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

  .smart-header { margin: 36px 0 20px; }
  .smart-header h3 { font-size: 16px; font-weight: 700; margin: 0 0 4px; }

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

  .smart-select {
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: 13px;
    outline: none;
  }
  .smart-select:focus { border-color: var(--accent); }

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

  .coll-list { display: flex; flex-direction: column; gap: 8px; }

  .coll-row {
    display: flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-secondary);
    padding-right: 6px;
  }
  .coll-row.expanded { border-bottom-left-radius: 0; border-bottom-right-radius: 0; }

  .coll-main {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: transparent;
    border: none;
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    text-align: left;
    min-width: 0;
  }
  .coll-main:hover { background: var(--bg-hover); }
  .coll-info { flex: 1; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .coll-name { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .coll-count { font-size: 12px; color: var(--text-muted); }
  .coll-caret { color: var(--text-muted); font-size: 11px; flex-shrink: 0; }

  /* ── Collage tile (collapsed-view cover preview) ──────────────
     2x2-ish grid of member covers; degrades gracefully for 0/1/2/3 covers
     and shows a "+N" overlay when the collection has more games than the
     collage can display. */
  .collage {
    width: 56px;
    height: 56px;
    flex-shrink: 0;
    border-radius: 6px;
    overflow: hidden;
    display: grid;
    grid-template-columns: 1fr 1fr;
    grid-template-rows: 1fr 1fr;
    gap: 1px;
    background: var(--border);
    position: relative;
  }
  .collage.n1 { grid-template-columns: 1fr; grid-template-rows: 1fr; }
  .collage.n2 { grid-template-columns: 1fr 1fr; grid-template-rows: 1fr; }
  .collage.n3 .collage-img:nth-child(3),
  .collage.n3 .collage-cell-ph:nth-child(3) {
    grid-column: 1 / 3;
  }
  .collage.empty {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-primary);
    border: 1px dashed var(--border);
  }
  .collage-empty-icon { font-size: 22px; opacity: 0.5; }
  .collage-img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .collage-cell-ph {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    background: var(--bg-primary);
    font-size: 14px;
    color: var(--text-muted);
  }
  .collage-more {
    position: absolute;
    bottom: 2px;
    right: 2px;
    background: rgba(0, 0, 0, 0.65);
    color: #fff;
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
    padding: 3px 5px;
    border-radius: 8px;
  }

  .coll-games {
    border: 1px solid var(--border);
    border-top: none;
    border-bottom-left-radius: 8px;
    border-bottom-right-radius: 8px;
    padding: 8px;
    background: var(--bg-primary);
  }

  .game-row {
    width: 100%;
    display: grid;
    grid-template-columns: 32px 1fr 90px 100px 80px;
    align-items: center;
    gap: 10px;
    padding: 6px 10px;
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

  .game-thumb {
    width: 32px;
    height: 32px;
    border-radius: 4px;
    overflow: hidden;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-secondary);
    flex-shrink: 0;
  }
  .thumb-img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .thumb-ph { font-size: 14px; color: var(--text-muted); }

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
