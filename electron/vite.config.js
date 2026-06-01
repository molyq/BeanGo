const { defineConfig } = require('vite');
const vue = require('@vitejs/plugin-vue');
const path = require('path');

module.exports = defineConfig({
  root: path.resolve(__dirname, 'src/renderer'),
  plugins: [vue()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    strictPort: true,
  },
  build: {
    outDir: path.resolve(__dirname, 'dist/renderer'),
    emptyOutDir: true,
  },
});
