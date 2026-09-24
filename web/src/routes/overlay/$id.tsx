// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'

// OBS/vMix overlay: transparent, no MUI, styled only by OverlayStyle (UI-7).
// index.html marks the document as an overlay surface before first paint.
// The real page lands in P1-16.
export const Route = createFileRoute('/overlay/$id')({
  component: Overlay,
})

function Overlay() {
  return <div data-overlay aria-live="polite" />
}
