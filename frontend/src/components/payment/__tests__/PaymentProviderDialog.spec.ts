import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PaymentProviderDialog from '@/components/payment/PaymentProviderDialog.vue'
import { STRIPE_SDK_API_VERSION } from '@/components/payment/providerConfig'
import type { ProviderInstance } from '@/types/payment'

const messages: Record<string, string> = {
  'admin.settings.payment.providerConfig': 'Credentials',
  'admin.settings.payment.field_gatewaySignTypeHint': 'RSA2 is recommended for new setups.',
  'admin.settings.payment.validationEasyPayRsaPublicKeyInvalid': 'Enter an RSA public key in PEM format.',
  'admin.settings.payment.validationEasyPayRsaKeyIdInvalid': 'Enter a valid PayPro Key ID.',
  'admin.settings.payment.easypayCustomMethods': 'Custom EasyPay methods',
  'admin.settings.payment.easypayCustomMethodsHint': 'Add provider-specific EasyPay type values.',
  'admin.settings.payment.addCustomMethod': 'Add method',
  'admin.settings.payment.customMethodType': 'Payment type',
  'admin.settings.payment.customMethodUpstreamType': 'Upstream type',
  'admin.settings.payment.customMethodDisplayName': 'Display name',
  'admin.settings.payment.customMethodDisplayNamePlaceholder': '信用卡',
  'admin.settings.payment.paymentGuideTrigger': 'View payment guide',
  'admin.settings.payment.alipayGuideSummary': 'Desktop prefers QR precreate and falls back to cashier; mobile prefers WAP checkout.',
  'admin.settings.payment.wxpayGuideSummary': 'Desktop prefers Native QR; mobile routes to JSAPI or H5 based on browser context.',
  'admin.settings.payment.airwallexGuideSummary': 'Use Payment Acceptance read/write only.',
  'admin.settings.payment.stripeWebhookHint': 'Configure Stripe webhook.',
  'admin.settings.payment.stripeWebhookApiVersionHint': 'Use Stripe API version {version}.',
  'admin.settings.payment.airwallexWebhookHint': 'Select payment_intent.succeeded and use the latest stable API version.',
}

const showError = vi.hoisted(() => vi.fn())

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      const message = messages[key] ?? key
      if (!params) return message
      return Object.entries(params).reduce(
        (value, [name, replacement]) => value.replaceAll(`{${name}}`, replacement),
        message,
      )
    },
  }),
}))

function providerFactory(overrides: Partial<ProviderInstance> = {}): ProviderInstance {
  return {
    id: 1,
    provider_key: 'airwallex',
    name: 'Airwallex',
    config: {},
    supported_types: ['airwallex'],
    enabled: true,
    payment_mode: '',
    refund_enabled: false,
    allow_user_refund: false,
    limits: '',
    sort_order: 0,
    ...overrides,
  }
}

function mountDialog(options: { editing?: ProviderInstance | null } = {}) {
  return mount(PaymentProviderDialog, {
    props: {
      show: true,
      saving: false,
      editing: options.editing ?? null,
      allKeyOptions: [
        { value: 'easypay', label: 'EasyPay' },
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'stripe', label: 'Stripe' },
        { value: 'airwallex', label: 'Airwallex' },
      ],
      enabledKeyOptions: [
        { value: 'easypay', label: 'EasyPay' },
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'airwallex', label: 'Airwallex' },
      ],
      allPaymentTypes: [
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
      ],
      redirectLabel: 'Redirect',
    },
    global: {
      stubs: {
        BaseDialog: {
          template: '<div><slot /><slot name="footer" /></div>',
        },
        Select: {
          props: ['modelValue', 'options', 'disabled'],
          emits: ['update:modelValue'],
          template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
        },
        ToggleSwitch: {
          template: '<div />',
        },
      },
    },
  })
}

describe('PaymentProviderDialog callback URLs', () => {
  it.each([
    ['https://notify.example.com/', 'https://return.example.com///', 'https://notify.example.com', 'https://return.example.com'],
    [' https://notify.example.com/sub/ ', ' https://return.example.com/site/ ', 'https://notify.example.com/sub', 'https://return.example.com/site'],
    ['https://notify.example.com', 'https://return.example.com', 'https://notify.example.com', 'https://return.example.com'],
    ['', '', window.location.origin, window.location.origin],
  ])('joins callback paths to %s and %s', async (notify, returnUrl, expectedNotify, expectedReturn) => {
    const provider = providerFactory({
      provider_key: 'easypay', name: 'EasyPay',
      config: { pid: 'pid-1', apiBase: 'https://pay.example.com' },
      supported_types: ['alipay'], payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    const bases = wrapper.findAll('input').filter(input => input.classes().includes('!rounded-r-none'))
    await bases[0].setValue(notify)
    await bases[1].setValue(returnUrl)
    await wrapper.find('form').trigger('submit')
    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.notifyUrl).toBe(expectedNotify + '/api/v1/payment/webhook/easypay')
    expect(payload.config.returnUrl).toBe(expectedReturn + '/payment/result')
    wrapper.unmount()
  })
})

describe('PaymentProviderDialog EasyPay RSA2 configuration', () => {
  const rsaPublicKey = [
    '-----BEGIN PUBLIC KEY-----',
    'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAm9jNiuZasGrUN4s1ssE0',
    'ehRRSj9JxkVTAsRsOS55t6adjuZgfzk+DroeVa8rgzItg5rUhhvznoFILFr0xcFZ',
    'EZYr7lFuJGCAwLOQsgQD1T3nmPi5jdC5Xy9GSDrDLpeteRpQymqxVmueLeDulUKo',
    'jAGWe856TCEOCBMl8Gd/SRTocLh4WZiMKWN9TPsqPqhfFjkegRLV7eRBCMfrObnz',
    'a07WnELwCZ0q3BOWOTEXBm/Fc0Si4c1HU+e/G0XQdDn9O2aO4itRNN3RAqSoQebx',
    'cto3KizJzPITqcxNtYfCdTP2m48FAaPU0VXj4vdsxzMKtGZOhL2kpC9REFpnIhbl',
    'oQIDAQAB',
    '-----END PUBLIC KEY-----',
  ].join('\n')

  function easypayProvider(config: Record<string, string>) {
    return providerFactory({
      provider_key: 'easypay',
      name: 'EasyPay',
      config: {
        pid: 'pid-1',
        apiBase: 'https://pay.example.com',
        notifyUrl: 'https://example.com/api/v1/payment/webhook/easypay',
        returnUrl: 'https://example.com/payment/result',
        ...config,
      },
      supported_types: ['alipay'],
      payment_mode: 'qrcode',
    })
  }

  it('defaults new EasyPay providers to RSA2 and shows its public-key fields', async () => {
    const wrapper = mountDialog()
    ;(wrapper.vm as unknown as { reset: (providerKey: string) => void }).reset('easypay')
    await nextTick()

    expect((wrapper.find('[data-config-key="gatewaySignType"]').element as HTMLSelectElement).value).toBe('RSA2')
    expect(wrapper.find('[data-config-key="gatewayPublicKey"]').element.tagName).toBe('TEXTAREA')
    expect(wrapper.find('[data-config-key="gatewayKeyId"]').exists()).toBe(true)
    expect(wrapper.text()).toContain(messages['admin.settings.payment.field_gatewaySignTypeHint'])
    wrapper.unmount()
  })

  it('keeps legacy instances on MD5 and preserves unknown existing values without exposing private keys', async () => {
    const provider = easypayProvider({
      legacyOption: 'keep-me',
      merchantPrivateKey: 'must-not-render-or-resubmit',
      rsa_private_key: 'must-not-render-or-resubmit',
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    expect((wrapper.find('[data-config-key="gatewaySignType"]').element as HTMLSelectElement).value).toBe('MD5')
    expect(wrapper.find('[data-config-key="gatewayPublicKey"]').exists()).toBe(false)
    expect(wrapper.find('[data-config-key="gatewayKeyId"]').exists()).toBe(false)
    expect((wrapper.find('[data-config-key="pkey"]').element as HTMLInputElement).type).toBe('password')
    expect(wrapper.text()).not.toContain('must-not-render-or-resubmit')

    await wrapper.find('form').trigger('submit.prevent')
    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.gatewaySignType).toBe('MD5')
    expect(payload.config.legacyOption).toBe('keep-me')
    expect(payload.config.pkey).toBeUndefined()
    expect(payload.config.merchantPrivateKey).toBeUndefined()
    expect(payload.config.rsa_private_key).toBeUndefined()
    wrapper.unmount()
  })

  it('saves RSA2 public key and Key ID without including private material', async () => {
    const provider = easypayProvider({
      gatewaySignType: 'RSA2',
      gatewayPublicKey: rsaPublicKey,
      gatewayKeyId: 'paypro_key_1',
      gatewayPrivateKey: 'must-not-render-or-resubmit',
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    expect(wrapper.find('[data-config-key="gatewayPublicKey"]').element.tagName).toBe('TEXTAREA')
    expect((wrapper.find('[data-config-key="gatewayKeyId"]').element as HTMLInputElement).value).toBe('paypro_key_1')
    expect(wrapper.find('[data-config-key="gatewayPrivateKey"]').exists()).toBe(false)
    await wrapper.find('[data-config-key="gatewayKeyId"]').setValue(' paypro_key_1 ')

    await wrapper.find('form').trigger('submit.prevent')
    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.gatewaySignType).toBe('RSA2')
    expect(payload.config.gatewayPublicKey).toBe(rsaPublicKey)
    expect(payload.config.gatewayKeyId).toBe('paypro_key_1')
    expect(payload.config.gatewayPrivateKey).toBeUndefined()
    expect(payload.config.privateKey).toBeUndefined()
    wrapper.unmount()
  })

  it.each([
    [
      { gatewaySignType: 'RSA2', gatewayPublicKey: '-----BEGIN PRIVATE KEY-----\nAQIDBA==\n-----END PRIVATE KEY-----', gatewayKeyId: 'paypro_key_1' },
      'admin.settings.payment.validationEasyPayRsaPublicKeyInvalid',
    ],
    [
      { gatewaySignType: 'RSA2', gatewayPublicKey: rsaPublicKey, gatewayKeyId: 'invalid/key' },
      'admin.settings.payment.validationEasyPayRsaKeyIdInvalid',
    ],
  ])('rejects invalid RSA2 settings with a specific validation message', async (config, messageKey) => {
    showError.mockClear()
    const wrapper = mountDialog({ editing: easypayProvider(config) })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(easypayProvider(config))
    await nextTick()
    await wrapper.find('form').trigger('submit.prevent')

    expect(wrapper.emitted('save')).toBeUndefined()
    await vi.waitFor(() => expect(showError).toHaveBeenCalledWith(messages[messageKey]))
    wrapper.unmount()
  })
})

describe('PaymentProviderDialog payment guide', () => {
  it('shows no payment guide for providers without a flow guide', () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.alipayGuideSummary'])
    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.wxpayGuideSummary'])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(false)
  })

  it.each([
    ['alipay', 'admin.settings.payment.alipayGuideSummary'],
    ['wxpay', 'admin.settings.payment.wxpayGuideSummary'],
    ['airwallex', 'admin.settings.payment.airwallexGuideSummary'],
  ])('shows the payment guide summary for %s', async (providerKey, summaryKey) => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset(providerKey)
    await nextTick()

    expect(wrapper.text()).toContain(messages[summaryKey])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(true)
  })

  it('shows Airwallex webhook event and API version guidance with the webhook URL', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('airwallex')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.airwallexWebhookHint'])
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/airwallex')
  })

  it('shows Stripe webhook API version guidance with the integrated SDK version', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('stripe')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.stripeWebhookHint'])
    expect(wrapper.text()).toContain(`Use Stripe API version ${STRIPE_SDK_API_VERSION}.`)
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/stripe')
  })

  it('emits an empty Airwallex accountId when the admin clears it', async () => {
    const provider = providerFactory({
      config: {
        clientId: 'cid_123',
        apiBase: 'https://api.airwallex.com/api/v1',
        countryCode: 'CN',
        currency: 'CNY',
        accountId: 'acct_123',
      },
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    const accountIdInput = wrapper
      .findAll('input[type="text"]')
      .find(input => (input.element as HTMLInputElement).value === 'acct_123')
    if (!accountIdInput) throw new Error('accountId input not found')

    await accountIdInput.setValue('')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.accountId).toBe('')
  })

  it.each(['epay', 'usdt.trc20'])('serializes EasyPay upstream type %s and adds the local type to supported_types', async (upstreamType) => {
    const provider = providerFactory({
      provider_key: 'easypay',
      name: 'EasyPay',
      config: {
        pid: 'pid-1',
        apiBase: 'https://pay.example.com',
        notifyUrl: 'https://example.com/api/v1/payment/webhook/easypay',
        returnUrl: 'https://example.com/payment/result',
      },
      supported_types: ['alipay', 'wxpay'],
      payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    await wrapper.find('button.btn-sm').trigger('click')
    await nextTick()

    const inputs = wrapper.findAll('input[type="text"]')
    const customTypeInputs = inputs.filter(input => (input.element as HTMLInputElement).placeholder === 'credit_card')
    const ldcTypeInput = customTypeInputs[0]
    const upstreamTypeInput = customTypeInputs[1]
    const displayNameInput = inputs.find(input => (input.element as HTMLInputElement).placeholder === '信用卡')
    if (!ldcTypeInput || !upstreamTypeInput || !displayNameInput) {
      throw new Error('custom method inputs not found')
    }

    await ldcTypeInput.setValue('ldc')
    await upstreamTypeInput.setValue(upstreamType)
    await displayNameInput.setValue('LDC')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as {
      config: Record<string, string>
      supported_types: string[]
    }
    expect(JSON.parse(payload.config.customMethods)).toEqual([{ type: 'ldc', upstreamType, displayName: 'LDC' }])
    expect(payload.supported_types).toEqual(['alipay', 'wxpay', 'ldc'])
  })

  it.each([
    ['alipay_hk', 'hkpay'],
    ['usdt.trc20', 'usdt.trc20'],
    ['usdt_trc20', 'usdt/trc20'],
  ])('rejects invalid EasyPay mapping %s to %s', async (type, upstreamType) => {
    const provider = providerFactory({
      provider_key: 'easypay',
      name: 'EasyPay',
      config: {
        pid: 'pid-1',
        apiBase: 'https://pay.example.com',
        notifyUrl: 'https://example.com/api/v1/payment/webhook/easypay',
        returnUrl: 'https://example.com/payment/result',
      },
      supported_types: ['alipay', 'wxpay'],
      payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    await wrapper.find('button.btn-sm').trigger('click')
    await nextTick()

    const inputs = wrapper.findAll('input[type="text"]')
    const customTypeInputs = inputs.filter(input => (input.element as HTMLInputElement).placeholder === 'credit_card')
    const typeInput = customTypeInputs[0]
    const upstreamTypeInput = customTypeInputs[1]
    const displayNameInput = inputs.find(input => (input.element as HTMLInputElement).placeholder === '信用卡')
    if (!typeInput || !upstreamTypeInput || !displayNameInput) {
      throw new Error('custom method inputs not found')
    }

    await typeInput.setValue(type)
    await upstreamTypeInput.setValue(upstreamType)
    await displayNameInput.setValue('Custom payment')
    await wrapper.find('form').trigger('submit.prevent')

    expect(wrapper.emitted('save')).toBeUndefined()
  })
})
