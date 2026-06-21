const clone = (value) => JSON.parse(JSON.stringify(value));

const DEFAULT_DATA = {
  areas: [],
  tables: [],
  records: [],
  histories: [],
  settings: { key: 'main', hourlyRate: 30, currency: '¥' },
};

const cache = clone(DEFAULT_DATA);

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  return res.json();
}

function replaceAll(data) {
  cache.areas = data?.areas || [];
  cache.tables = data?.tables || [];
  cache.records = data?.records || [];
  cache.histories = data?.histories || [];
  cache.settings = { ...DEFAULT_DATA.settings, ...(data?.settings || {}) };
}

async function loadAll() {
  const data = await api('/api/db');
  replaceAll(data || DEFAULT_DATA);
  return clone(cache);
}

function put(namespace, payload) {
  if (namespace === 'settings') {
    cache.settings = clone(payload);
  } else {
    const arr = cache[namespace] || [];
    const idx = arr.findIndex((x) => x.id === payload.id);
    if (idx >= 0) arr[idx] = clone(payload);
    else arr.push(clone(payload));
    cache[namespace] = arr;
  }
  return api(`/api/db/${namespace}`, { method: 'POST', body: JSON.stringify(clone(payload)) });
}

function putBatch(namespace, payloads) {
  const arr = cache[namespace] || [];
  for (const payload of payloads) {
    const idx = arr.findIndex((x) => x.id === payload.id);
    if (idx >= 0) arr[idx] = clone(payload);
    else arr.push(clone(payload));
  }
  cache[namespace] = arr;
  return api(`/api/db/${namespace}/batch`, { method: 'POST', body: JSON.stringify(clone(payloads)) });
}

function del(namespace, id) {
  if (namespace === 'settings') {
    cache.settings = clone(DEFAULT_DATA.settings);
  } else {
    cache[namespace] = (cache[namespace] || []).filter((x) => x.id !== id);
  }
  return api(`/api/db/${namespace}/${id}`, { method: 'DELETE' });
}

export const db = { loadAll, put, putBatch, del, replaceAll };
