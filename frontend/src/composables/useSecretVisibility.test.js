import { describe, expect, it } from 'vitest'

import { useSecretVisibility } from './useSecretVisibility.js'

describe('useSecretVisibility', () => {
 it('tracks each field locally and independently', () => {
  const { visible, toggle } = useSecretVisibility(['password', 'token'])

  expect(visible.password).toBe(false)
  expect(visible.token).toBe(false)

  expect(toggle('password')).toBe(true)
  expect(visible.password).toBe(true)
  expect(visible.token).toBe(false)

  expect(toggle('password')).toBe(false)
  expect(toggle('token')).toBe(true)
  expect(visible.password).toBe(false)
  expect(visible.token).toBe(true)
 })

 it('resets every field to password mode', () => {
  const { visible, toggle, reset } = useSecretVisibility(['password', 'token'])

  toggle('password')
  toggle('token')
  reset()

  expect(visible).toEqual({ password: false, token: false })
 })
})
