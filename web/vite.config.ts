// SPDX-License-Identifier: Apache-2.0
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import type { ProxyOptions } from 'vite'
import { defineConfig } from 'vitest/config'

// VITE_API picks what /api talks to in dev:
//   server (default): the Go server (task dev:server), VITE_API_TARGET overrides its URL.
//   mock: the Prism mock of api/openapi.yaml (task dev:mock). Prism enforces the
//         spec's security, so the proxy adds an admin bearer token. WebSockets
//         aren't mocked and still go to the Go server.
const api = process.env.VITE_API ?? 'server'
if (api !== 'server' && api !== 'mock') throw new Error(`VITE_API=${api}: want server or mock`)
const serverTarget = process.env.VITE_API_TARGET ?? 'http://localhost:8080'
const mockTarget = 'http://localhost:4010'

const http: ProxyOptions =
  api === 'mock'
    ? { target: mockTarget, headers: { Authorization: 'Bearer mock' } }
    : { target: serverTarget }

export default defineConfig({
  plugins: [tanstackRouter({ target: 'react', autoCodeSplitting: true }), react()],
  server: {
    host: true,
    proxy: {
      '/api': http,
      '/healthz': http,
      '/ws': { target: serverTarget, ws: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
  },
})
