import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('../client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { checkUpdates, getRollbackVersions, rollback, type RollbackVersionInfo } from '@/api/admin/system'

describe('admin system rollback API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('getRollbackVersions fetches the rollback version list', async () => {
    const versions: RollbackVersionInfo[] = [
      {
        version: '0.1.146',
        published_at: '2026-07-07T00:00:00Z',
        html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.146'
      }
    ]
    get.mockResolvedValue({ data: { versions } })

    const result = await getRollbackVersions()

    expect(get).toHaveBeenCalledWith('/admin/system/rollback-versions')
    expect(result.versions).toEqual(versions)
  })

  it('preserves Docker update metadata returned by the update check', async () => {
    get.mockResolvedValue({
      data: {
        current_version: '0.1.0',
        latest_version: 'abcdef123456',
        has_update: true,
        cached: false,
        build_type: 'release',
        update_mode: 'docker',
        current_commit: '123456789abc',
        latest_commit: 'abcdef123456',
        branch: 'custom/community-qrcode',
        staged: true
      }
    })

    const result = await checkUpdates(true)

    expect(get).toHaveBeenCalledWith('/admin/system/check-updates', {
      params: { force: 'true' }
    })
    expect(result).toMatchObject({
      update_mode: 'docker',
      staged: true,
      latest_commit: 'abcdef123456'
    })
  })

  it('rollback posts the target version in the request body', async () => {
    post.mockResolvedValue({ data: { message: 'ok', need_restart: true } })

    const result = await rollback('0.1.146')

    expect(post).toHaveBeenCalledWith(
      '/admin/system/rollback',
      { version: '0.1.146' },
      { timeout: 15 * 60 * 1000 }
    )
    expect(result.need_restart).toBe(true)
  })

  it('rollback without a version posts no body (legacy backup rollback)', async () => {
    post.mockResolvedValue({ data: { message: 'ok', need_restart: true } })

    await rollback()

    expect(post).toHaveBeenCalledWith(
      '/admin/system/rollback',
      undefined,
      { timeout: 15 * 60 * 1000 }
    )
  })
})
