const { defineConfig } = require('vite');
const vue = require('@vitejs/plugin-vue');
const path = require('path');
const fs = require('fs');

const DATA_DIR = path.resolve(__dirname, '..', 'data');
const DB_FILE = path.join(DATA_DIR, 'db.json');

function ensureDir() {
  if (!fs.existsSync(DATA_DIR)) fs.mkdirSync(DATA_DIR, { recursive: true });
}

function readDb() {
  ensureDir();
  try {
    if (fs.existsSync(DB_FILE)) return JSON.parse(fs.readFileSync(DB_FILE, 'utf8'));
  } catch (e) { console.error('[db read error]', e); }
  return { areas: [], tables: [], records: [], histories: [], settings: { key: 'main', hourlyRate: 30, currency: '¥' } };
}

function writeDb(data) {
  ensureDir();
  const tmp = DB_FILE + '.tmp';
  fs.writeFileSync(tmp, JSON.stringify(data), 'utf8');
  fs.renameSync(tmp, DB_FILE);
}

function parseBody(req) {
  return new Promise((resolve) => {
    let body = '';
    req.on('data', chunk => { body += chunk; });
    req.on('end', () => {
      try { resolve(JSON.parse(body || '{}')); } catch { resolve({}); }
    });
  });
}

function apiMiddleware(req, res, next) {
  if (!req.url.startsWith('/api/db')) return next();

  const cleanPath = req.url.split('?')[0];
  const parts = cleanPath.replace('/api/db', '').split('/').filter(Boolean);

  if (req.method === 'GET' && cleanPath === '/api/db') {
    res.setHeader('Content-Type', 'application/json');
    return res.end(JSON.stringify(readDb()));
  }

  if (req.method === 'POST' && parts.length === 1) {
    parseBody(req).then(body => {
      const db = readDb();
      const ns = parts[0];
      if (ns === 'settings') {
        db.settings = body;
      } else {
        if (!Array.isArray(db[ns])) db[ns] = [];
        const idx = db[ns].findIndex(x => x.id === body.id);
        if (idx >= 0) db[ns][idx] = body; else db[ns].push(body);
      }
      writeDb(db);
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify(body));
    });
    return;
  }

  if (req.method === 'POST' && parts.length === 2 && parts[1] === 'batch') {
    parseBody(req).then(body => {
      const db = readDb();
      const ns = parts[0];
      if (!Array.isArray(db[ns])) db[ns] = [];
      for (const item of body) {
        const idx = db[ns].findIndex(x => x.id === item.id);
        if (idx >= 0) db[ns][idx] = item; else db[ns].push(item);
      }
      writeDb(db);
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ ok: true }));
    });
    return;
  }

  if (req.method === 'DELETE' && parts.length === 2) {
    const db = readDb();
    const ns = parts[0];
    const id = parts[1];
    if (ns === 'settings') {
      db.settings = { key: 'main', hourlyRate: 30, currency: '¥' };
    } else {
      db[ns] = (db[ns] || []).filter(x => x.id !== id);
    }
    writeDb(db);
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ ok: true }));
    return;
  }

  next();
}

function apiPlugin() {
  return {
    name: 'api-plugin',
    configureServer(server) {
      server.middlewares.use(apiMiddleware);
      // 移到 SPA fallback 之前
      const stack = server.middlewares.stack;
      const our = stack.pop();
      stack.splice(stack.length - 1, 0, our);
    },
  };
}

module.exports = defineConfig({
  root: path.resolve(__dirname, 'src/renderer'),
  plugins: [vue(), apiPlugin()],
  server: {
    host: '0.0.0.0',
    port: 22700,
  },
  build: {
    outDir: path.resolve(__dirname, 'dist/renderer'),
    emptyOutDir: true,
  },
});
