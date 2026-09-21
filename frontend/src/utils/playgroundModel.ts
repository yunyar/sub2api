const IMAGE_MODEL_PATTERNS = [
  /(^|[-_.])image([-.]|$)/,
  /(^|[-_.])images([-.]|$)/,
  /dall-e/,
  /imagen/,
  /grok-imagine/,
  /(^|[-_.])flux([-.]|$)/,
  /recraft/,
  /ideogram/
]

export function isPlaygroundImageModel(model: string): boolean {
  const normalized = model.trim().toLowerCase()
  return normalized !== '' && IMAGE_MODEL_PATTERNS.some(pattern => pattern.test(normalized))
}
