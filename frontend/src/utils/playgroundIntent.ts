export type PlaygroundIntent = 'chat' | 'image'

const TEXT_REQUEST = /(?:不要|不用|无需|别)(?:再|帮我|继续)?(?:生图|生成图片|生成图像|画图|绘图)|(?:解释|讲解|分析|描述|评价|总结|识别|翻译)(?:一下|这|那|一|这个|这张|该|图片|图像)|(?:写|优化|修改|改写|提供|设计|生成).{0,20}(?:提示词|prompt)|(?:如何|怎么|为什么|怎样).{0,20}(?:生图|画图|生成图片)|\b(?:explain|describe|analy[sz]e|summari[sz]e|translate|identify)\b|\b(?:write|improve|rewrite|create|generate)\b.{0,30}\bprompt\b|\b(?:do not|don't|no need to)\s+(?:draw|generate|create)\b/i
const IMAGE_REQUEST = /(?:画|绘制|绘画|生图|出图|作画)(?:一|个|张|幅|出|成|一下|些|只|头|座|辆|条|对|猫|狗|人|风景|海|山|我)|(?:生成|制作|设计|创作|帮我做|帮我画|来|给我).{0,30}(?:图片|图像|插画|海报|壁纸|头像|封面|照片|效果图|一张|一幅|logo|icon)|(?:生成|制作|设计|创作).{0,8}(?:图|画)|\b(?:draw|paint|illustrate)\b|\b(?:generate|create|make|design|render)\b.{0,45}\b(?:image|picture|photo|illustration|poster|wallpaper|avatar|logo|icon|artwork|cover)\b/i
const IMAGE_REVISION = /(?:再来|再生成|重画|重新画|换成|改成|换个|换一|改一|更写实|更真实|更亮|更暗|加上|去掉|去除|背景|颜色|构图|画风|风格|分辨率)|\b(?:another|redraw|regenerate|brighter|darker|background|colou?r|style|replace|remove|add)\b/i

export function resolvePlaygroundIntent(prompt: string, previousIntent?: PlaygroundIntent): PlaygroundIntent {
  const text = prompt.trim()
  if (!text || TEXT_REQUEST.test(text)) return 'chat'
  if (/(?:生图|出图|画图|绘图)(?:[：:，,。！!\s]|$)/.test(text) || IMAGE_REQUEST.test(text)) return 'image'
  if (previousIntent === 'image' && IMAGE_REVISION.test(text)) return 'image'
  return 'chat'
}
