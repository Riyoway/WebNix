import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    target: 'es2022',
    minify: 'oxc',
    sourcemap: false,
    cssCodeSplit: true,
    reportCompressedSize: true,
  },
});
