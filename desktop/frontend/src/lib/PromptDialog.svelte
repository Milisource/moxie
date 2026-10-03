<script>
  // Single app-root Dialog instance backing promptDialog.svelte.js's
  // promptAction() — replaces window.prompt() with a focus-trapped,
  // aria-modal, Escape-to-close dialog. Resolves the trimmed value on
  // submit, or null on cancel/dismiss, matching window.prompt's contract.
  import {Dialog} from 'bits-ui'
  import Button from './Button.svelte'
  import {promptState, settlePrompt} from './promptDialog.svelte.js'

  let inputEl = $state(null)

  function onOpenChange(next) {
    if (!next) settlePrompt(null)
  }

  function submit() {
    const v = promptState.value.trim()
    settlePrompt(v === '' ? null : v)
  }

  function onSubmit(e) {
    e.preventDefault()
    submit()
  }

  // Focus + select the input's existing value once it mounts open, matching
  // window.prompt's behavior of pre-filling and selecting the default text.
  function focusInput(node) {
    node.focus()
    node.select()
  }
</script>

<Dialog.Root open={promptState.open} {onOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="dialog-overlay" />
    <Dialog.Content class="dialog-content">
      <Dialog.Title class="dialog-title">{promptState.title}</Dialog.Title>
      <form onsubmit={onSubmit}>
        {#if promptState.label}
          <label class="prompt-label" for="prompt-dialog-input">{promptState.label}</label>
        {/if}
        <input
          id="prompt-dialog-input"
          class="prompt-input"
          type="text"
          bind:value={promptState.value}
          bind:this={inputEl}
          use:focusInput
        />
        <div class="dialog-actions">
          <Dialog.Close>
            {#snippet child({props})}
              <Button {...props} type="button">Cancel</Button>
            {/snippet}
          </Dialog.Close>
          <Button type="submit" variant="primary">Save</Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  .prompt-label {
    display: block;
    margin-bottom: 6px;
    font-size: var(--text-sm);
    color: var(--text-secondary);
  }
  .prompt-input {
    width: 100%;
    box-sizing: border-box;
    padding: 8px 10px;
    margin-bottom: 20px;
    border: 1px solid var(--border);
    border-radius: var(--radius-1);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--text-base);
  }
  .prompt-input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
