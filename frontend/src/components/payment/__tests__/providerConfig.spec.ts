import { describe, expect, it } from 'vitest'
import {
  PAYMENT_CURRENCY_OPTIONS,
  PROVIDER_CONFIG_FIELDS,
  isEasyPayPrivateKeyConfigField,
  isEasyPayRsaPublicKeyPem,
  isValidEasyPayGatewaySignType,
  isValidEasyPayRsaKeyId,
  isBuiltInAlipayMethod,
  isBuiltInWxpayMethod,
  parseEasyPayCustomMethods,
  serializeEasyPayCustomMethods,
} from '@/components/payment/providerConfig'

function findField(providerKey: string, key: string) {
  const fields = PROVIDER_CONFIG_FIELDS[providerKey] || []
  return fields.find(field => field.key === key)
}

describe('PROVIDER_CONFIG_FIELDS.wxpay', () => {
  it('keeps admin form validation aligned with backend-required credentials', () => {
    expect(findField('wxpay', 'publicKeyId')?.optional).toBeFalsy()
    expect(findField('wxpay', 'certSerial')?.optional).toBeFalsy()
  })

  it('only keeps the simplified visible credential set in the admin form', () => {
    expect(findField('wxpay', 'mpAppId')).toBeUndefined()
    expect(findField('wxpay', 'h5AppName')).toBeUndefined()
    expect(findField('wxpay', 'h5AppUrl')).toBeUndefined()
  })
})

describe('PROVIDER_CONFIG_FIELDS.airwallex', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('airwallex', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })

  it('marks accountId as optional and explains when it can be left blank', () => {
    const accountId = findField('airwallex', 'accountId')

    expect(accountId?.optional).toBe(true)
    expect(accountId?.clearable).toBe(true)
    expect(accountId?.hintKey).toBe('admin.settings.payment.field_accountIdHint')
  })

  it('explains that apiBase must match the Airwallex key environment', () => {
    expect(findField('airwallex', 'apiBase')?.hintKey).toBe('admin.settings.payment.field_airwallexApiBaseHint')
  })
})

describe('PROVIDER_CONFIG_FIELDS.stripe', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('stripe', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })
})

describe('PROVIDER_CONFIG_FIELDS.easypay RSA2', () => {
  it('keeps legacy MD5 as the edit fallback and recommends RSA2 for new instances in the dialog', () => {
    expect(findField('easypay', 'gatewaySignType')?.defaultValue).toBe('MD5')
    expect(findField('easypay', 'gatewaySignType')?.options?.map(option => option.value)).toEqual(['MD5', 'RSA2'])
    expect(findField('easypay', 'gatewayPublicKey')).toMatchObject({
      optional: true,
      multiline: true,
      sensitive: false,
    })
    expect(findField('easypay', 'gatewayKeyId')).toMatchObject({ optional: true, sensitive: false })
    expect(findField('easypay', 'pkey')?.sensitive).toBe(true)
    expect(findField('easypay', 'privateKey')).toBeUndefined()
  })

  it('validates the supported sign modes, Key ID syntax, and public PEM structure', () => {
    expect(isValidEasyPayGatewaySignType('MD5')).toBe(true)
    expect(isValidEasyPayGatewaySignType('RSA2')).toBe(true)
    expect(isValidEasyPayGatewaySignType('RSA')).toBe(false)
    expect(isValidEasyPayRsaKeyId('paypro_key-1')).toBe(true)
    expect(isValidEasyPayRsaKeyId('bad/key')).toBe(false)
    expect(isValidEasyPayRsaKeyId('x'.repeat(65))).toBe(false)
    expect(isEasyPayRsaPublicKeyPem(
      '-----BEGIN PUBLIC KEY-----\nAQIDBA==\n-----END PUBLIC KEY-----',
    )).toBe(true)
    expect(isEasyPayRsaPublicKeyPem(
      '-----BEGIN PRIVATE KEY-----\nAQIDBA==\n-----END PRIVATE KEY-----',
    )).toBe(false)
    expect(isEasyPayRsaPublicKeyPem(
      '-----BEGIN PUBLIC KEY-----\nAQID BA==\n-----END PUBLIC KEY-----',
    )).toBe(false)
    expect(isEasyPayRsaPublicKeyPem('not-a-pem')).toBe(false)
  })

  it('recognizes private-key-like EasyPay config names for exclusion', () => {
    expect(isEasyPayPrivateKeyConfigField('merchantPrivateKey')).toBe(true)
    expect(isEasyPayPrivateKeyConfigField('rsa_private_key')).toBe(true)
    expect(isEasyPayPrivateKeyConfigField('pkey')).toBe(false)
    expect(isEasyPayPrivateKeyConfigField('gatewayKeyId')).toBe(false)
  })
})

describe('EasyPay custom methods config', () => {
  it('parses customMethods from the JSON string stored in provider config', () => {
    expect(parseEasyPayCustomMethods(
      '[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":"USDT-TRC20"}]',
    )).toEqual([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: 'USDT-TRC20' },
    ])
  })

  it('serializes non-empty custom methods into the config string format', () => {
    expect(serializeEasyPayCustomMethods([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: '  ', upstreamType: 'ignored', displayName: 'Ignored' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: '' },
    ])).toBe('[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":""}]')
  })

  it('returns an empty string for invalid or empty custom methods', () => {
    expect(parseEasyPayCustomMethods('not-json')).toEqual([])
    expect(serializeEasyPayCustomMethods([{ type: '', upstreamType: 'epay', displayName: 'LDC' }])).toBe('')
  })
})

describe('built-in payment method helpers', () => {
  it('only treats exact built-in aliases as Alipay or WeChat Pay', () => {
    expect(isBuiltInAlipayMethod('alipay')).toBe(true)
    expect(isBuiltInAlipayMethod('alipay_direct')).toBe(true)
    expect(isBuiltInAlipayMethod('card_alipay')).toBe(false)

    expect(isBuiltInWxpayMethod('wxpay')).toBe(true)
    expect(isBuiltInWxpayMethod('wxpay_direct')).toBe(true)
    expect(isBuiltInWxpayMethod('card_wxpay')).toBe(false)
  })
})
