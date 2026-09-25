// SPDX-License-Identifier: Apache-2.0
import HttpsOutlined from '@mui/icons-material/HttpsOutlined'
import KeyOutlined from '@mui/icons-material/KeyOutlined'
import GraphicEqOutlined from '@mui/icons-material/GraphicEqOutlined'
import MemoryOutlined from '@mui/icons-material/MemoryOutlined'
import MenuBookOutlined from '@mui/icons-material/MenuBookOutlined'
import SettingsOutlined from '@mui/icons-material/SettingsOutlined'
import SubtitlesOutlined from '@mui/icons-material/SubtitlesOutlined'
import ViewAgendaOutlined from '@mui/icons-material/ViewAgendaOutlined'
import type { ReactNode } from 'react'
import { create } from 'zustand'

export type AdminPath =
  | '/admin'
  | '/admin/glossaries'
  | '/admin/models'
  | '/admin/overlays'
  | '/admin/providers'
  | '/admin/recordings'
  | '/admin/settings'
  | '/admin/tls'

export type AdminPageKey =
  | 'sessions'
  | 'glossaries'
  | 'overlays'
  | 'recordings'
  | 'providers'
  | 'models'
  | 'settings'
  | 'tls'

export interface AdminNavItem {
  to: AdminPath
  /** `nav.<key>` in admin.json. */
  key: AdminPageKey
  icon: ReactNode
}

/** Every admin page, in rail order: the rail, the narrow-screen menu and ⌘K all use it. */
export const adminNav: AdminNavItem[] = [
  { to: '/admin', key: 'sessions', icon: <ViewAgendaOutlined /> },
  { to: '/admin/glossaries', key: 'glossaries', icon: <MenuBookOutlined /> },
  { to: '/admin/overlays', key: 'overlays', icon: <SubtitlesOutlined /> },
  { to: '/admin/recordings', key: 'recordings', icon: <GraphicEqOutlined /> },
  { to: '/admin/providers', key: 'providers', icon: <KeyOutlined /> },
  { to: '/admin/models', key: 'models', icon: <MemoryOutlined /> },
  { to: '/admin/settings', key: 'settings', icon: <SettingsOutlined /> },
  { to: '/admin/tls', key: 'tls', icon: <HttpsOutlined /> },
]

interface CaptureTokens {
  /** Ingest token by session id: only readable right after it's made, so kept for this visit. */
  tokens: Record<string, string>
  setToken(sessionId: string, token: string): void
}

export const useCaptureTokens = create<CaptureTokens>()((set) => ({
  tokens: {},
  setToken: (id, token) => set((s) => ({ tokens: { ...s.tokens, [id]: token } })),
}))
