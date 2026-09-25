// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import { useTranslation } from 'react-i18next'
import { AdminPage } from '../admin/AdminLayout'
import { HardwarePanel } from './HardwarePanel'
import { ModelsPanel } from './ModelsPanel'

/** `/admin/models`: hardware, benchmark and local models outside the setup wizard (AI-12, AI-13). */
export function ModelsPage() {
  const { t } = useTranslation('admin')
  return (
    <AdminPage title={t('nav.models')}>
      <Box sx={{ display: 'grid', gap: 'var(--space-md)', maxInlineSize: '64rem' }}>
        <HardwarePanel />
        <ModelsPanel />
      </Box>
    </AdminPage>
  )
}
