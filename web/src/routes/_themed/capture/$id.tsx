// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { CapturePage } from '../../../features/capture/CapturePage'

export const Route = createFileRoute('/_themed/capture/$id')({
  validateSearch: (search: Record<string, unknown>): { token?: string } =>
    search.token == null || search.token === '' ? {} : { token: String(search.token) },
  component: Page,
})

function Page() {
  const { id } = Route.useParams()
  const { token } = Route.useSearch()
  return <CapturePage sessionId={id} token={token} />
}
