import path from 'node:path'
import { fileURLToPath } from 'node:url'

import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const LEGACY_PUBLIC = 'http://localhost:8080'
const DEFAULT_PUBLIC = 'http://localhost:8180'
const DEFAULT_PROTECTED = 'http://localhost:8443'

function viteCacheProfile(): string {
  const raw = process.env.SHIP_STATUS_VITE_CACHE_PROFILE?.trim()
  if (raw && /^[a-zA-Z0-9_-]{1,32}$/.test(raw)) {
    return raw
  }
  return 'local'
}

function resolvePublicDomain(mode: string, raw: string | undefined): string {
  const v = raw?.trim()
  if (v === LEGACY_PUBLIC) {
    return DEFAULT_PUBLIC
  }
  if (v) {
    return v
  }
  return mode === 'development' ? DEFAULT_PUBLIC : ''
}

function resolveProtectedDomain(mode: string, raw: string | undefined): string {
  const v = raw?.trim()
  if (v) {
    return v
  }
  return mode === 'development' ? DEFAULT_PROTECTED : ''
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, __dirname, '')
  const vitePublic = resolvePublicDomain(mode, env.VITE_PUBLIC_DOMAIN)
  const viteProtected = resolveProtectedDomain(mode, env.VITE_PROTECTED_DOMAIN)

  return {
    plugins: [react()],
    define: {
      'import.meta.env.VITE_PUBLIC_DOMAIN': JSON.stringify(vitePublic),
      'import.meta.env.VITE_PROTECTED_DOMAIN': JSON.stringify(viteProtected),
    },
    cacheDir: path.join(__dirname, 'node_modules', `.vite-${viteCacheProfile()}`),
    server: {
      // Bind the IPv6 wildcard so both localhost (::1) and 127.0.0.1 work.
      host: '::',
      port: 3030,
      proxy: {
        '/oauth': 'http://127.0.0.1:8443',
        '/api': 'http://127.0.0.1:8443',
      },
    },
    build: {
      outDir: 'build',
      chunkSizeWarningLimit: 2000,
    },
  }
})
