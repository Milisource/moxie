<script>
  import {onMount} from 'svelte'
  import {
    GetScanPaths,
    AddScanPath,
    RemoveScanPath,
    PickDirectory,
  } from '../../wailsjs/go/main/App'

  // onScan: when supplied, each path gets a Scan button. Settings omits it and
  // shows path management only.
  let {
    onScan = null,
    scanning = false,
    currentPath = '',
    onPathsChanged = () => {},
  } = $props()

  let scanPaths = $state([])
  let newPath = $state('')
  let error = $state('')

  async function loadPaths() {
    try {
      scanPaths = (await GetScanPaths()) || []
      error = ''
    } catch (e) {
      error = `Failed to load scan paths: ${e}`
    }
  }

  async function handleAdd() {
    const path = newPath.trim()
    if (!path) return
    try {
      await AddScanPath(path)
      newPath = ''
      await loadPaths()
      onPathsChanged()
    } catch (e) {
      error = `Failed to add path: ${e}`
    }
  }

  async function handleBrowse() {
    try {
      const dir = await PickDirectory()
      if (!dir) return
      await AddScanPath(dir)
      await loadPaths()
      onPathsChanged()
    } catch (e) {
      error = `Failed to add path: ${e}`
    }
  }

  async function handleRemove(path) {
    try {
      await RemoveScanPath(path)
      await loadPaths()
      onPathsChanged()
    } catch (e) {
      error = `Failed to remove path: ${e}`
    }
  }

  onMount(loadPaths)
</script>

<div class="scan-paths">
  <div class="add-path">
    <input
      type="text"
      class="path-input"
      placeholder="/path/to/games"
      bind:value={newPath}
      onkeydown={(e) => e.key === 'Enter' && handleAdd()}
    />
    <button class="btn btn-outline" onclick={handleBrowse}>Browse…</button>
    <button class="btn btn-primary" onclick={handleAdd} disabled={!newPath.trim()}>
      Add Path
    </button>
  </div>

  {#if error}
    <p class="path-error">{error}</p>
  {/if}

  {#if scanPaths.length > 0}
    <div class="paths-list">
      {#each scanPaths as path}
        <div class="path-row">
          <span class="path-label" title={path}>{path}</span>
          <div class="path-actions">
            {#if onScan}
              <button
                class="btn btn-scan"
                onclick={() => onScan(path)}
                disabled={scanning}
              >
                {scanning && currentPath === path ? 'Scanning…' : 'Scan'}
              </button>
            {/if}
            <button
              class="btn btn-remove"
              onclick={() => handleRemove(path)}
              disabled={scanning}
            >
              ✕
            </button>
          </div>
        </div>
      {/each}
    </div>
  {:else}
    <div class="empty-paths">
      <p>No scan paths configured. Add a directory above to get started.</p>
    </div>
  {/if}
</div>

<style>
  .add-path {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
  }

  .path-input {
    flex: 1;
    padding: 8px 12px;
    font-size: 13px;
    font-family: var(--font-mono);
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
  }
  .path-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .btn {
    padding: 8px 14px;
    font-size: 13px;
    border-radius: 6px;
    border: 1px solid transparent;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .btn-primary {
    background: var(--accent);
    color: #fff;
  }
  .btn-primary:hover:not(:disabled) {
    background: var(--accent-hover);
  }
  .btn-outline {
    background: transparent;
    color: var(--text-primary);
    border-color: var(--border);
  }
  .btn-outline:hover:not(:disabled) {
    background: var(--bg-hover);
  }

  .paths-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .path-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 6px;
  }

  .path-label {
    flex: 1;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .path-actions {
    display: flex;
    gap: 6px;
  }

  .btn-scan {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--accent);
    padding: 4px 12px;
    font-size: 12px;
  }
  .btn-scan:hover:not(:disabled) { background: var(--accent); color: #fff; }

  .btn-remove {
    background: transparent;
    color: var(--text-muted);
    padding: 4px 8px;
    font-size: 12px;
  }
  .btn-remove:hover:not(:disabled) {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }

  .empty-paths {
    padding: 20px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
    border: 1px dashed var(--border);
    border-radius: 6px;
  }

  .path-error {
    margin: 0 0 10px;
    font-size: 12px;
    color: var(--danger);
  }
</style>
