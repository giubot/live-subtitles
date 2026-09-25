// SPDX-License-Identifier: Apache-2.0
import AddOutlined from '@mui/icons-material/AddOutlined'
import EditOutlined from '@mui/icons-material/EditOutlined'
import MenuBookOutlined from '@mui/icons-material/MenuBookOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { EmptyState } from '../../components/EmptyState'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Panel } from '../../components/Panel'
import { AdminPage } from '../admin/AdminLayout'
import { GlossaryDialog } from './GlossaryDialog'

type DialogState = { mode: 'create' } | { mode: 'edit'; id: string } | undefined

/** How many terms to name on a glossary's card. */
const previewTerms = 8

/** `/admin/glossaries` (P2-06, AI-7): glossaries sessions can use. */
export function GlossariesPage() {
  const { t, i18n } = useTranslation('glossary')
  const glossaries = api.useQuery('get', '/api/glossaries')
  const [dialog, setDialog] = useState<DialogState>()
  const editing =
    dialog?.mode === 'edit' ? glossaries.data?.find((g) => g.id === dialog.id) : undefined
  const date = new Intl.DateTimeFormat(i18n.resolvedLanguage, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })

  return (
    <AdminPage
      title={t('title')}
      actions={
        <Button
          variant="contained"
          startIcon={<AddOutlined aria-hidden />}
          onClick={() => setDialog({ mode: 'create' })}
        >
          {t('newGlossary')}
        </Button>
      }
    >
      {glossaries.error && <ErrorAlert error={glossaries.error} />}
      {glossaries.data?.length === 0 && (
        <EmptyState
          icon={<MenuBookOutlined />}
          title={t('empty.title')}
          description={t('empty.body')}
        />
      )}
      <Box sx={{ display: 'grid', gap: 'var(--space-sm)' }}>
        {glossaries.data?.map((g) => {
          const names = g.terms.map((x) => x.term)
          return (
            <Panel
              key={g.id}
              title={g.name}
              actions={
                <Button
                  variant="outlined"
                  color="secondary"
                  startIcon={<EditOutlined aria-hidden />}
                  onClick={() => setDialog({ mode: 'edit', id: g.id })}
                >
                  {t('edit')}
                </Button>
              }
            >
              <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
                <Typography
                  variant="body2"
                  sx={{ color: 'var(--color-neutral)', fontVariantNumeric: 'tabular-nums' }}
                >
                  {t('summary', {
                    count: g.terms.length,
                    keep: g.doNotTranslate.length,
                    updated: date.format(new Date(g.updatedAt)),
                  })}
                </Typography>
                {names.length > 0 && (
                  <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                    {names.slice(0, previewTerms).join(' · ')}
                    {names.length > previewTerms &&
                      ` · ${t('more', { count: names.length - previewTerms })}`}
                  </Typography>
                )}
              </Box>
            </Panel>
          )
        })}
      </Box>
      {dialog?.mode === 'create' && <GlossaryDialog onClose={() => setDialog(undefined)} />}
      {editing && (
        <GlossaryDialog key={editing.id} glossary={editing} onClose={() => setDialog(undefined)} />
      )}
    </AdminPage>
  )
}
