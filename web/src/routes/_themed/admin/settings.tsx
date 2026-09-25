// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { SettingsPage } from '../../../features/settings/SettingsPage'

export const Route = createFileRoute('/_themed/admin/settings')({
  component: SettingsPage,
})
