// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { OverlayPage } from '../../features/overlay/OverlayPage'
import { parseOverlaySearch } from '../../features/overlay/overlayStyle'

// OBS/vMix overlay: transparent, no MUI, styled only by its preset and the
// link (UI-7). index.html marks the document as an overlay surface before
// first paint.
export const Route = createFileRoute('/overlay/$id')({
  validateSearch: parseOverlaySearch,
  component: Overlay,
})

function Overlay() {
  const { id } = Route.useParams()
  const search = Route.useSearch()
  return <OverlayPage sessionId={id} search={search} />
}
