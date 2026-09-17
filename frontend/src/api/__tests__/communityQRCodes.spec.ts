import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { communityQRCodesAPI } from '../communityQRCodes'
import { communityQRCodesAdminAPI } from '../admin/communityQRCodes'

vi.mock('../client', () => ({
  apiClient: {
    get: vi.fn(),
    put: vi.fn()
  }
}))

describe('community QR code APIs', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads active user QR codes', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: [{ id: 'one' }] })
    await expect(communityQRCodesAPI.list()).resolves.toEqual([{ id: 'one' }])
    expect(apiClient.get).toHaveBeenCalledWith('/community-qrcodes')
  })

  it('updates the admin list as one ordered payload', async () => {
    const items = [{ id: 'one', name: 'Group', description: '', image_data: 'data:image/png;base64,eA==', enabled: true, sort_order: 0 }]
    vi.mocked(apiClient.put).mockResolvedValue({ data: items })
    await expect(communityQRCodesAdminAPI.update(items)).resolves.toEqual(items)
    expect(apiClient.put).toHaveBeenCalledWith('/admin/community-qrcodes', { items })
  })
})
