<script>
  // Single app-root AlertDialog instance backing confirmDialog.svelte.js's
  // confirmAction() — replaces window.confirm() with a focus-trapped,
  // aria-modal, Escape-to-close dialog while keeping every call site's
  // control flow identical (await confirmAction(...) resolves true/false
  // exactly like window.confirm did).
  import {AlertDialog} from 'bits-ui'
  import Button from './Button.svelte'
  import {confirmState, settleConfirm} from './confirmDialog.svelte.js'

  function onOpenChange(next) {
    if (!next) settleConfirm(false)
  }
</script>

<AlertDialog.Root open={confirmState.open} {onOpenChange}>
  <AlertDialog.Portal>
    <AlertDialog.Overlay class="dialog-overlay" />
    <AlertDialog.Content class="dialog-content">
      <AlertDialog.Title class="dialog-title">{confirmState.title}</AlertDialog.Title>
      {#if confirmState.description}
        <AlertDialog.Description class="dialog-description">{confirmState.description}</AlertDialog.Description>
      {/if}
      <div class="dialog-actions">
        <AlertDialog.Cancel>
          {#snippet child({props})}
            <Button {...props}>Cancel</Button>
          {/snippet}
        </AlertDialog.Cancel>
        <AlertDialog.Action>
          {#snippet child({props})}
            <Button {...props} variant={confirmState.danger ? 'danger' : 'primary'} onclick={() => settleConfirm(true)}>
              {confirmState.confirmLabel}
            </Button>
          {/snippet}
        </AlertDialog.Action>
      </div>
    </AlertDialog.Content>
  </AlertDialog.Portal>
</AlertDialog.Root>

<style>
  /* .dialog-overlay/.dialog-content/.dialog-title/.dialog-description are
     shared chrome defined once in app.css (also used by PromptDialog). */
  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
