'use strict';
const rowLimit = 1000;
const queueLimit = 2000;
const fingerprintLimit = 4096;
const renderBatch = 80;
const rows = document.querySelector('#rows');
const state = document.querySelector('#state');
const stats = document.querySelector('#stats');
const pauseButton = document.querySelector('#pause');
const queue = [];
const fingerprints = new Map();
let source = null;
let paused = false;
let duplicates = 0;
let uiDrops = 0;

function setState(label, kind = '') {
  state.textContent = label;
  state.className = kind;
}
function updateStats() {
  stats.textContent = `rows ${rows.children.length} · queued ${queue.length} · duplicates ${duplicates} · UI drops ${uiDrops}`;
}
function remember(raw) {
  if (fingerprints.has(raw)) return false;
  fingerprints.set(raw, true);
  if (fingerprints.size > fingerprintLimit) fingerprints.delete(fingerprints.keys().next().value);
  return true;
}
function append(record, raw) {
  const session = record?.fields?.session_id ?? '';
  const level = record?.level ?? '';
  if (document.querySelector('#session').value && session !== document.querySelector('#session').value) return;
  if (document.querySelector('#level').value && level !== document.querySelector('#level').value) return;

  const tr = document.createElement('tr');
  tr.dataset.level = level;
  const values = [record?.time ?? '', level, session, record?.sub ?? ''];
  for (const value of values) {
    const td = document.createElement('td');
    td.textContent = String(value);
    tr.append(td);
  }
  const detailCell = document.createElement('td');
  const detail = document.createElement('details');
  const summary = document.createElement('summary');
  summary.textContent = String(record?.fields?.msg ?? record?.msg ?? '(record)');
  const pre = document.createElement('pre');
  pre.textContent = raw;
  detail.append(summary, pre);
  detailCell.append(detail);
  tr.append(detailCell);
  rows.append(tr);
  while (rows.children.length > rowLimit) rows.firstElementChild.remove();
}
function render() {
  if (!paused) {
    for (let count = 0; count < renderBatch && queue.length; count++) {
      const raw = queue.shift();
      try { append(JSON.parse(raw), raw); } catch { append({fields: {msg: 'non-JSON event'}}, raw); }
    }
  }
  updateStats();
  requestAnimationFrame(render);
}
function disconnect() {
  source?.close();
  source = null;
  document.querySelector('#connect').textContent = 'Connect';
  setState('closed');
}
function connect() {
  if (source) { disconnect(); return; }
  source = new EventSource(document.querySelector('#url').value);
  document.querySelector('#connect').textContent = 'Disconnect';
  setState('connecting', 'retrying');
  source.onopen = () => setState('live', 'live');
  source.onerror = () => setState('reconnecting', 'retrying');
  source.addEventListener('connected', event => {
    try {
      const metadata = JSON.parse(event.data);
      setState(`live · client ${metadata.client_id}`, 'live');
    } catch { setState('live', 'live'); }
  });
  source.onmessage = event => {
    if (!remember(event.data)) { duplicates++; return; }
    if (queue.length >= queueLimit) { queue.shift(); uiDrops++; }
    queue.push(event.data);
  };
}

document.querySelector('#connect').addEventListener('click', connect);
document.querySelector('#clear').addEventListener('click', () => {
  rows.replaceChildren(); queue.length = 0; fingerprints.clear(); duplicates = 0; uiDrops = 0; updateStats();
});
pauseButton.addEventListener('click', () => {
  paused = !paused; pauseButton.textContent = paused ? 'Resume' : 'Pause';
});
window.addEventListener('beforeunload', () => source?.close());
requestAnimationFrame(render);
