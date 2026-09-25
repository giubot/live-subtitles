// SPDX-License-Identifier: Apache-2.0
import SearchOutlined from '@mui/icons-material/SearchOutlined'
import Box from '@mui/material/Box'
import Dialog from '@mui/material/Dialog'
import { useColorScheme } from '@mui/material/styles'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useId, useMemo, useState, type ChangeEvent, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { copyText } from '../../components/clipboard'
import { ErrorAlert } from '../../components/ErrorAlert'
import { KbdHint } from '../../components/KbdHint'
import { nativeLanguageName } from '../../components/languageNames'
import { uiLanguages } from '../../i18n'
import { useAdminEventsStore } from '../../realtime/admin'
import { adminNav, useCaptureTokens } from './nav'
import { captureBase, captureUrl } from './sessionForm'

interface Command {
  id: string
  label: string
  /** Shown on the right: what kind of command it is. */
  group: string
  run: () => void
}

/** Lowercase without accents, so "sesion" finds "Sesión". */
const fold = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()

export interface CommandPaletteProps {
  onClose: () => void
}

/**
 * ⌘K / Ctrl+K command palette (P3-19, ADM-1): session controls, links,
 * admin pages, theme and UI language. Mount it only while open, so each
 * opening starts empty; the dialog traps focus and gives it back on close.
 */
export function CommandPalette({ onClose }: CommandPaletteProps) {
  const { t, i18n } = useTranslation('admin')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { setMode } = useColorScheme()
  const listId = useId()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const sessions = api.useQuery('get', '/api/sessions')
  const statuses = useAdminEventsStore((s) => s.statuses)
  const tokens = useCaptureTokens((s) => s.tokens)

  const settle = {
    onSuccess: onClose,
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] }),
  }
  const start = api.useMutation('post', '/api/sessions/{sessionId}/start', settle)
  const pause = api.useMutation('post', '/api/sessions/{sessionId}/pause', settle)
  const stop = api.useMutation('post', '/api/sessions/{sessionId}/stop', settle)
  const error: unknown = start.error ?? pause.error ?? stop.error

  const commands = useMemo<Command[]>(() => {
    const out: Command[] = []
    const open = (url: string) => () => {
      window.open(url, '_blank', 'noopener')
      onClose()
    }
    const copy = (url: string) => () => {
      void copyText(url)
      onClose()
    }
    for (const s of sessions.data ?? []) {
      const name = s.name
      const state = statuses[s.id]?.state ?? s.state
      const path = { params: { path: { sessionId: s.id } } }
      const group = t('palette.groups.session')
      if (state === 'idle' || state === 'error' || state === 'paused')
        out.push({
          id: `start-${s.id}`,
          label: t(state === 'paused' ? 'palette.resume' : 'palette.start', { name }),
          group,
          run: () => start.mutate(path),
        })
      if (state === 'live')
        out.push({
          id: `pause-${s.id}`,
          label: t('palette.pause', { name }),
          group,
          run: () => pause.mutate(path),
        })
      if (state === 'live' || state === 'paused' || state === 'starting')
        out.push({
          id: `stop-${s.id}`,
          label: t('palette.stop', { name }),
          group,
          run: () => stop.mutate(path),
        })
      const token = tokens[s.id]
      const links: ['viewer' | 'stage' | 'overlay' | 'capture', string][] = [
        ['viewer', s.urls.viewer],
        ['stage', s.urls.stage],
        ['overlay', s.urls.overlay],
      ]
      if (token) links.push(['capture', captureUrl(captureBase(s), token)])
      for (const [kind, url] of links) {
        const what = t(`palette.links.${kind}`)
        out.push({
          id: `open-${kind}-${s.id}`,
          label: t('palette.open', { what, name }),
          group: t('palette.groups.link'),
          run: open(url),
        })
        out.push({
          id: `copy-${kind}-${s.id}`,
          label: t('palette.copy', { what, name }),
          group: t('palette.groups.link'),
          run: copy(url),
        })
      }
    }
    for (const page of adminNav)
      out.push({
        id: `go-${page.key}`,
        label: t('palette.go', { page: t(`nav.${page.key}`) }),
        group: t('palette.groups.page'),
        run: () => {
          void navigate({ to: page.to })
          onClose()
        },
      })
    for (const mode of ['light', 'dark', 'system'] as const)
      out.push({
        id: `theme-${mode}`,
        label: t(`palette.theme.${mode}`),
        group: t('palette.groups.preference'),
        run: () => {
          setMode(mode)
          onClose()
        },
      })
    for (const lang of uiLanguages)
      out.push({
        id: `lang-${lang}`,
        label: t('palette.language', { language: nativeLanguageName(lang) }),
        group: t('palette.groups.preference'),
        run: () => {
          void i18n.changeLanguage(lang)
          onClose()
        },
      })
    return out
  }, [sessions.data, statuses, tokens, t, i18n, navigate, onClose, setMode, start, pause, stop])

  const words = fold(query).split(/\s+/).filter(Boolean)
  const shown = commands.filter((c) => {
    const text = fold(`${c.label} ${c.group}`)
    return words.every((w) => text.includes(w))
  })
  const current = Math.min(active, shown.length - 1)
  const optionId = (i: number) => `${listId}-${i}`

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (shown.length === 0) return
      const step = e.key === 'ArrowDown' ? 1 : -1
      const next = (current + step + shown.length) % shown.length
      setActive(next)
      document.getElementById(optionId(next))?.scrollIntoView({ block: 'nearest' })
    } else if (e.key === 'Enter') {
      e.preventDefault()
      shown[current]?.run()
    }
  }

  return (
    <Dialog
      open
      onClose={onClose}
      fullWidth
      maxWidth="sm"
      slotProps={{
        paper: {
          'aria-modal': true,
          'aria-label': t('palette.label'),
          sx: { marginBlockStart: '12dvh', overflow: 'hidden' },
        },
        transition: { timeout: 0 },
      }}
      sx={{ '& .MuiDialog-container': { alignItems: 'flex-start' } }}
    >
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-sm)',
          paddingInline: 'var(--space-md)',
          borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
          '& > svg': { color: 'var(--color-muted)', fontSize: '1.25rem' },
        }}
      >
        <SearchOutlined aria-hidden />
        <Box
          component="input"
          autoFocus
          role="combobox"
          aria-expanded
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={shown.length > 0 ? optionId(current) : undefined}
          aria-label={t('palette.label')}
          placeholder={t('palette.placeholder')}
          value={query}
          onChange={(e: ChangeEvent<HTMLInputElement>) => {
            setQuery(e.target.value)
            setActive(0)
          }}
          onKeyDown={onKeyDown}
          sx={{
            flex: 1,
            minInlineSize: 0,
            minBlockSize: '3rem',
            border: 0,
            outline: 0,
            background: 'transparent',
            color: 'var(--color-ink)',
            font: 'inherit',
            fontSize: 'var(--text-base)',
          }}
        />
        <KbdHint keys={['Esc']} />
      </Box>
      {error != null && <ErrorAlert error={error} sx={{ margin: 'var(--space-sm)' }} />}
      <Box
        component="ul"
        id={listId}
        role="listbox"
        aria-label={t('palette.results')}
        sx={{
          listStyle: 'none',
          margin: 0,
          padding: 'var(--space-2xs)',
          maxBlockSize: '22rem',
          overflowY: 'auto',
        }}
      >
        {shown.map((c, i) => (
          <Box
            component="li"
            key={c.id}
            id={optionId(i)}
            role="option"
            aria-selected={i === current}
            onMouseMove={() => i !== current && setActive(i)}
            onClick={c.run}
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--space-sm)',
              minBlockSize: '2.5rem',
              paddingInline: 'var(--space-sm)',
              borderRadius: 'var(--radius-input)',
              cursor: 'pointer',
              color: 'var(--color-ink-2)',
              '&[aria-selected="true"]': {
                backgroundColor: 'var(--color-accent-soft)',
                color: 'var(--color-accent-text)',
              },
            }}
          >
            <Box
              component="span"
              sx={{ flex: 1, minInlineSize: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}
            >
              {c.label}
            </Box>
            <Box
              component="span"
              sx={{
                fontFamily: 'var(--font-mono)',
                fontSize: 'var(--text-xs)',
                letterSpacing: 'var(--tracking-label)',
                textTransform: 'uppercase',
                color: 'var(--color-muted)',
                whiteSpace: 'nowrap',
              }}
            >
              {c.group}
            </Box>
          </Box>
        ))}
        {shown.length === 0 && (
          <Box
            component="li"
            role="presentation"
            sx={{ padding: 'var(--space-sm)', color: 'var(--color-muted)' }}
          >
            {t('palette.empty')}
          </Box>
        )}
      </Box>
    </Dialog>
  )
}
