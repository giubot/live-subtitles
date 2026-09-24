// SPDX-License-Identifier: Apache-2.0
import EditOutlined from '@mui/icons-material/EditOutlined'
import ExpandLessOutlined from '@mui/icons-material/ExpandLessOutlined'
import LinkOutlined from '@mui/icons-material/LinkOutlined'
import PauseOutlined from '@mui/icons-material/PauseOutlined'
import PlayArrowOutlined from '@mui/icons-material/PlayArrowOutlined'
import StopOutlined from '@mui/icons-material/StopOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { SessionStatus } from '../../api/types'
import { ErrorAlert } from '../../components/ErrorAlert'
import { LevelMeter } from '../../components/LevelMeter'
import { Panel } from '../../components/Panel'
import { Stat } from '../../components/Stat'
import { StatusChip } from '../../components/StatusChip'
import { stateChip } from '../viewer/format'
import { SessionLinks } from './SessionLinks'
import type { Session } from './sessionForm'

export interface SessionCardProps {
  session: Session
  /** Live status from /ws/admin, when known. */
  status?: SessionStatus
  /** The ingest token, if this page created or rotated it. */
  token?: string
  onToken: (token: string) => void
  expanded: boolean
  onToggleLinks: () => void
  onEdit: () => void
}

/** One session: state, controls, a few live readouts and its links. */
export function SessionCard({
  session,
  status,
  token,
  onToken,
  expanded,
  onToggleLinks,
  onEdit,
}: SessionCardProps) {
  const { t } = useTranslation('admin')
  const linksId = useId()
  const queryClient = useQueryClient()
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] })
  const path = { params: { path: { sessionId: session.id } } }
  const start = api.useMutation('post', '/api/sessions/{sessionId}/start', { onSettled: refresh })
  const pause = api.useMutation('post', '/api/sessions/{sessionId}/pause', { onSettled: refresh })
  const stop = api.useMutation('post', '/api/sessions/{sessionId}/stop', { onSettled: refresh })
  const actionError: unknown = start.error ?? pause.error ?? stop.error
  const busy = start.isPending || pause.isPending || stop.isPending

  const state = status?.state ?? session.state
  const audio = status?.audio
  const room = [
    session.room,
    (session.targetLanguages ?? []).map((l) => l.toUpperCase()).join(' · '),
  ]
    .filter(Boolean)
    .join(' — ')

  return (
    <Panel tone={state === 'error' ? 'error' : 'default'} sx={{ gap: 'var(--space-md)' }}>
      <Box
        component="article"
        aria-label={session.name}
        sx={{ display: 'grid', gap: 'var(--space-md)' }}
      >
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 'var(--space-xs) var(--space-sm)',
          }}
        >
          <Typography variant="h2" sx={{ fontSize: 'var(--text-md)' }}>
            {session.name}
          </Typography>
          <StatusChip status={stateChip[state]} label={t(`state.${state}`)} />
          <Typography variant="body2" sx={{ color: 'var(--color-muted)', marginInlineEnd: 'auto' }}>
            {room}
          </Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
            {(state === 'idle' || state === 'error') && (
              <Button
                variant="outlined"
                color="secondary"
                size="small"
                startIcon={<PlayArrowOutlined aria-hidden />}
                loading={start.isPending}
                disabled={busy}
                onClick={() => start.mutate(path)}
              >
                {t('card.start')}
              </Button>
            )}
            {state === 'paused' && (
              <Button
                variant="outlined"
                color="secondary"
                size="small"
                startIcon={<PlayArrowOutlined aria-hidden />}
                loading={start.isPending}
                disabled={busy}
                onClick={() => start.mutate(path)}
              >
                {t('card.resume')}
              </Button>
            )}
            {state === 'live' && (
              <Button
                variant="outlined"
                color="secondary"
                size="small"
                startIcon={<PauseOutlined aria-hidden />}
                loading={pause.isPending}
                disabled={busy}
                onClick={() => pause.mutate(path)}
              >
                {t('card.pause')}
              </Button>
            )}
            {(state === 'live' || state === 'paused' || state === 'starting') && (
              <Button
                variant="outlined"
                color="error"
                size="small"
                startIcon={<StopOutlined aria-hidden />}
                loading={stop.isPending}
                disabled={busy}
                onClick={() => stop.mutate(path)}
              >
                {t('card.stop')}
              </Button>
            )}
            <Button
              variant="text"
              color="secondary"
              size="small"
              startIcon={<EditOutlined aria-hidden />}
              onClick={onEdit}
            >
              {t('card.edit')}
            </Button>
            <Button
              variant="text"
              color="secondary"
              size="small"
              startIcon={
                expanded ? <ExpandLessOutlined aria-hidden /> : <LinkOutlined aria-hidden />
              }
              aria-expanded={expanded}
              aria-controls={linksId}
              onClick={onToggleLinks}
            >
              {t('card.links')}
            </Button>
          </Box>
        </Box>

        <Box
          sx={{
            display: 'grid',
            gap: 'var(--space-md)',
            gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
            '@media (min-width: 48rem)': {
              gridTemplateColumns: 'minmax(0, 1.6fr) repeat(3, minmax(0, 1fr))',
            },
          }}
        >
          <Stat
            label={t('card.input', { source: t(`source.${audio?.source ?? 'browser'}`) })}
            value={
              <LevelMeter db={audio?.connected ? (audio.levelDbfs ?? -Infinity) : -Infinity} />
            }
            detail={audio?.connected ? undefined : t('card.noCapture')}
            sx={{ gridColumn: '1 / -1', '@media (min-width: 48rem)': { gridColumn: 'auto' } }}
          />
          <Stat
            label={t('card.provider')}
            value={t(`provider.${status?.provider ?? session.effectiveProvider}`)}
          />
          <Stat label={t('card.speaking')} value={status?.detectedLanguage?.toUpperCase() ?? '—'} />
          <Stat label={t('card.viewers')} value={String(status?.viewers ?? 0)} />
        </Box>

        {status?.error && state === 'error' && <ErrorAlert error={status.error} />}
        {actionError != null && <ErrorAlert error={actionError} />}

        {expanded && (
          <Box id={linksId}>
            <SessionLinks session={session} token={token} onToken={onToken} />
          </Box>
        )}
      </Box>
    </Panel>
  )
}
