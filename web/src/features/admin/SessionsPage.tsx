// SPDX-License-Identifier: Apache-2.0
import AddOutlined from '@mui/icons-material/AddOutlined'
import ViewAgendaOutlined from '@mui/icons-material/ViewAgendaOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { EmptyState } from '../../components/EmptyState'
import { ErrorAlert } from '../../components/ErrorAlert'
import { useAdminEventsStore } from '../../realtime/admin'
import { AdminPage } from './AdminLayout'
import { SessionCard } from './SessionCard'
import { SessionDialog } from './SessionDialog'

type DialogState = { mode: 'create' } | { mode: 'edit'; id: string } | undefined

/** `/admin`: every session with its controls and links (SES-1, SES-4). */
export function SessionsPage() {
  const { t } = useTranslation('admin')
  const sessions = api.useQuery('get', '/api/sessions')
  const statuses = useAdminEventsStore((s) => s.statuses)
  const [dialog, setDialog] = useState<DialogState>()
  // Ingest tokens are only readable when created; keep them for this visit.
  const [tokens, setTokens] = useState<Record<string, string>>({})
  const [expanded, setExpanded] = useState<string>()

  const editing =
    dialog?.mode === 'edit' ? sessions.data?.find((s) => s.id === dialog.id) : undefined

  return (
    <AdminPage
      title={t('title')}
      actions={
        <Button
          variant="contained"
          startIcon={<AddOutlined aria-hidden />}
          onClick={() => setDialog({ mode: 'create' })}
        >
          {t('newSession')}
        </Button>
      }
    >
      {sessions.error && <ErrorAlert error={sessions.error} />}
      {sessions.data?.length === 0 && (
        <EmptyState
          icon={<ViewAgendaOutlined />}
          title={t('empty.title')}
          description={t('empty.body')}
        />
      )}
      <Box sx={{ display: 'grid', gap: 'var(--space-sm)' }}>
        {sessions.data?.map((s) => (
          <SessionCard
            key={s.id}
            session={s}
            status={statuses[s.id]}
            token={tokens[s.id]}
            onToken={(token) => setTokens((m) => ({ ...m, [s.id]: token }))}
            expanded={expanded === s.id}
            onToggleLinks={() => setExpanded((e) => (e === s.id ? undefined : s.id))}
            onEdit={() => setDialog({ mode: 'edit', id: s.id })}
          />
        ))}
      </Box>
      {dialog?.mode === 'create' && (
        <SessionDialog
          onClose={() => setDialog(undefined)}
          onCreated={(id, token) => {
            setTokens((m) => ({ ...m, [id]: token }))
            setExpanded(id)
          }}
        />
      )}
      {editing && (
        <SessionDialog key={editing.id} session={editing} onClose={() => setDialog(undefined)} />
      )}
    </AdminPage>
  )
}
