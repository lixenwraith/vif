'use strict';

// Ordered severities match tool/vif-log/internal/logfile/schema.go: TRACE..ERROR
// are threshold-comparable, PROC and BAD are categories outside that order.
const orderedLevels = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR'];
const queueLimit = 2000;
const fingerprintLimit = 4096;
const renderBatch = 80;

const el = id => document.querySelector('#' + id);
const rowsEl = el('rows');
const queue = [];
const fingerprints = new Map();
const shown = new Map();
let retained = [];
let rowLimit = 1000;
let sequence = 0;
let source = null;
let paused = false;
let duplicates = 0;
let uiDrops = 0;
let reconnects = 0;
let clientID = '';

function setState(label, kind = '') {
  el('state').textContent = label;
  el('state').className = kind;
}

function liveLabel() {
  const client = clientID ? ` · client #${clientID}` : '';
  return `live${client}${reconnects ? ` · reconnects ${reconnects}` : ''}`;
}

function updateStats() {
  el('stats').textContent =
    `showing ${shown.size} of ${retained.length} retained · cap ${rowLimit}` +
    ` · queued ${queue.length} · duplicates ${duplicates} · UI drops ${uiDrops}`;
}

function remember(raw) {
  if (fingerprints.has(raw)) return false;
  fingerprints.set(raw, true);
  if (fingerprints.size > fingerprintLimit) {
    fingerprints.delete(fingerprints.keys().next().value);
  }
  return true;
}

// A record passes when its session matches exactly, its text contains the
// search, and its level clears the threshold or is an enabled category.
function matches(entry) {
  const session = entry.record?.fields?.session_id ?? '';
  const wanted = el('session').value.trim();
  if (wanted && session !== wanted) return false;

  const search = el('search').value.trim().toLowerCase();
  if (search && !entry.raw.toLowerCase().includes(search)) return false;

  const level = entry.record?.level ?? '';
  const index = orderedLevels.indexOf(level);
  if (index < 0) return el(level === 'BAD' ? 'bad' : 'proc').checked;
  return index >= orderedLevels.indexOf(el('level').value || 'TRACE');
}

function buildRow(entry) {
  const tr = document.createElement('tr');
  tr.dataset.level = entry.record?.level ?? '';
  const values = [
    entry.record?.time ?? '',
    entry.record?.level ?? '',
    entry.record?.fields?.session_id ?? '',
    entry.record?.sub ?? '',
  ];
  for (const value of values) {
    const td = document.createElement('td');
    td.textContent = String(value);
    tr.append(td);
  }
  const detailCell = document.createElement('td');
  const detail = document.createElement('details');
  const summary = document.createElement('summary');
  summary.textContent = String(entry.record?.fields?.msg ?? entry.record?.msg ?? '(record)');
  const pre = document.createElement('pre');
  pre.textContent = entry.raw;
  detail.append(summary, pre);
  detailCell.append(detail);
  tr.append(detailCell);
  return tr;
}

function evict() {
  while (retained.length > rowLimit) {
    const gone = retained.shift();
    const tr = shown.get(gone.seq);
    if (tr) {
      tr.remove();
      shown.delete(gone.seq);
    }
  }
}

function accept(raw) {
  let record;
  try {
    record = JSON.parse(raw);
  } catch {
    record = { fields: { msg: 'non-JSON event' } };
  }
  const entry = { seq: ++sequence, raw, record };
  retained.push(entry);
  if (matches(entry)) {
    rowsEl.prepend(buildRow(entry));
    shown.set(entry.seq, rowsEl.firstElementChild);
  }
  evict();
}

function refilter() {
  rowsEl.replaceChildren();
  shown.clear();
  for (const entry of retained) {
    if (!matches(entry)) continue;
    rowsEl.prepend(buildRow(entry));
    shown.set(entry.seq, rowsEl.firstElementChild);
  }
  updateStats();
}

function render() {
  if (!paused) {
    for (let count = 0; count < renderBatch && queue.length; count++) {
      accept(queue.shift());
    }
  }
  updateStats();
  requestAnimationFrame(render);
}

function disconnect() {
  source?.close();
  source = null;
  clientID = '';
  el('connect').textContent = 'Connect';
  setState('closed');
}

function connect() {
  if (source) {
    disconnect();
    return;
  }
  source = new EventSource(el('url').value);
  el('connect').textContent = 'Disconnect';
  setState('connecting', 'retrying');
  source.onopen = () => setState(liveLabel(), 'live');
  source.onerror = () => setState('reconnecting', 'retrying');
  // LogWisp assigns a monotonic id per accepted connection, so a rising number
  // across one viewing session means the stream reconnected, not that other
  // clients are attached.
  source.addEventListener('connected', event => {
    if (clientID) reconnects++;
    try {
      clientID = JSON.parse(event.data).client_id ?? '';
    } catch {
      clientID = '';
    }
    setState(liveLabel(), 'live');
  });
  source.onmessage = event => {
    if (!remember(event.data)) {
      duplicates++;
      return;
    }
    if (queue.length >= queueLimit) {
      queue.shift();
      uiDrops++;
    }
    queue.push(event.data);
  };
}

function exportShown() {
  const lines = retained.filter(entry => shown.has(entry.seq)).map(entry => entry.raw);
  if (!lines.length) return;
  const url = URL.createObjectURL(new Blob([lines.join('\n') + '\n'], { type: 'application/x-ndjson' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = `vif-fleet-${new Date().toISOString().replace(/[:.]/g, '-')}.jsonl`;
  link.click();
  URL.revokeObjectURL(url);
}

el('connect').addEventListener('click', connect);
el('export').addEventListener('click', exportShown);
el('clear').addEventListener('click', () => {
  retained = [];
  shown.clear();
  rowsEl.replaceChildren();
  queue.length = 0;
  fingerprints.clear();
  duplicates = 0;
  uiDrops = 0;
  updateStats();
});
el('pause').addEventListener('click', () => {
  paused = !paused;
  el('pause').textContent = paused ? 'Resume' : 'Pause';
});
el('limit').addEventListener('change', () => {
  const value = Number.parseInt(el('limit').value, 10);
  rowLimit = Number.isFinite(value) && value > 0 ? value : rowLimit;
  el('limit').value = String(rowLimit);
  evict();
  updateStats();
});
for (const id of ['session', 'search', 'level', 'proc', 'bad']) {
  el(id).addEventListener('input', refilter);
}
window.addEventListener('beforeunload', () => source?.close());

// The table header sticks under the control bar, whose height changes as the
// controls wrap.
const header = document.querySelector('header');
const trackHeader = () =>
  document.documentElement.style.setProperty('--header-height', `${header.offsetHeight}px`);
new ResizeObserver(trackHeader).observe(header);
trackHeader();

el('limit').value = String(rowLimit);
requestAnimationFrame(render);
