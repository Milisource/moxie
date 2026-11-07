<script>
  // Reference sheet for the keyboard model. The bindings listed here are the
  // ones actually implemented: App's global "/" and Ctrl/⌘+F handler, the
  // roving-focus arrow keys in useLibraryNav, Enter on a focused card, and
  // the platform context-menu key. Nothing aspirational gets listed.
  import {Dialog} from 'bits-ui'

  let {open = $bindable(false)} = $props()

  const groups = [
    {
      title: 'Search and views',
      rows: [
        {keys: ['/', 'Ctrl/⌘ F'], label: 'Focus the library search'},
        {keys: ['?'], label: 'Open this shortcut sheet'},
        {keys: ['F11'], label: 'Toggle couch mode (fullscreen)'},
        {keys: ['Esc'], label: 'Close a dialog or menu'},
      ],
    },
    {
      title: 'Library',
      rows: [
        {keys: ['↑', '↓', '←', '→'], label: 'Move between games'},
        {keys: ['Enter'], label: 'Open the focused game'},
        {keys: ['Shift', 'F10'], label: 'Open the game context menu'},
        {keys: ['Tab'], label: 'Leave the library for the sidebar'},
      ],
    },
  ]
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="dialog-overlay" />
    <Dialog.Content class="dialog-content shortcuts" style="max-width: 560px; max-height: 84vh; overflow-y: auto;">
      <div class="shortcuts-head">
        <Dialog.Title class="dialog-title">Keyboard shortcuts</Dialog.Title>
        <Dialog.Close class="shortcuts-close" aria-label="Close">✕</Dialog.Close>
      </div>

      {#each groups as group (group.title)}
        <section class="shortcut-group">
          <h3>{group.title}</h3>
          <dl>
            {#each group.rows as row (row.label + row.keys.join())}
              <div class="shortcut-row">
                <dt>{row.label}</dt>
                <dd>
                  {#each row.keys as key (key)}
                    <kbd>{key}</kbd>
                  {/each}
                </dd>
              </div>
            {/each}
          </dl>
        </section>
      {/each}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  .shortcuts-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 4px;
  }
  :global(.shortcuts-head .dialog-title) { margin: 0; }
  /* `class` reaches a bits-ui component, so Svelte cannot attach its scope
     hash — target it globally. */
  :global(.shortcuts-close) {
    flex-shrink: 0;
    width: 26px;
    height: 26px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  :global(.shortcuts-close):hover { background: var(--bg-hover); color: var(--text-primary); }

  .shortcut-group { margin-top: 18px; }
  .shortcut-group h3 {
    margin: 0 0 8px;
    font-family: var(--font-display);
    font-size: var(--text-2xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-label);
    color: var(--text-muted);
  }
  dl { margin: 0; }
  .shortcut-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 7px 0;
    border-bottom: 1px solid var(--border);
  }
  .shortcut-row:last-child { border-bottom: none; }
  dt { font-size: var(--text-base); color: var(--text-secondary); }
  dd { margin: 0; display: flex; flex-wrap: wrap; gap: 4px; justify-content: flex-end; }
  kbd {
    min-width: 22px;
    padding: 2px 6px;
    border: 1px solid var(--rule-strong);
    background: var(--bg-tertiary);
    color: var(--text-primary);
    font-family: var(--font-mono);
    font-size: var(--text-2xs);
    text-align: center;
  }
</style>
