<script>
  // Single app-root "Edit Game" dialog backing editGameDialog.svelte.js's
  // openEditGame(). One form edits every user-editable field in a single
  // EditGame call; the backend treats Title as a metadata-only change (it
  // never renames the directory — that stays on RenameGame).
  import {Dialog} from 'bits-ui'
  import {GetGameDetail, EditGame} from '../../wailsjs/go/main/App'
  import {engineColor, engineOptions} from './engineColors.js'
  import {GAME_STATUSES, statusLabel} from './statuses.js'
  import {editGameState, settleEditGame} from './editGameDialog.svelte.js'

  let loading = $state(false)
  let saving = $state(false)
  let error = $state('')
  let form = $state(null)

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
    try {
      const d = await GetGameDetail(id)
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
