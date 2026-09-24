// SPDX-License-Identifier: Apache-2.0
import ChevronRightOutlined from '@mui/icons-material/ChevronRightOutlined'
import RefreshOutlined from '@mui/icons-material/RefreshOutlined'
import SubtitlesOutlined from '@mui/icons-material/SubtitlesOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Container from '@mui/material/Container'
import Typography from '@mui/material/Typography'
import { Link } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { PublicSession } from '../../api/types'
import { EmptyState } from '../../components/EmptyState'
import { ErrorAlert } from '../../components/ErrorAlert'
import { StatusChip } from '../../components/StatusChip'
import { ThemeToggle } from '../../components/ThemeToggle'
import { UiLanguageSwitcher } from '../../components/UiLanguageSwitcher'
import { stateChip } from './format'

const listPollMs = 10000

/** Running sessions first, then the rest in the server's order. */
function byActivity(a: PublicSession, b: PublicSession) {
  const rank = (s: PublicSession) => (s.state === 'live' ? 0 : s.state === 'idle' ? 2 : 1)
  return rank(a) - rank(b)
}

/** `/s`: the sessions the audience can follow (OUT-2). */
export function SessionListPage() {
  const { t } = useTranslation('viewer')
  const sessions = api.useQuery('get', '/api/public/sessions', undefined, {
    refetchInterval: listPollMs,
  })
  // The spec lists no error responses here, but the network can still fail.
  const error: unknown = sessions.error
  const title = t('sessionsTitle')
  useEffect(() => {
    document.title = title
  }, [title])

  return (
    <Container component="main" maxWidth="sm" sx={{ paddingBlock: 'var(--space-lg)' }}>
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          justifyContent: 'flex-end',
          gap: 'var(--space-xs)',
          marginBlockEnd: 'var(--space-md)',
        }}
      >
        <UiLanguageSwitcher />
        <ThemeToggle />
      </Box>
      <Typography variant="h1" sx={{ marginBlockEnd: 'var(--space-xs)' }}>
        {title}
      </Typography>
      <Typography sx={{ color: 'var(--color-neutral)', marginBlockEnd: 'var(--space-lg)' }}>
        {t('sessionsIntro')}
      </Typography>

      {error != null && (
        <ErrorAlert
          error={error}
          action={
            <Button
              variant="outlined"
              color="secondary"
              startIcon={<RefreshOutlined aria-hidden />}
              onClick={() => void sessions.refetch()}
            >
              {t('retry')}
            </Button>
          }
        />
      )}
      {sessions.data?.length === 0 && (
        <EmptyState
          icon={<SubtitlesOutlined />}
          title={t('noSessions.title')}
          description={t('noSessions.body')}
        />
      )}
      {sessions.data && sessions.data.length > 0 && (
        <Box
          component="ul"
          sx={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 'var(--space-sm)' }}
        >
          {[...sessions.data].sort(byActivity).map((s) => (
            <li key={s.id}>
              <Link
                to="/s/$id"
                params={{ id: s.id }}
                style={{
                  display: 'block',
                  color: 'inherit',
                  textDecoration: 'none',
                  borderRadius: 'var(--radius-card)',
                }}
              >
                <Box
                  sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 'var(--space-sm)',
                    padding: 'var(--space-md)',
                    border: 'var(--rule-hair) solid var(--color-rule-2)',
                    borderRadius: 'var(--radius-card)',
                    '@media (hover: hover)': {
                      '&:hover': { backgroundColor: 'var(--color-paper-3)' },
                    },
                  }}
                >
                  <Box sx={{ minInlineSize: 0, marginInlineEnd: 'auto' }}>
                    <Typography variant="h5" component="h2" sx={{ overflowWrap: 'anywhere' }}>
                      {s.name}
                    </Typography>
                    {s.room && (
                      <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
                        {s.room}
                      </Typography>
                    )}
                  </Box>
                  <StatusChip status={stateChip[s.state]} label={t(`state.${s.state}`)} />
                  <ChevronRightOutlined aria-hidden sx={{ color: 'var(--color-muted)' }} />
                </Box>
              </Link>
            </li>
          ))}
        </Box>
      )}
    </Container>
  )
}
