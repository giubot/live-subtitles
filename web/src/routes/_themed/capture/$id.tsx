// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { StubPage } from '../../../components/StubPage'

export const Route = createFileRoute('/_themed/capture/$id')({
  component: Page,
})

function Page() {
  const { t } = useTranslation('capture')
  const { id } = Route.useParams()
  return <StubPage title={t('title', { id })} />
}
