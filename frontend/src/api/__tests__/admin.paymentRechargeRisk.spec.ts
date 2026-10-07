import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post },
}))

import { adminPaymentAPI } from '@/api/admin/payment'

describe('admin recharge risk API', () => {
  beforeEach(() => {
    get.mockReset().mockResolvedValue({ data: {} })
    post.mockReset().mockResolvedValue({ data: {} })
  })

  it('lists risk IPs with the requested filters and pagination', async () => {
    const params = { page: 2, page_size: 20 }

    await adminPaymentAPI.listRiskIPs(params)

    expect(get).toHaveBeenCalledWith('/admin/payment/risk-ips', { params })
  })

  it('blocks the public IP from a persisted order only after explicit confirmation', async () => {
    const request = { order_id: 17, reason: 'Verified order evidence', confirm: true as const }

    await adminPaymentAPI.blockRiskOrderIP(request)

    expect(post).toHaveBeenCalledWith('/admin/payment/risk-ips/block-order', request)
  })

  it('unblocks an IP with a reason and supports an explicitly confirmed scan', async () => {
    const unblock = { ip: '2001:db8::1', reason: 'Reviewed evidence', confirm: true as const }

    await adminPaymentAPI.unblockRiskIP(unblock)
    await adminPaymentAPI.scanBlockedRiskAccounts({ confirm: true })

    expect(post).toHaveBeenNthCalledWith(1, '/admin/payment/risk-ips/unblock', unblock)
    expect(post).toHaveBeenNthCalledWith(2, '/admin/payment/risk-ips/scan', { confirm: true })
  })
})
