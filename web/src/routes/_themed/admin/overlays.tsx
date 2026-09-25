// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { OverlayPresetsPage } from '../../../features/presets/OverlayPresetsPage'

export const Route = createFileRoute('/_themed/admin/overlays')({
  component: OverlayPresetsPage,
})
