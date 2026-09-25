// SPDX-License-Identifier: Apache-2.0
import Typography from '@mui/material/Typography'
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AdminPage } from '../../../features/admin/AdminLayout'

export const Route = createFileRoute('/_themed/admin/tls')({
  component: Page,
})

function Page() {
  const { t } = useTranslation('admin')
  const { t: tc } = useTranslation()
  return (
    <AdminPage title={t('nav.tls')}>
      <Typography>{tc('stub')}</Typography>
    </AdminPage>
  )
}
