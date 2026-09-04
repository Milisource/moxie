<script>
  // Shared button primitive — consolidates the .btn/.btn-primary/.btn-danger/
  // .btn-outline/.btn-sm/.btn-xs CSS that used to be copy-pasted per-view
  // (DownloadsView, GameUpdatesView, F95Browser, SyncDialog, UpdateDialog,
  // AddGameDialog, DedupDialog all defined near-identical versions of this).
  //
  // variant: 'outline' (default, bordered/transparent) | 'primary' (filled
  //   accent) | 'danger' (transparent, danger-colored text/border — covers
  //   both the old .btn-danger and .btn-outline.btn-cancel combo, which
  //   rendered identically)
  // size: 'md' (default) | 'sm' | 'xs'
  let {
    variant = 'outline',
    size = 'md',
    disabled = false,
    title = undefined,
    type = 'button',
    onclick = () => {},
    children,
    ...rest
  } = $props()
</script>

<button
  class="btn btn-{variant} btn-{size}"
  {type}
  {disabled}
  {title}
  {onclick}
  {...rest}
>
  {@render children?.()}
</button>

<style>
  .btn {
    padding: 7px 16px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--text-base);
    cursor: pointer;
    transition: background 0.12s;
    white-space: nowrap;
  }
  .btn:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .btn-sm {
    padding: 4px 10px;
    font-size: var(--text-sm);
  }
  .btn-xs {
    padding: 2px 8px;
    font-size: var(--text-xs);
    border-radius: 4px;
  }

  .btn-primary {
    background: var(--accent);
    color: #fff;
    border-color: var(--accent);
  }
  .btn-primary:hover:not(:disabled) {
    background: var(--accent-hover);
  }

  .btn-danger {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 30%, transparent);
  }
  .btn-danger:hover:not(:disabled) {
    background: color-mix(in srgb, var(--danger) 10%, transparent);
  }
</style>
