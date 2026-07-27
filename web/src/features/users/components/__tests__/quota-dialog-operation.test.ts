/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  buildQuotaDialogPayload,
  createQuotaDialogOperationState,
} from '../user-quota-operation.ts'

describe('user quota dialog operation id', () => {
  test('reuses the same operation_id when a failed quota submission is retried without changing inputs', () => {
    const operationState = createQuotaDialogOperationState(() => 'quota-op-1')

    const firstPayload = buildQuotaDialogPayload({
      userId: 12,
      mode: 'add',
      value: 2,
      operationState,
    })
    const retryPayload = buildQuotaDialogPayload({
      userId: 12,
      mode: 'add',
      value: 2,
      operationState,
    })

    assert.equal(firstPayload.operation_id, 'quota-op-1')
    assert.equal(retryPayload.operation_id, 'quota-op-1')
  })

  test('rotates operation_id when quota dialog inputs change after a failed attempt', () => {
    let counter = 0
    const operationState = createQuotaDialogOperationState(
      () => `quota-op-${++counter}`
    )

    const firstPayload = buildQuotaDialogPayload({
      userId: 12,
      mode: 'add',
      value: 2,
      operationState,
    })
    operationState.reset()
    const changedPayload = buildQuotaDialogPayload({
      userId: 12,
      mode: 'subtract',
      value: 2,
      operationState,
    })

    assert.equal(firstPayload.operation_id, 'quota-op-1')
    assert.equal(changedPayload.operation_id, 'quota-op-2')
  })
})
