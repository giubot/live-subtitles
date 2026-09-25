// SPDX-License-Identifier: Apache-2.0
import { createFileRoute } from '@tanstack/react-router'
import { ModelsPage } from '../../../features/models/ModelsPage'

export const Route = createFileRoute('/_themed/admin/models')({
  component: ModelsPage,
})
