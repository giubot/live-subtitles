// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { ReplayPage } from '../../../features/replay/ReplayPage'

export const Route = createFileRoute('/_themed/replay/$id')({
  validateSearch: (search: Record<string, unknown>): { recording?: string } =>
    typeof search.recording === 'string' ? { recording: search.recording } : {},
  component: Page,
})

function Page() {
  const { id } = Route.useParams()
  const { recording } = Route.useSearch()
  return <ReplayPage sessionId={id} recordingId={recording} />
}
