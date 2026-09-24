// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { SessionListPage } from '../../../features/viewer/SessionListPage'

export const Route = createFileRoute('/_themed/s/')({
  component: SessionListPage,
})
