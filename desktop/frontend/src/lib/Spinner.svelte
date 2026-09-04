<script>
  // Shared loading-spinner primitive — consolidates the .spinner CSS/markup
  // copy-pasted across DedupDialog, SyncDialog, and other views (each with
  // slightly different sizes/speeds, e.g. 24px/0.8s vs 16px/0.6s).
  let { size = 24, duration = 0.8 } = $props()
</script>

<div
  class="spinner"
  style="--spinner-size: {size}px; --spinner-duration: {duration}s"
></div>

<style>
  .spinner {
    width: var(--spinner-size);
    height: var(--spinner-size);
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin var(--spinner-duration) linear infinite;
    flex-shrink: 0;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
