// SPDX-License-Identifier: Apache-2.0
import { createFileRoute, Outlet } from '@tanstack/react-router'
import { AppThemeProvider } from '../theme/AppThemeProvider'

// Pathless layout for every surface that follows the UI theme.
export const Route = createFileRoute('/_themed')({
  component: () => (
    <AppThemeProvider>
      <Outlet />
    </AppThemeProvider>
  ),
})
