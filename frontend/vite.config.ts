import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'react-vendor',
              test: /node_modules[\\/](?:react|react-dom|scheduler)[\\/]/,
              priority: 30,
            },
            {
              name: 'antd-vendor',
              test: /node_modules[\\/](?:antd|@ant-design|@rc-component|rc-[^\\/]+)[\\/]/,
              priority: 20,
              maxSize: 1280 * 1024,
              entriesAware: true,
              entriesAwareMergeThreshold: 48 * 1024,
            },
            {
              name: 'vendor',
              test: /node_modules[\\/]/,
              priority: 10,
              maxSize: 1280 * 1024,
              entriesAware: true,
              entriesAwareMergeThreshold: 48 * 1024,
            },
          ],
        },
      },
    },
  },
  server: {
    strictPort: true,
  },
});
