<script>
  // Single app-root "Edit Game" dialog backing editGameDialog.svelte.js's
  // openEditGame(). One form edits every user-editable field in a single
  // EditGame call; the backend treats Title as a metadata-only change (it
  // never renames the directory — that stays on RenameGame).
  import {Dialog} from 'bits-ui'
  import {
    GetGameDetail, EditGame, GetCoverBaseURL,
    FindCoverCandidates, SetGameCover, SetGameCoverFromFile,
    PickCoverImage, PreviewCoverFile,
  } from '../../wailsjs/go/main/App'
  import {engineColor, engineOptions} from './engineColors.js'
  import {GAME_STATUSES, statusLabel} from './statuses.js'
  import {editGameState, settleEditGame} from './editGameDialog.svelte.js'

  let loading = $state(false)
  let saving = $state(false)
  let error = $state('')
  let form = $state(null)

  // ── Cover art ──────────────────────────────────────────────
  // Cover edits are staged and applied on Save, like the text fields: the
  // dialog shows a live preview (the cached cover, the typed URL or the local
  // file) without touching the cache until Save succeeds.
  let coverBase = $state('')
  let cover = $state(null)        // persisted cover: {hasCover, coverUrl, coverW, coverH, coverSource}
  let coverUrlInput = $state('')
  let pending = $state(null)      // {kind:'url', url, source, preview} | {kind:'file', file, preview}
  let coverBusy = $state(false)
  let coverError = $state('')
  let coverStatus = $state('')
  let picker = $state({open: false, loading: false, error: '', items: []})

  let coverPreview = $derived.by(() => {
    if (pending?.preview) return pending.preview
    if (cover?.hasCover && coverBase) {
      const rev = cover.coverW ? `${cover.coverW}x${cover.coverH}${cover.coverSource ? '-' + cover.coverSource : ''}` : ''
      return `${coverBase}/cover/${editGameState.gameId}/thumb${rev ? '?v=' + rev : ''}`
    }
    return cover?.coverUrl || ''
  })

  let engineChoices = $derived.by(() => {
    const base = engineOptions()
    if (form?.engine && !base.includes(form.engine)) return [form.engine, ...base]
    return base
  })

  function fmtErr(e) {
    return String(e).replace(/^Error:\s*/, '')
  }

  // Reload whenever the dialog opens (or is re-targeted at another game).
  $effect(() => {
    const id = editGameState.gameId
    if (editGameState.open && id) load(id)
  })

  async function load(id) {
    loading = true
    error = ''
    form = null
    pending = null
    coverError = ''
    coverStatus = ''
    picker = {open: false, loading: false, error: '', items: []}
    try {
      if (!coverBase) {
        try { coverBase = await GetCoverBaseURL() } catch { /* remote fallback still works */ }
      }
      const d = await GetGameDetail(id)
      cover = {
        hasCover: !!d.hasCover,
        coverUrl: d.coverUrl || '',
        coverW: d.coverW || 0,
        coverH: d.coverH || 0,
        coverSource: d.coverSource || '',
      }
      coverUrlInput = d.coverUrl || ''
      form = {
        title: d.title || '',
        developer: d.developer || '',
        overview: d.overview || '',
        engine: d.engine || '',
        version: d.version || '',
        exePath: d.exePath || '',
        winePrefix: d.winePrefix || '',
        status: d.status || 'unknown',
        notes: d.notes || '',
        tags: (d.tags || []).join(', '),
        f95Url: d.f95Url || '',
        storeLinks: Object.entries(d.storeLinks || {}).map(([store, url]) => ({store, url})),
      }
    } catch (e) {
      error = fmtErr(e)
    } finally {
      loading = false
    }
  }

  // ── Cover handlers ─────────────────────────────────────────
  function stageCoverUrl() {
    const url = coverUrlInput.trim()
    if (!url) {
      coverError = 'Enter an image URL first.'
      return
    }
    if (!/^https?:\/\//i.test(url)) {
      coverError = 'Cover URL must start with http:// or https://'
      return
    }
    pending = {kind: 'url', url, source: 'manual', preview: url}
    coverError = ''
    coverStatus = 'Cover URL will be applied when you save.'
  }

  async function chooseCoverFile() {
    coverError = ''
    coverBusy = true
    try {
      const path = await PickCoverImage()
      if (!path) return
      let preview = ''
      try {
        preview = await PreviewCoverFile(path)
      } catch (e) {
        // Preview is best-effort: still stage the file, the backend validates
        // it for real on save.
        coverError = fmtErr(e)
      }
      pending = {kind: 'file', file: path, preview}
      coverStatus = 'Local image will be applied when you save.'
    } catch (e) {
      coverError = fmtErr(e)
    } finally {
      coverBusy = false
    }
  }

  async function toggleSearch() {
    if (picker.open) {
      picker = {...picker, open: false}
      return
    }
    picker = {open: true, loading: true, error: '', items: []}
    coverError = ''
    try {
      const items = await FindCoverCandidates(Number(editGameState.gameId))
      picker = {open: true, loading: false, error: '', items: items || []}
    } catch (e) {
      picker = {open: true, loading: false, error: fmtErr(e), items: []}
    }
  }

  function chooseCandidate(c) {
    pending = {kind: 'url', url: c.url, source: c.source, preview: c.thumb || c.url}
    coverUrlInput = c.url
    picker = {...picker, open: false}
    coverError = ''
    coverStatus = `Cover from ${c.source} will be applied when you save.`
  }

  function clearPendingCover() {
    pending = null
    coverError = ''
    coverStatus = ''
    coverUrlInput = cover?.coverUrl || ''
  }

  function parseTags(s) {
    return s.split(',').map(t => t.trim()).filter(Boolean)
  }

  function storeLinksObject(rows) {
    const out = {}
    for (const r of rows) {
      const k = r.store.trim()
      const v = r.url.trim()
      if (k && v) out[k] = v
    }
    return out
  }

  function addStoreRow() {
    form.storeLinks = [...form.storeLinks, {store: '', url: ''}]
  }
  function removeStoreRow(i) {
    form.storeLinks = form.storeLinks.filter((_, j) => j !== i)
  }

  async function save(e) {
    e?.preventDefault()
    if (!form || saving) return
    const title = form.title.trim()
    if (!title) {
      error = 'Title must not be empty.'
      return
    }
    saving = true
    error = ''
    try {
      // Apply a staged cover change first: it is the network/IO-heavy step, so
      // a bad URL or unreadable file surfaces before any field is written.
      if (pending?.kind === 'url') {
        await SetGameCover(editGameState.gameId, pending.url, pending.source || 'manual')
      } else if (pending?.kind === 'file') {
        await SetGameCoverFromFile(editGameState.gameId, pending.file)
      }
      await EditGame(editGameState.gameId, {
        title,
        developer: form.developer.trim(),
        overview: form.overview,
        engine: form.engine,
        version: form.version.trim(),
        exePath: form.exePath.trim(),
        winePrefix: form.winePrefix.trim(),
        status: form.status,
        notes: form.notes,
        tags: parseTags(form.tags),
        f95Url: form.f95Url.trim(),
        storeLinks: storeLinksObject(form.storeLinks),
      })
      settleEditGame(true)
    } catch (err) {
      error = fmtErr(err)
    } finally {
      saving = false
    }
  }

  function onOpenChange(next) {
    if (!next) settleEditGame(false)
  }
</script>

<Dialog.Root open={editGameState.open} {onOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="dialog-overlay" />
    <Dialog.Content class="dialog-content" style="max-width: 660px; max-height: 88vh; overflow-y: auto;">
      <Dialog.Title class="dialog-title">Edit Game</Dialog.Title>

      {#if loading}
        <p class="edit-loading">Loading…</p>
      {:else if form}
        <form class="edit-form" onsubmit={save}>
          <div class="field-grid">
            <label class="field span-2">
              <span class="field-label">Title</span>
              <!-- svelte-ignore a11y_autofocus -->
              <input class="field-input" type="text" bind:value={form.title} autofocus />
            </label>

            <div class="field span-2 cover-edit">
              <span class="field-label">Cover art</span>
              <div class="cover-edit-body">
                <div class="cover-preview">
                  {#if coverPreview}
                    <img src={coverPreview} alt="Cover preview" />
                  {:else}
                    <span class="cover-preview-empty">No cover</span>
                  {/if}
                </div>
                <div class="cover-edit-controls">
                  <div class="cover-url-row">
                    <input
                      class="field-input mono"
                      type="text"
                      aria-label="Cover image URL"
                      placeholder="https://…/cover.jpg"
                      bind:value={coverUrlInput}
                      onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); stageCoverUrl() } }}
                    />
                    <button type="button" class="cover-btn" onclick={stageCoverUrl} disabled={coverBusy}>Use URL</button>
                  </div>
                  <div class="cover-btn-row">
                    <button type="button" class="cover-btn" onclick={chooseCoverFile} disabled={coverBusy}>
                      {coverBusy ? 'Choosing…' : 'Choose file…'}
                    </button>
                    <button type="button" class="cover-btn" onclick={toggleSearch} disabled={coverBusy || picker.loading}>
                      {picker.loading ? 'Searching…' : (picker.open ? 'Hide search' : 'Search online…')}
                    </button>
                    {#if pending}
                      <button type="button" class="cover-btn" onclick={clearPendingCover} disabled={coverBusy}>Undo change</button>
                    {/if}
                  </div>
                  {#if coverStatus}<p class="cover-hint">{coverStatus}</p>{/if}
                  {#if coverError}<p class="cover-hint cover-hint-err">{coverError}</p>{/if}
                  {#if picker.open}
                    <div class="cover-picker">
                      <div class="cover-picker-head">
                        <span>Cover candidates</span>
                        <button type="button" class="cover-btn" onclick={() => picker = {...picker, open: false}}>Close</button>
                      </div>
                      {#if picker.loading}
                        <p class="cover-hint">Searching…</p>
                      {:else if picker.error}
                        <p class="cover-hint cover-hint-err">{picker.error}</p>
                      {:else if picker.items.length === 0}
                        <p class="cover-hint">No exact title match on the enabled sources (Settings → Cover art).</p>
                      {/if}
                      <div class="cover-picker-grid">
                        {#each picker.items as c (c.url)}
                          <button
                            type="button"
                            class="cand"
                            class:cand-active={pending?.kind === 'url' && pending.url === c.url}
                            onclick={() => chooseCandidate(c)}
                            title={c.note || c.source}
                          >
                            <img src={c.thumb || c.url} alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer" />
                            <span class="cand-meta">{c.source} · {c.w}×{c.h}</span>
                          </button>
                        {/each}
                      </div>
                    </div>
                  {/if}
                </div>
              </div>
            </div>

            <label class="field">
              <span class="field-label">Developer</span>
              <input class="field-input" type="text" bind:value={form.developer} />
            </label>

            <label class="field">
              <span class="field-label">Status</span>
              <select class="field-input" bind:value={form.status}>
                {#each GAME_STATUSES as s}
                  <option value={s}>{statusLabel(s)}</option>
                {/each}
              </select>
            </label>

            <label class="field span-2">
              <span class="field-label">Overview</span>
              <textarea class="field-input" rows="4" bind:value={form.overview}></textarea>
            </label>

            <label class="field">
              <span class="field-label">Engine</span>
              <span class="engine-row">
                <span class="engine-badge" style="--ec: {engineColor(form.engine)}">{form.engine || 'Unknown'}</span>
                <select class="field-input" bind:value={form.engine}>
                  <option value="">— None —</option>
                  {#each engineChoices as eng}
                    <option value={eng}>{eng}</option>
                  {/each}
                </select>
              </span>
            </label>

            <label class="field">
              <span class="field-label">Version</span>
              <input class="field-input" type="text" bind:value={form.version} />
            </label>

            <label class="field span-2">
              <span class="field-label">Executable</span>
              <input class="field-input mono" type="text" placeholder="/path/to/game.exe" bind:value={form.exePath} />
            </label>

            <label class="field span-2">
              <span class="field-label">Wine prefix</span>
              <input class="field-input mono" type="text" placeholder="Leave empty for the system default" bind:value={form.winePrefix} />
            </label>

            <label class="field span-2">
              <span class="field-label">Tags <span class="hint">comma-separated</span></span>
              <input class="field-input" type="text" placeholder="romance, sandbox, 2d" bind:value={form.tags} />
            </label>

            <label class="field span-2">
              <span class="field-label">Notes</span>
              <textarea class="field-input" rows="3" bind:value={form.notes}></textarea>
            </label>

            <label class="field span-2">
              <span class="field-label">F95Zone URL</span>
              <input class="field-input mono" type="text" placeholder="https://f95zone.to/threads/…" bind:value={form.f95Url} />
            </label>

            <div class="field span-2">
              <span class="field-label">Store links</span>
              {#each form.storeLinks as row, i}
                <div class="store-row">
                  <input class="field-input store-key" type="text" placeholder="steam" bind:value={row.store} />
                  <input class="field-input mono" type="text" placeholder="https://…" bind:value={row.url} />
                  <button type="button" class="icon-btn" title="Remove" onclick={() => removeStoreRow(i)}>✕</button>
                </div>
              {/each}
              <button type="button" class="add-store" onclick={addStoreRow}>+ Add store</button>
            </div>
          </div>

          {#if error}
            <p class="edit-error">{error}</p>
          {/if}

          <div class="edit-actions">
            <button type="button" class="btn" onclick={() => settleEditGame(false)}>Cancel</button>
            <button type="submit" class="btn btn-primary" disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        </form>
      {:else if error}
        <p class="edit-error">{error}</p>
        <div class="edit-actions">
          <button type="button" class="btn" onclick={() => settleEditGame(false)}>Close</button>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  .edit-loading {
    margin: 8px 0 16px;
    color: var(--text-secondary);
  }

  .field-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    margin-bottom: 16px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .span-2 { grid-column: 1 / -1; }

  .field-label {
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .hint {
    font-weight: 400;
    text-transform: none;
    letter-spacing: 0;
    color: var(--text-muted);
  }

  .field-input {
    width: 100%;
    box-sizing: border-box;
    padding: 7px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-base);
    font-family: inherit;
    outline: none;
    resize: vertical;
  }
  .field-input:focus { border-color: var(--accent); }
  .field-input.mono { font-family: var(--font-mono); font-size: var(--text-sm); }
  textarea.field-input { resize: vertical; }

  .engine-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .engine-badge {
    flex-shrink: 0;
    padding: 2px 10px;
    border-radius: var(--radius-1);
    font-size: var(--text-sm);
    font-weight: 600;
    background: color-mix(in srgb, var(--ec) 15%, transparent);
    color: var(--ec);
    white-space: nowrap;
  }

  .store-row {
    display: flex;
    gap: 6px;
    margin-bottom: 6px;
  }
  .store-key { flex: 0 0 140px; }
  .icon-btn {
    flex-shrink: 0;
    width: 30px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
  }
  .icon-btn:hover { color: var(--danger); border-color: var(--danger); }
  .add-store {
    align-self: flex-start;
    margin-top: 2px;
    padding: 4px 10px;
    border: 1px dashed var(--border);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .add-store:hover { color: var(--text-primary); border-color: var(--rule-strong); }

  /* ── Cover art editor ─────────────────────────── */
  .cover-edit-body {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .cover-preview {
    flex: 0 0 96px;
    width: 96px;
    aspect-ratio: 3 / 4;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-tertiary);
    overflow: hidden;
  }
  .cover-preview img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .cover-preview-empty { color: var(--text-muted); font-size: var(--text-xs); }
  .cover-edit-controls { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
  .cover-url-row { display: flex; gap: 6px; }
  .cover-url-row .field-input { flex: 1; min-width: 0; }
  .cover-btn-row { display: flex; flex-wrap: wrap; gap: 6px; }
  .cover-btn {
    padding: 4px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--text-sm);
    cursor: pointer;
    white-space: nowrap;
  }
  .cover-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
  .cover-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .cover-hint { margin: 0; font-size: var(--text-xs); color: var(--text-muted); }
  .cover-hint-err { color: var(--danger); }
  .cover-picker {
    margin-top: 4px;
    padding: 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-secondary);
  }
  .cover-picker-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 4px;
    font-size: var(--text-sm);
    color: var(--text-secondary);
  }
  .cover-picker-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
    gap: 6px;
    margin-top: 6px;
  }
  .cand {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-tertiary);
    cursor: pointer;
    text-align: left;
    overflow: hidden;
  }
  .cand:hover { border-color: var(--rule-strong); }
  .cand-active { border-color: var(--accent); }
  .cand img { width: 100%; aspect-ratio: 3 / 4; object-fit: cover; display: block; }
  .cand-meta {
    padding: 2px 4px;
    font-size: var(--text-2xs);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .edit-error {
    margin: 0 0 12px;
    padding: 8px 10px;
    border: 1px solid var(--danger);
    border-radius: var(--radius-1);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
    color: var(--danger);
    font-size: var(--text-base);
  }

  .edit-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .btn {
    padding: 7px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-base);
    cursor: pointer;
  }
  .btn:hover:not(:disabled) { background: var(--bg-hover); }
  .btn-primary {
    border-color: transparent;
    background: var(--accent);
    color: var(--on-accent);
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
