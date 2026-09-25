// SPDX-License-Identifier: Apache-2.0
import CloseOutlined from '@mui/icons-material/CloseOutlined'
import EditOutlined from '@mui/icons-material/EditOutlined'
import SubtitlesOutlined from '@mui/icons-material/SubtitlesOutlined'
import VisibilityOffOutlined from '@mui/icons-material/VisibilityOffOutlined'
import VisibilityOutlined from '@mui/icons-material/VisibilityOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Drawer from '@mui/material/Drawer'
import IconButton from '@mui/material/IconButton'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { Caption } from '../../api/types'
import { EmptyState } from '../../components/EmptyState'
import { ErrorAlert } from '../../components/ErrorAlert'
import { KbdHint } from '../../components/KbdHint'
import { sourceTrack } from '../../components/languageNames'
import { Notice } from '../../components/Notice'
import { segmentedSx } from '../../components/segmented'
import { StatusChip } from '../../components/StatusChip'
import { Tooltip } from '../../components/Tooltip'
import { emptyTrack, useCaptions } from '../../realtime/captions'
import {
  clockTime,
  correctionsUnavailable,
  editorLines,
  isNotImplemented,
  type EditorLine,
} from './captionEdit'
import type { Session } from './sessionForm'

/** Recent finals asked for on connect, per track. */
const historySize = 200

export interface CaptionEditorProps {
  session: Session
  onClose: () => void
}

/**
 * Live caption correction (ADM-4): the recent lines of one track, newest
 * first, each editable inline (Enter saves, Esc cancels) or hidden from
 * viewers. Changes go through PATCH /api/sessions/{id}/captions/{segmentId};
 * the server broadcasts the corrected line, which arrives back here over
 * /ws/captions. A server without the endpoint (501) gets a notice and the
 * controls turn off.
 */
export function CaptionEditor({ session, onClose }: CaptionEditorProps) {
  const { t } = useTranslation('admin')
  const tracks = [sourceTrack, ...(session.targetLanguages ?? [])]
  const [track, setTrack] = useState<string>(session.targetLanguages?.[0] ?? sourceTrack)
  const live = useCaptions(session.id, [track], { history: historySize })
  const current = live.tracks[track] ?? emptyTrack
  // Lines hidden from here, by track then segment: the live track drops them.
  const [hidden, setHidden] = useState<Record<string, Record<string, Caption>>>({})
  const [editing, setEditing] = useState<{ segmentId: string; draft: string } | null>(null)
  const [unavailable, setUnavailable] = useState(() => correctionsUnavailable.has(session.id))
  const patch = api.useMutation('patch', '/api/sessions/{sessionId}/captions/{segmentId}', {
    onSuccess: (caption, vars) => {
      const lang = vars.params.query.lang
      setHidden((all) => {
        const mine = { ...all[lang] }
        if (caption.hidden) mine[caption.segmentId] = caption
        else delete mine[caption.segmentId]
        return { ...all, [lang]: mine }
      })
      setEditing(null)
    },
    onError: (err) => {
      if (!isNotImplemented(err)) return
      correctionsUnavailable.add(session.id)
      setUnavailable(true)
      setEditing(null)
    },
  })
  const pendingId = patch.isPending ? patch.variables?.params.path.segmentId : undefined
  const lines = editorLines(current.finals, hidden[track] ?? {})

  const send = (segmentId: string, body: { text?: string; hidden?: boolean }) =>
    patch.mutate({
      params: { path: { sessionId: session.id, segmentId }, query: { lang: track } },
      body,
    })

  const save = (line: EditorLine) => {
    if (!editing) return
    const text = editing.draft.trim()
    if (text === '' || text === line.caption.text) {
      setEditing(null)
      return
    }
    send(line.caption.segmentId, { text })
  }

  const onKeyDown = (e: KeyboardEvent, line: EditorLine) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      save(line)
    } else if (e.key === 'Escape') {
      // Cancel the edit, not the drawer.
      e.preventDefault()
      e.stopPropagation()
      setEditing(null)
    }
  }

  // The text's own language for screen readers (OUT-11): the source track mixes languages.
  const lineLang = (c: Caption) => (track === sourceTrack ? c.sourceLang : c.lang)
  const trackLabel = (lang: string) =>
    lang === sourceTrack
      ? session.sourceLanguage && session.sourceLanguage !== 'auto'
        ? t('corrections.sourceTrackLang', { lang: session.sourceLanguage.toUpperCase() })
        : t('corrections.sourceTrack')
      : lang.toUpperCase()

  return (
    <Drawer
      anchor="right"
      open
      onClose={onClose}
      slotProps={{
        paper: {
          'aria-labelledby': `corrections-title-${session.id}`,
          sx: {
            backgroundImage: 'none',
            inlineSize: 'min(38rem, 100vw)',
            display: 'grid',
            gridTemplateRows: 'auto minmax(0, 1fr)',
          },
        },
      }}
    >
      <Box
        component="header"
        sx={{
          display: 'grid',
          gap: 'var(--space-sm)',
          padding: 'var(--space-md)',
          borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 'var(--space-xs)' }}>
          <Typography
            id={`corrections-title-${session.id}`}
            variant="h2"
            sx={{ fontSize: 'var(--text-md)', marginInlineEnd: 'auto' }}
          >
            {t('corrections.title', { name: session.name })}
          </Typography>
          <IconButton size="small" aria-label={t('corrections.close')} onClick={onClose}>
            <CloseOutlined fontSize="small" aria-hidden />
          </IconButton>
        </Box>
        <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
          {t('corrections.intro')}
        </Typography>
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 'var(--space-xs) var(--space-md)',
          }}
        >
          <ToggleButtonGroup
            aria-label={t('corrections.track')}
            value={track}
            exclusive
            onChange={(_, v: string | null) => {
              if (!v) return
              setTrack(v)
              setEditing(null)
            }}
            sx={segmentedSx}
          >
            {tracks.map((lang) => (
              <ToggleButton key={lang} value={lang}>
                {trackLabel(lang)}
              </ToggleButton>
            ))}
          </ToggleButtonGroup>
          {!unavailable && (
            <Typography
              variant="body2"
              sx={{
                display: 'inline-flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                gap: 'var(--space-2xs)',
                color: 'var(--color-neutral)',
              }}
            >
              <KbdHint keys={['Enter']} /> {t('corrections.saveHint')} · <KbdHint keys={['Esc']} />{' '}
              {t('corrections.cancelHint')}
            </Typography>
          )}
        </Box>
        {unavailable && <Notice>{t('corrections.unavailable')}</Notice>}
        {patch.error != null && !isNotImplemented(patch.error) && (
          <ErrorAlert error={patch.error} />
        )}
        {live.connection === 'reconnecting' && <Notice>{t('corrections.reconnecting')}</Notice>}
      </Box>

      <Box
        component="ol"
        aria-label={t('corrections.lines', { track: trackLabel(track) })}
        sx={{
          listStyle: 'none',
          margin: 0,
          padding: 'var(--space-sm) var(--space-md) var(--space-lg)',
          overflowY: 'auto',
          overscrollBehavior: 'contain',
          display: 'grid',
          alignContent: 'start',
          gap: 'var(--space-2xs)',
        }}
      >
        {current.interim && (
          <Box component="li" sx={{ ...rowSx, color: 'var(--color-muted)', fontStyle: 'italic' }}>
            <Box sx={timeSx}>{clockTime(current.interim.start)}</Box>
            <Box sx={{ minInlineSize: 0 }}>
              <span lang={lineLang(current.interim)}>{current.interim.text}</span>{' '}
              <StatusChip status="starting" label={t('corrections.inProgress')} />
            </Box>
          </Box>
        )}
        {lines.length === 0 && !current.interim && (
          <Box component="li">
            <EmptyState
              titleAs="h3"
              icon={<SubtitlesOutlined />}
              title={t('corrections.empty.title')}
              description={t('corrections.empty.body')}
            />
          </Box>
        )}
        {lines.map((line) => {
          const c = line.caption
          const isEditing = editing?.segmentId === c.segmentId
          const pending = pendingId === c.segmentId
          return (
            <Box
              component="li"
              key={c.segmentId}
              sx={[rowSx, line.hidden && { color: 'var(--color-neutral)' }]}
            >
              <Box sx={timeSx}>{clockTime(c.start)}</Box>
              {isEditing ? (
                <TextField
                  autoFocus
                  fullWidth
                  multiline
                  size="small"
                  value={editing.draft}
                  onChange={(e) => setEditing({ segmentId: c.segmentId, draft: e.target.value })}
                  onKeyDown={(e) => onKeyDown(e, line)}
                  disabled={pending}
                  slotProps={{
                    htmlInput: { 'aria-label': t('corrections.textLabel') },
                  }}
                />
              ) : (
                <Box
                  sx={{
                    minInlineSize: 0,
                    overflowWrap: 'anywhere',
                    textDecoration: line.hidden ? 'line-through' : undefined,
                  }}
                >
                  <span lang={lineLang(c)}>{c.text}</span>
                  {(c.edited || line.hidden) && (
                    <Box
                      component="span"
                      sx={{
                        display: 'inline-flex',
                        gap: 'var(--space-2xs)',
                        marginInlineStart: 'var(--space-xs)',
                      }}
                    >
                      {c.edited && (
                        <StatusChip status="idle" noDot label={t('corrections.edited')} />
                      )}
                      {line.hidden && (
                        <StatusChip status="warn" noDot label={t('corrections.hidden')} />
                      )}
                    </Box>
                  )}
                </Box>
              )}
              <Box sx={{ display: 'flex', gap: 'var(--space-3xs)', alignItems: 'flex-start' }}>
                {isEditing ? (
                  <>
                    <Button
                      size="small"
                      variant="contained"
                      loading={pending}
                      onClick={() => save(line)}
                    >
                      {t('corrections.save')}
                    </Button>
                    <Button
                      size="small"
                      variant="text"
                      color="secondary"
                      disabled={pending}
                      onClick={() => setEditing(null)}
                    >
                      {t('corrections.cancel')}
                    </Button>
                  </>
                ) : (
                  <>
                    {!line.hidden && (
                      <Tooltip title={t('corrections.edit')}>
                        <span>
                          <IconButton
                            size="small"
                            aria-label={t('corrections.edit')}
                            disabled={unavailable || patch.isPending}
                            onClick={() => setEditing({ segmentId: c.segmentId, draft: c.text })}
                          >
                            <EditOutlined fontSize="small" aria-hidden />
                          </IconButton>
                        </span>
                      </Tooltip>
                    )}
                    <Tooltip title={line.hidden ? t('corrections.unhide') : t('corrections.hide')}>
                      <span>
                        <IconButton
                          size="small"
                          aria-label={line.hidden ? t('corrections.unhide') : t('corrections.hide')}
                          disabled={unavailable || patch.isPending}
                          onClick={() => send(c.segmentId, { hidden: !line.hidden })}
                        >
                          {line.hidden ? (
                            <VisibilityOutlined fontSize="small" aria-hidden />
                          ) : (
                            <VisibilityOffOutlined fontSize="small" aria-hidden />
                          )}
                        </IconButton>
                      </span>
                    </Tooltip>
                  </>
                )}
              </Box>
            </Box>
          )
        })}
      </Box>
    </Drawer>
  )
}

const rowSx = {
  display: 'grid',
  gridTemplateColumns: 'auto minmax(0, 1fr) auto',
  alignItems: 'start',
  gap: 'var(--space-sm)',
  paddingBlock: 'var(--space-xs)',
  borderBlockEnd: 'var(--rule-hair) solid var(--color-rule)',
  fontSize: 'var(--text-sm)',
  lineHeight: 'var(--leading-body)',
} as const

const timeSx = {
  fontFamily: 'var(--font-mono)',
  fontSize: 'var(--text-xs)',
  color: 'var(--color-neutral)',
  paddingBlockStart: 'var(--space-3xs)',
  minInlineSize: '3.5rem',
} as const
