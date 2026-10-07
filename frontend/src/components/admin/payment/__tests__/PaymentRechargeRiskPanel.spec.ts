import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import enLocale from '@/i18n/locales/en'
import zhLocale from '@/i18n/locales/zh'
import PaymentRechargeRiskPanel from '../PaymentRechargeRiskPanel.vue'

const { adminPaymentAPI, showError, showSuccess } = vi.hoisted(() => ({
  adminPaymentAPI: {
    listRiskIPs: vi.fn(),
    blockRiskOrderIP: vi.fn(),
    unblockRiskIP: vi.fn(),
    scanBlockedRiskAccounts: vi.fn(),
  },
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'en-US' },
    t: (key: string) => key,
  }),
}))

enableAutoUnmount(afterEach)

const record = {
  ip: '198.51.100.23',
  reason: 'Verified order risk',
  evidence: '{"order_id":100,"user_id":42,"source":"persisted_payment_order"}',
  actor: 'admin@example.com',
  created_at: 1791367740,
  active: true,
  linked_users: 2,
}

function setup() {
  adminPaymentAPI.listRiskIPs.mockResolvedValue({
    data: { items: [record], total: 1, page: 1, page_size: 20, pages: 1 },
  })
  adminPaymentAPI.blockRiskOrderIP.mockResolvedValue({ data: { blocked: true } })
  adminPaymentAPI.unblockRiskIP.mockResolvedValue({ data: { released: true, accounts_restored: false } })
  adminPaymentAPI.scanBlockedRiskAccounts.mockResolvedValue({ data: { restricted_accounts: 1 } })
  showError.mockReset()
  showSuccess.mockReset()

  return mount(PaymentRechargeRiskPanel, {
    global: {
      stubs: {
        Icon: true,
        LoadingSpinner: true,
      },
    },
  })
}

describe('PaymentRechargeRiskPanel', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('renders only the real record fields and explains the supported policy', async () => {
    const wrapper = setup()
    await flushPromises()

    expect(wrapper.text()).toContain('payment.admin.risk.frequencyRuleDescription')
    expect(wrapper.text()).toContain('payment.admin.risk.permanentBlockRuleDescription')
    expect(wrapper.text()).toContain('payment.admin.risk.periodicScan')
    expect(wrapper.text()).toContain('payment.admin.risk.sharedIPWarning')
    expect(wrapper.text()).toContain('198.51.100.23')
    expect(wrapper.text()).toContain('2')
    expect(wrapper.text()).toContain('admin@example.com')
    expect(adminPaymentAPI.listRiskIPs).toHaveBeenCalledWith({ page: 1, page_size: 20 })
    expect(adminPaymentAPI).not.toHaveProperty('getRiskPolicy')
    expect(adminPaymentAPI).not.toHaveProperty('getRiskIPDetail')
  })

  it('keeps frequency limits separate from permanent-block triggers in both locales', () => {
    const zhRisk = zhLocale.payment.admin.risk
    const enRisk = enLocale.payment.admin.risk

    expect(zhRisk.frequencyRuleDescription).toContain('成功创建')
    expect(zhRisk.frequencyRuleDescription).toContain('不计入次数')
    expect(zhRisk.frequencyRuleDescription).toContain('不会永久封禁 IP')
    expect(zhRisk.permanentBlockRuleDescription).toContain('RSA2')
    expect(zhRisk.permanentBlockRuleDescription).toContain('管理员')
    expect(enRisk.frequencyRuleDescription).toContain('successfully created')
    expect(enRisk.frequencyRuleDescription).toContain('do not count')
    expect(enRisk.frequencyRuleDescription).toContain('never permanently blocks')
    expect(enRisk.permanentBlockRuleDescription).toContain('server-verified RSA2')
    expect(enRisk.permanentBlockRuleDescription).toContain('administrator')
  })

  it('requires explicit confirmation before blocking an order IP', async () => {
    const wrapper = setup()
    await flushPromises()

    await wrapper.get('input[type="number"]').setValue('123')
    await wrapper.get('input[placeholder="payment.admin.risk.blockReasonPlaceholder"]').setValue('Verified abuse')
    const blockAction = wrapper.findAll('button').find(button => button.text().includes('blockOrderAction'))
    await blockAction?.trigger('click')
    await flushPromises()

    const confirmButton = wrapper.findAll('button').find(button => button.text().includes('confirmBlock'))
    expect(confirmButton?.attributes('disabled')).toBeDefined()
    await wrapper.get('[role="dialog"] input[type="checkbox"]').setValue(true)
    await confirmButton?.trigger('click')
    await flushPromises()

    expect(adminPaymentAPI.blockRiskOrderIP).toHaveBeenCalledWith({
      order_id: 123,
      reason: 'Verified abuse',
      confirm: true,
    })
    expect(adminPaymentAPI.listRiskIPs).toHaveBeenCalledTimes(2)
  })

  it('requires a reason and confirmation to unblock without restoring accounts', async () => {
    const wrapper = setup()
    await flushPromises()

    const unblockAction = wrapper.findAll('button').find(button => button.text().includes('unblockAction'))
    await unblockAction?.trigger('click')
    await flushPromises()

    const confirmButton = wrapper.findAll('button').find(button => button.text().includes('confirmUnblock'))
    expect(confirmButton?.attributes('disabled')).toBeDefined()
    await wrapper.get('[role="dialog"] textarea').setValue('Reviewed false positive')
    await wrapper.get('[role="dialog"] input[type="checkbox"]').setValue(true)
    await confirmButton?.trigger('click')
    await flushPromises()

    expect(adminPaymentAPI.unblockRiskIP).toHaveBeenCalledWith({
      ip: '198.51.100.23',
      reason: 'Reviewed false positive',
      confirm: true,
    })
    expect(showSuccess).toHaveBeenCalledWith('payment.admin.risk.unblockSuccess')
  })

  it('requires confirmation before manually scanning linked accounts', async () => {
    const wrapper = setup()
    await flushPromises()

    const scanAction = wrapper.findAll('button').find(button => button.text().includes('scanNow'))
    await scanAction?.trigger('click')
    await flushPromises()
    const confirmButton = wrapper.findAll('button').find(button => button.text().includes('confirmScan'))
    expect(confirmButton?.attributes('disabled')).toBeDefined()

    await wrapper.get('[role="dialog"] input[type="checkbox"]').setValue(true)
    await confirmButton?.trigger('click')
    await flushPromises()

    expect(adminPaymentAPI.scanBlockedRiskAccounts).toHaveBeenCalledWith({ confirm: true })
  })
})
