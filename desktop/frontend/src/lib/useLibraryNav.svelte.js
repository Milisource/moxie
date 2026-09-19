/**
 * Roving-tabindex keyboard navigation for the library grid/table — extracted
 * out of GameList.svelte, which mixed this with rendering and sort/filter
 * logic.
 *
 * Every card/row was individually tabindex="0" — fine for a dozen games, a
 * tab-trap for a real library (Tab would walk all 400+ before reaching the
 * next sidebar item). This is the Steam/Playnite pattern instead: exactly
 * one card/row is a Tab stop at a time; arrow keys move it and drive focus,
 * so Tab in/out of the library costs one stop either way. `rovingId` falls
 * back to the first visible item whenever the previously-focused id drops
 * out of the displayed list (filter/sort changed under it) so a stop is
 * always reachable.
 *
 * Callers own the grid/table DOM, virtualizer scrolling, and context menu
 * state (all component-local), so those are supplied as getters/callbacks
 * rather than this module reaching into component internals directly.
 */
import {tick} from 'svelte'

/**
 * @param {object} opts
 * @param {() => any[]} opts.getDisplayed - current filtered+sorted game list
 * @param {() => 'grid'|'list'} opts.getViewMode
 * @param {() => number} opts.getGridColumns
 * @param {() => HTMLElement|null} opts.getContainer - grid or table scroll container, per current view mode
 * @param {(index: number) => void} opts.scrollToIndex - scroll the active virtualizer to a flat display index
 * @param {(rect: DOMRect, game: object) => void} opts.onOpenContextMenu - keyboard equivalent of right-click
 */
export function createLibraryNav({
  getDisplayed,
  getViewMode,
  getGridColumns,
  getContainer,
  scrollToIndex,
  onOpenContextMenu,
}) {
  let focusedId = $state(null)
  let rovingId = $derived.by(() => {
    const displayed = getDisplayed()
    if (focusedId != null && displayed.some(g => g.id === focusedId)) return focusedId
    return displayed[0]?.id ?? null
  })

  function clampIndex(i, len) {
    return Math.max(0, Math.min(len - 1, i))
  }

  // Rows only exist in the DOM within the virtualizer's window, so moving
  // focus onto an off-screen target has to scroll it into view first, wait
  // a tick for it to actually mount, then focus the real element.
  async function focusGameEl(id) {
    await tick()
    getContainer()?.querySelector(`[data-game-id="${id}"]`)?.focus()
  }

  async function moveFocus(delta) {
    const displayed = getDisplayed()
    if (displayed.length === 0) return
    const curIdx = focusedId != null ? displayed.findIndex(g => g.id === focusedId) : -1
    const nextIdx = clampIndex((curIdx === -1 ? 0 : curIdx) + delta, displayed.length)
    const g = displayed[nextIdx]
    if (!g) return
    focusedId = g.id
    scrollToIndex(nextIdx)
    await focusGameEl(g.id)
  }

  // Right-click already opens the context menu (positioned at the mouse);
  // the keyboard equivalent (Menu key / Shift+F10, same as Windows Explorer)
  // positions it against the focused card/row's own bounding box instead.
  function openContextMenuFromKeyboard(e) {
    const el = e.target?.closest?.('[data-game-id]')
    const displayed = getDisplayed()
    const game = el && displayed.find(g => String(g.id) === el.dataset.gameId)
    if (!game) return
    onOpenContextMenu(el.getBoundingClientRect(), game)
  }

  function onNavKeydown(e) {
    if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
      e.preventDefault()
      openContextMenuFromKeyboard(e)
      return
    }
    const cols = getViewMode() === 'grid' ? getGridColumns() : 1
    switch (e.key) {
      case 'ArrowRight':
        if (getViewMode() !== 'grid') return
        e.preventDefault(); moveFocus(1); break
      case 'ArrowLeft':
        if (getViewMode() !== 'grid') return
        e.preventDefault(); moveFocus(-1); break
      case 'ArrowDown': e.preventDefault(); moveFocus(cols); break
      case 'ArrowUp':   e.preventDefault(); moveFocus(-cols); break
    }
  }

  return {
    get rovingId() { return rovingId },
    setFocused: (id) => { focusedId = id },
    onNavKeydown,
  }
}
