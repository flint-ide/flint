// Test helper: asserts that `actual` has (at least) the properties of `expected`.
import assert from 'node:assert/strict'

export function assertSubset(actual: unknown, expected: Record<string, unknown>): void {
  assert.ok(actual !== null && typeof actual === 'object', `expected an object, got ${String(actual)}`)
  const picked = Object.fromEntries(Object.keys(expected).map((k) => [k, (actual as Record<string, unknown>)[k]]))
  assert.deepEqual(picked, expected)
}
