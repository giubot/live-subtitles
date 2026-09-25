// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { ReplayPage } from '../../../features/replay/ReplayPage'

export const Route = createFileRoute('/_themed/replay/$id')({
  component: Page,
})

function Page() {
  const { id } = Route.useParams()
  return <ReplayPage sessionId={id} />
}
