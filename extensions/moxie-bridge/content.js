/*
 * moxie-bridge — reports F95Zone download destinations back to moxie.
 *
 * moxie opens a /dl/ check page with a fragment like
 *   #moxie=<port>:<token>
 * The fragment is never sent to F95Zone. This content script reads it, tells
 * the background worker which tab to watch, and the worker POSTs the URL the
 * tab lands on (after the human passes the check) to moxie's loopback
 * listener.
 */
(() => {
  const match = location.hash.match(/[#&]moxie=([0-9]+:[A-Fa-f0-9]+)/);
  if (!match) return;

  const hostEl = document.querySelector('.f95dl-link-host');
  chrome.runtime.sendMessage({
    type: 'moxie-dl',
    token: match[1],
    host: hostEl ? hostEl.textContent.trim() : '',
  });
})();
