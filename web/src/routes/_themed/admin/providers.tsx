// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { ProvidersPage } from '../../../features/settings/ProvidersPage'

export const Route = createFileRoute('/_themed/admin/providers')({
  component: ProvidersPage,
})
