// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { SessionsPage } from '../../../features/admin/SessionsPage'

export const Route = createFileRoute('/_themed/admin/')({
  component: SessionsPage,
})
