// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { AdminLayout } from '../../../features/admin/AdminLayout'

// Admin shell: login gate, side rail, /ws/admin. Feature lanes add their
// pages as children of this route.
export const Route = createFileRoute('/_themed/admin')({
  component: AdminLayout,
})
