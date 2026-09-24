// SPDX-License-Identifier: Apache-2.0
import { createFileRoute, Outlet } from '@tanstack/react-router'

// Admin shell (side rail, top bar, ⌘K) lands in P1-17; feature lanes add
// their pages as children of this route.
export const Route = createFileRoute('/_themed/admin')({
  component: Outlet,
})
