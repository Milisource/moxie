<script>
  import {PickDirectory, DetectGame, AddGame} from '../../wailsjs/go/main/App'
  import {engineColor} from './engineColors.js'
  import {initials} from './initials.js'

  let {
    onGameAdded = () => {},
    onNavigate = () => {},
  } = $props()

  const commonEngines = [
    "ren'py", "unity", "rpg maker", "rpgmakermv", "rpgmakermz",
    "html", "wolf rpg", "flash", "unreal", "godot", "electron", "nw.js",
  ]
  const ENGINE_LABELS = {
    "ren'py": "Ren'Py", unity: "Unity", "rpg maker": "RPG Maker",
    rpgmakermv: "RPG Maker MV", rpgmakermz: "RPG Maker MZ", html: "HTML",
    "wolf rpg": "Wolf RPG", flash: "Flash", unreal: "Unreal Engine",
    godot: "Godot", electron: "Electron", "nw.js": "NW.js",
  }
  const engineLabel = (e) => ENGINE_LABELS[e] || e

  let directoryPath = $state('')
  let detecting = $state(false)
  let detection = $state(null)          // DetectGame result or null
  let title = $state('')
  let engine = $state('')
  let version = $state('')
  let adding = $state(false)
  let result = $state(null)             // { success, gameId?, error? }
  let error = $state('')

  let canDetect = $derived(directoryPath.trim().length > 0)
  let hasDetection = $derived(detection !== null && !detecting)
  let canAdd = $derived(hasDetection && title.trim().length > 0 && !adding)

  // Invalidates any in-flight DetectGame so a stale response can't repopulate
  // the preview from a directory the user has since changed.
  let detectSeq = 0
  function resetDetection() {
    detectSeq++
    detecting = false
    detection = null
    title = ''
    engine = ''
    version = ''
    result = null
    error = ''
  }

  async function handleBrowse() {
    try {
      const dir = await PickDirectory()
      if (dir) {
        directoryPath = dir
        resetDetection()
      }
    } catch (e) {
      error = String(e)
    }
  }

  async function handleDetect() {
    const path = directoryPath.trim()
    if (!path) return

    const seq = ++detectSeq
    detecting = true
    detection = null
    result = null
    error = ''

    try {
      const d = await DetectGame(path)
      if (seq !== detectSeq) return
      detection = d
      title = d.title || ''
      engine = d.engine || ''
      version = d.version || ''
      if (d.error) error = d.error
    } catch (e) {
      if (seq !== detectSeq) return
      error = String(e)
    } finally {
      if (seq === detectSeq) detecting = false
    }
  }

  async function handleAdd() {
    if (!canAdd) return

    adding = true
    result = null
    error = ''

    try {
      const gameId = await AddGame(directoryPath.trim(), title.trim(), engine.trim(), version.trim())
      result = {success: true, gameId, title: title.trim()}
      detection = null
      title = ''
      engine = ''
      version = ''
      onGameAdded()
    } catch (e) {
      result = {success: false, error: String(e)}
    } finally {
      adding = false
    }
  }

  function handlePathInput() {
    resetDetection()
  }
</script>

<div class="add-game-dialog">
  <div class="add-game-header">
    <h2>Add game</h2>
    <p class="add-game-subtitle">Point Moxie at a folder. It detects the engine and adds the title to your library.</p>
  </div>

  <div class="add-game-body">
    <div class="add-form">
      <div class="input-card">
        <span class="field-label">Game folder</span>
        <div class="dir-input-row">
          <input
            type="text"
            class="path-input"
            placeholder="/path/to/game/directory"
            bind:value={directoryPath}
            oninput={handlePathInput}
            onkeydown={(e) => e.key === 'Enter' && canDetect && handleDetect()}
          />
          <button class="btn btn-outline" onclick={handleBrowse}>Browse</button>
        </div>
        <div class="detect-row">
          <button class="btn btn-outline" onclick={handleDetect} disabled={!canDetect || detecting}>
            {#if detecting}<span class="spinner"></span>Detecting…{:else}Detect engine{/if}
          </button>
          <span class="detect-hint">Reads the folder and fills title, engine and version.</span>
        </div>
      </div>

      {#if detecting}
        <div class="detecting-section">
          <div class="spinner"></div>
          <p>Scanning directory and detecting engine…</p>
        </div>
      {/if}

      {#if hasDetection}
        <div class="preview-section">
          <div class="preview-head">
            <div class="preview-cover" aria-hidden="true">{initials(title || 'Game')}</div>
            <div class="preview-meta">
              <h3>Detected game</h3>
              <p>Review the title and engine, then add it to the library.</p>
              <span class="engine-badge" style="--ec: {engineColor(engine)}">
                <span class="engine-dot"></span>{engine || 'Unknown engine'}
              </span>
            </div>
          </div>

          <div class="preview-fields">
            <div class="field-group field-span">
              <label class="field-label" for="add-title">Title</label>
              <input id="add-title" type="text" class="field-input" placeholder="Game title" bind:value={title} />
            </div>

            <div class="field-group">
              <label class="field-label" for="add-engine">Engine</label>
              <select id="add-engine" class="field-select" bind:value={engine}>
                <option value="">— Custom —</option>
                {#if engine && !commonEngines.includes(engine)}
                  <option value={engine}>{engineLabel(engine)}</option>
                {/if}
                {#each commonEngines as eng}
                  <option value={eng}>{engineLabel(eng)}</option>
                {/each}
              </select>
            </div>

            <div class="field-group">
              <label class="field-label" for="add-version">Version</label>
              <input id="add-version" type="text" class="field-input" placeholder="e.g. 1.0" bind:value={version} />
            </div>

            {#if detection.sizeLabel}
              <div class="field-group">
                <span class="field-label">Size</span>
                <p class="field-readonly">{detection.sizeLabel}</p>
              </div>
            {/if}

            <div class="field-group field-span">
              <span class="field-label">Directory</span>
              <p class="field-readonly field-mono">{detection.path || directoryPath}</p>
            </div>
          </div>

          <div class="add-row">
            <button class="btn btn-primary btn-add" onclick={handleAdd} disabled={!canAdd}>
              {#if adding}<span class="spinner"></span>Adding…{:else}Add to library{/if}
            </button>
          </div>
        </div>
      {/if}

      {#if error && !result}
        <div class="error-section">
          <p class="error-title">Detection failed:</p>
          <p class="error-line">{error}</p>
        </div>
      {/if}

      {#if result}
        {#if result.success}
          <div class="result-section">
            <h3 class="result-title">Game added</h3>
            <p class="result-detail">"{result.title || title}" is now in your library.</p>
          </div>
        {:else}
          <div class="error-section">
            <p class="error-title">Failed to add game:</p>
            <p class="error-line">{result.error}</p>
          </div>
        {/if}
      {/if}

      <div class="bulk-card">
        <div>
          <h3>Adding a folder of games?</h3>
          <p>Scan a directory tree, detect every engine and add everything at once.</p>
        </div>
        <button class="btn btn-outline" onclick={() => onNavigate('scan')}>Open scan</button>
      </div>
    </div>

    <aside class="add-help">
      <section>
        <h3>What happens next</h3>
        <ol class="steps">
          <li><span class="step-num">1</span><span>Moxie reads the folder and identifies the engine.</span></li>
          <li><span class="step-num">2</span><span>It finds the executable and estimates the install size.</span></li>
          <li><span class="step-num">3</span><span>The game appears in Library; add metadata from F95Zone.</span></li>
        </ol>
      </section>

      <section>
        <h3>Supported engines</h3>
        <div class="engine-chips">
          {#each commonEngines as eng}
            <span class="engine-chip">{engineLabel(eng)}</span>
          {/each}
        </div>
        <p class="help-note">The folder must contain the game executable or a recognizable engine signature.</p>
      </section>
    </aside>
  </div>
</div>

<style>
  .add-game-dialog {
    flex: 1;
    overflow: auto;
    padding: var(--space-7) var(--gutter);
    width: 100%;
  }

  .add-game-header { margin-bottom: 20px; }
  .add-game-header h2 { font-size: var(--text-2xl); font-weight: 600; margin: 0 0 4px; }
  .add-game-subtitle { font-size: var(--text-sm); color: var(--text-secondary); margin: 0; }

  .add-game-body {
    display: grid;
    grid-template-columns: minmax(0, 640px) 360px;
    gap: 28px;
    align-items: start;
  }
  .add-form { min-width: 0; }

  .input-card, .preview-section, .add-help {
    border: 1px solid var(--border);
    background: var(--bg-secondary);
    padding: 18px;
  }
  .input-card { margin-bottom: 14px; }

  .field-label {
    display: block;
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--text-secondary);
    margin-bottom: 5px;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
  }

  .dir-input-row { display: flex; gap: 8px; }
  .path-input {
    flex: 1;
    min-width: 0;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    outline: none;
  }
  .path-input:focus { border-color: var(--accent); }

  .detect-row { display: flex; align-items: center; gap: 12px; margin-top: 12px; }
  .detect-hint { font-size: var(--text-xs); color: var(--text-muted); }

  .btn {
    padding: 7px 14px;
    border: none;
    border-radius: var(--radius-1);
    font-size: var(--text-sm);
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-outline { border: 1px solid var(--rule-strong); background: transparent; color: var(--text-primary); }
  .btn-outline:hover:not(:disabled) { background: var(--bg-hover); }
  .btn-primary { background: var(--accent); color: var(--on-accent); }
  .btn-primary:hover:not(:disabled) { background: var(--accent-hover); }

  .detecting-section {
    display: flex; align-items: center; gap: 10px;
    margin: 14px 0; padding: 16px;
    border: 1px solid var(--border); background: var(--bg-secondary);
    font-size: var(--text-sm); color: var(--text-secondary);
  }
  .spinner {
    width: 16px; height: 16px;
    border: 2px solid var(--border); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.6s linear infinite; flex-shrink: 0;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .preview-section { margin-top: 14px; }
  .preview-head { display: flex; gap: 16px; align-items: center; margin-bottom: 18px; }
  .preview-cover {
    width: 76px; height: 101px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    border: 1px solid var(--border);
    background: var(--bg-tertiary);
    color: var(--text-muted);
    font-family: var(--font-display);
    font-size: var(--text-2xl);
    font-weight: 600;
  }
  .preview-meta { min-width: 0; }
  .preview-meta h3 { margin: 0 0 4px; font-size: var(--text-lg); font-weight: 600; }
  .preview-meta p { margin: 0 0 8px; font-size: var(--text-xs); color: var(--text-muted); }
  .engine-badge {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 2px 7px;
    border: 1px solid var(--rule-strong);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .engine-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--ec); flex-shrink: 0; }

  .preview-fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px 16px;
  }
  .field-span { grid-column: 1 / -1; }
  .field-input, .field-select {
    width: 100%;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-sm);
    outline: none;
    box-sizing: border-box;
  }
  .field-select { padding-right: 28px; cursor: pointer; background-color: var(--bg-primary); }
  .field-input:focus, .field-select:focus { border-color: var(--accent); }
  .field-readonly { margin: 0; padding: 8px 0; font-size: var(--text-sm); color: var(--text-primary); }
  .field-mono { font-family: var(--font-mono); color: var(--text-secondary); font-size: var(--text-xs); overflow-wrap: anywhere; }

  .add-row { margin-top: 18px; padding-top: 16px; border-top: 1px solid var(--border); }
  .btn-add { width: 100%; padding: 10px 16px; display: inline-flex; align-items: center; justify-content: center; gap: 6px; }

  .add-help { display: flex; flex-direction: column; gap: 22px; }
  .add-help h3 { margin: 0 0 10px; font-size: var(--text-lg); font-weight: 600; }
  .steps { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 12px; }
  .steps li { display: grid; grid-template-columns: 24px minmax(0, 1fr); gap: 10px; align-items: start; font-size: var(--text-sm); color: var(--text-secondary); line-height: 1.5; }
  .step-num {
    width: 24px; height: 24px; display: inline-flex; align-items: center; justify-content: center;
    border-radius: 50%; background: var(--bg-tertiary); color: var(--accent);
    font-family: var(--font-mono); font-size: var(--text-xs);
  }
  .engine-chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .engine-chip {
    padding: 3px 7px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    white-space: nowrap;
  }
  .help-note { margin: 12px 0 0; font-size: var(--text-xs); color: var(--text-muted); line-height: 1.5; }

  .bulk-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    margin-top: 14px;
    padding: 16px 18px;
    border: 1px solid var(--border);
    background: var(--bg-secondary);
  }
  .bulk-card h3 { margin: 0 0 4px; font-size: var(--text-base); font-weight: 600; }
  .bulk-card p { margin: 0; font-size: var(--text-xs); color: var(--text-muted); }

  .result-section { margin-top: 14px; padding: 14px 16px; border: 1px solid var(--success); background: color-mix(in srgb, var(--success) 8%, transparent); }
  .result-title { margin: 0 0 4px; font-size: var(--text-base); font-weight: 600; color: var(--success); }
  .result-detail { margin: 0; font-size: var(--text-sm); color: var(--text-secondary); }

  .error-section { margin-top: 14px; padding: 14px 16px; border: 1px solid var(--danger); background: color-mix(in srgb, var(--danger) 8%, transparent); }
  .error-title { margin: 0 0 4px; font-size: var(--text-sm); font-weight: 600; color: var(--danger); }
  .error-line { margin: 0; font-size: var(--text-xs); color: var(--text-secondary); font-family: var(--font-mono); }

  @media (max-width: 1100px) {
    .add-game-body { grid-template-columns: minmax(0, 1fr); }
    .add-help { order: -1; }
  }
  @media (max-width: 640px) {
    .preview-fields { grid-template-columns: minmax(0, 1fr); }
    .field-span { grid-column: auto; }
  }
</style>
