<script>
  // One consistent 24-grid line-icon set for the sidebar. emoji/Unicode glyphs
  // were removed earlier because they rendered differently per platform; these
  // are inline SVG with a fixed stroke so they stay identical everywhere.
  let {name = '', size = 18} = $props()

  const paths = {
    library: 'M4 4H11V20H4Z M13 4H20V20H13Z',
    browse: 'M12 3A9 9 0 1 0 12 21A9 9 0 1 0 12 3 M3 12H21 M12 3C15.5 6.5 15.5 17.5 12 21 M12 3C8.5 6.5 8.5 17.5 12 21',
    add: 'M12 5V19 M5 12H19',
    updates: 'M3 17L9 11L13 15L21 7 M15 7H21V13',
    downloads: 'M12 3V14 M7 9L12 14L17 9 M5 20H19',
    collections: 'M12 3L21 8L12 13L3 8Z M3 12L12 17L21 12 M3 16L12 21L21 16',
    saves: 'M6 3H18V21L12 17L6 21Z',
    scan: 'M4 9V4H9 M20 9V4H15 M4 15V20H9 M20 15V20H15',
    sync: 'M20 12C20 7.6 16.4 4 12 4C9.6 4 7.4 5 5.8 6.6 M4 12C4 16.4 7.6 20 12 20C14.4 20 16.6 19 18.2 17.4 M18 4V8H14 M6 20V16H10',
    covers: 'M3 5H21V19H3Z M3 16L8 11L11 14L15 10L21 16',
    duplicates: 'M8 8H20V20H8Z M4 4H16V16',
    trash: 'M4 7H20 M9 7V4H15V7 M6 7L7 21H17L18 7',
    settings: 'M12 9A3 3 0 1 0 12 15A3 3 0 1 0 12 9 M12 2V5 M12 19V22 M2 12H5 M19 12H22 M5 5L7 7 M17 17L19 19 M19 5L17 7 M7 17L5 19',
  }
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.75"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  <path d={paths[name] || ''} />
</svg>
