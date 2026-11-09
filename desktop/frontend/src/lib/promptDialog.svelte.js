// Shared prompt-dialog service — a Promise-returning drop-in replacement
// for window.prompt(), backed by a single Dialog instance mounted once at
// the app root (see PromptDialog.svelte). Resolves the trimmed input string
// on submit, or null on cancel — matching window.prompt's null-on-cancel
// contract so call sites need no control-flow changes beyond the await.

export const promptState = $state({
  open: false,
  title: '',
  label: '',
  value: '',
})

let resolver = null

export function promptAction({title, label = '', defaultValue = ''}) {
  return new Promise((resolve) => {
    if (resolver) resolver(null)
    resolver = resolve
    promptState.title = title
    promptState.label = label
    promptState.value = defaultValue
    promptState.open = true
  })
}

export function settlePrompt(value) {
  promptState.open = false
  const r = resolver
  resolver = null
  if (r) r(value)
}
