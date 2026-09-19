/**
 * Thin wrapper around @tanstack/svelte-virtual's subscribe-by-hand pattern —
 * was previously duplicated in full (grid + table) inside GameList.svelte.
 *
 * NOTE on why this can't just be `$store`: `setOptions()` unconditionally
 * force-emits a new store value on every call (see the library's source — it
 * re-sets the writable even when the visible range didn't change, so
 * count-only updates still notify). Reading the store reactively (`$store`)
 * from *inside* the same $effect that calls `.setOptions()` would make that
 * effect depend on its own output and spin forever
 * (`effect_update_depth_exceeded`). So: `.setOptions()`/`.measure()` are
 * called on a plain (non-reactive) reference to the instance, and a
 * hand-rolled `.subscribe()` mirrors the current virtual items/total size
 * into actual `$state` for callers to read.
 */
import {createVirtualizer} from '@tanstack/svelte-virtual'

/**
 * @param {object} initial - initial virtualizer options (count/getScrollElement/estimateSize/overscan)
 * @returns {{virtualRows: any[], totalSize: number, setOptions: (opts: object) => void, scrollToIndex: (index: number, opts?: object) => void, unsubscribe: () => void}}
 */
export function createVirtualList(initial) {
  let api                        // plain ref — same singleton instance every emit
  let virtualRows = $state.raw([])
  let totalSize = $state(0)

  const unsubscribe = createVirtualizer(initial).subscribe(v => {
    api = v
    virtualRows = v.getVirtualItems()
    totalSize = v.getTotalSize()
  })

  // Call from an $effect with freshly computed options whenever any input
  // (scroll element, row count, estimated size, item keys) changes.
  function setOptions(opts) {
    api?.setOptions(opts)
    api?.measure()
  }

  function scrollToIndex(index, opts) {
    api?.scrollToIndex(index, opts)
  }

  return {
    get virtualRows() { return virtualRows },
    get totalSize() { return totalSize },
    setOptions,
    scrollToIndex,
    unsubscribe,
  }
}
