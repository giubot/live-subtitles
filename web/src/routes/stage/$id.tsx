// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { StagePage } from '../../features/stage/StagePage'
import { parseStageSearch } from '../../features/stage/stageOptions'

// Stage screen: its own high-contrast presets, never the UI theme (UI-7).
export const Route = createFileRoute('/stage/$id')({
  validateSearch: parseStageSearch,
  component: Stage,
})

function Stage() {
  const { id } = Route.useParams()
  const search = Route.useSearch()
  return <StagePage sessionId={id} search={search} />
}
