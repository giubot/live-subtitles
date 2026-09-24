// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { ViewerPage } from '../../../features/viewer/ViewerPage'

export const Route = createFileRoute('/_themed/s/$id')({
  validateSearch: (search: Record<string, unknown>): { lang?: string } =>
    typeof search.lang === 'string' && search.lang ? { lang: search.lang } : {},
  component: Page,
})

function Page() {
  const { id } = Route.useParams()
  const { lang } = Route.useSearch()
  return <ViewerPage sessionId={id} lang={lang} />
}
