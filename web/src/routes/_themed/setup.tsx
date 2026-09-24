// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { SetupPage } from '../../features/admin/SetupPage'

export const Route = createFileRoute('/_themed/setup')({
  component: SetupPage,
})
