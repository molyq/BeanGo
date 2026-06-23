const { defineConfig } = require('vite');
const vue = require('@vitejs/plugin-vue');
const path = require('path');

module.exports = defineConfig({
  root: path.resolve(__dirname, 'src/renderer'),
  plugins: [vue()],
  server: {
    host: '0.0.0.0',
    port: 22700,
    proxy: {
      '/api': 'http://127.0.0.1:22701',
    },
  },
  build: {
    outDir: path.resolve(__dirname, 'dist/renderer'),
    emptyOutDir: true,
  },
});
