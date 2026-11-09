/*
 * moxie-bridge background worker.
 *
 * Watches the tab a /dl/ check page was opened in and reports the host URL it
 * finally navigates to. Pending watches are mirrored to storage so a service
 * worker restart (MV3 idles after ~30s) does not lose a check the user is
 * still solving.
 */
const api = chrome;

const PENDING_KEY = 'moxiePending';
const MAX_AGE_MS = 15 * 60 * 1000;

async function getPending() {
  const stored = await api.storage.local.get(PENDING_KEY);
  return stored[PENDING_KEY] || {};
}

async function setPending(pending) {
  await api.storage.local.set({ [PENDING_KEY]: pending });
}

function isF95Host(hostname) {
  return hostname === 'f95zone.to' || hostname.endsWith('.f95zone.to');
}

async function report(tokenSpec, url) {
  // tokenSpec is "<port>:<token>"
  const m = /^(\d+):([A-Fa-f0-9]+)$/.exec(tokenSpec);
  if (!m) return;
  const [, port, token] = m;
  try {
    await fetch(`http://127.0.0.1:${port}/resolved`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token, url }),
    });
  } catch (err) {
    console.warn('moxie-bridge: callback failed', err);
  }
}

async function handleTabUpdate(tabId, changeInfo) {
  const url = changeInfo.url;
  if (!url) return;

  let hostname;
  try {
    hostname = new URL(url).hostname;
  } catch {
    return;
  }
  if (isF95Host(hostname)) return; // still on the check / masked interstitial

  const pending = await getPending();
  const entry = pending[tabId];
  if (!entry) return;

  delete pending[tabId];
  await setPending(pending);
  await report(entry.token, url);
}

api.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || msg.type !== 'moxie-dl' || !sender.tab || sender.tab.id == null) {
    return;
  }
  getPending().then(async (pending) => {
    const now = Date.now();
    for (const [id, entry] of Object.entries(pending)) {
      if (now - entry.at > MAX_AGE_MS) delete pending[id];
    }
    pending[sender.tab.id] = {
      token: msg.token,
      host: msg.host || '',
      at: now,
    };
    await setPending(pending);

    // If the tab already left F95Zone before the message arrived, report now.
    try {
      const tab = await api.tabs.get(sender.tab.id);
      if (tab && tab.url) {
        const hostname = new URL(tab.url).hostname;
        if (!isF95Host(hostname)) {
          await handleTabUpdate(sender.tab.id, { url: tab.url });
        }
      }
    } catch {
      /* tab closed */
    }
  });
});

api.tabs.onUpdated.addListener((tabId, changeInfo) => {
  handleTabUpdate(tabId, changeInfo);
});

// Drop watches for tabs the user closed.
api.tabs.onRemoved.addListener((tabId) => {
  getPending().then(async (pending) => {
    if (pending[tabId]) {
      delete pending[tabId];
      await setPending(pending);
    }
  });
});
