// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { RecordingsPage } from '../../../features/recordings/RecordingsPage'

export const Route = createFileRoute('/_themed/admin/recordings')({
  component: RecordingsPage,
})
