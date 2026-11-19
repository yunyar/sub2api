(() => {
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
const fine = matchMedia('(pointer: fine)').matches;
$('#yr').textContent = new Date().getFullYear();

/* ---------- 流动粒子：善意之流 ---------- */
const cv = $('#flow'), ctx = cv.getContext('2d');
let W, H, DPR, parts = [], mouse = {x: -9999, y: -9999};
const COLORS = ['255,154,107', '255,127,166', '179,140,255', '95,200,232', '95,211,174', '255,205,90'];
function resize(){
  DPR = Math.min(devicePixelRatio || 1, 2);
  W = cv.width = innerWidth * DPR; H = cv.height = innerHeight * DPR;
  cv.style.width = innerWidth + 'px'; cv.style.height = innerHeight + 'px';
  const n = Math.min(140, Math.floor(innerWidth * innerHeight / 11000));
  parts = Array.from({length: n}, () => newP(true));
}
function newP(rand){
  return {x: Math.random() * W, y: rand ? Math.random() * H : H + 20,
    r: (Math.random() * 2.2 + .8) * DPR, c: COLORS[Math.random() * COLORS.length | 0],
    a: Math.random() * .5 + .25, life: 0, seed: Math.random() * 1000};
}
function field(x, y, t){
  // 平滑的伪噪声流场
  const s = .0016 / DPR;
  return Math.sin(x * s + t * .0003) * Math.cos(y * s * 1.3 - t * .0002) * Math.PI * 1.2 - Math.PI / 2;
}
let last = 0;
function tick(t){
  ctx.clearRect(0, 0, W, H);
  for (const p of parts){
    const ang = field(p.x, p.y, t + p.seed);
    let vx = Math.cos(ang) * .55 * DPR, vy = Math.sin(ang) * .55 * DPR - .25 * DPR;
    const dx = p.x - mouse.x * DPR, dy = p.y - mouse.y * DPR, d2 = dx * dx + dy * dy, R = 140 * DPR;
    if (d2 < R * R){ const f = (1 - Math.sqrt(d2) / R) * 2.2; vx += dx / Math.sqrt(d2 + 1) * f; vy += dy / Math.sqrt(d2 + 1) * f; }
    p.x += vx; p.y += vy; p.life++;
    if (p.y < -20 || p.x < -20 || p.x > W + 20) Object.assign(p, newP(false), {x: Math.random() * W});
    const tw = .6 + .4 * Math.sin(t * .002 + p.seed);
    ctx.beginPath(); ctx.arc(p.x, p.y, p.r, 0, 6.283);
    ctx.fillStyle = `rgba(${p.c},${p.a * tw})`; ctx.shadowColor = `rgba(${p.c},.8)`; ctx.shadowBlur = 10 * DPR; ctx.fill();
  }
  // 粒子之间的善意连线
  ctx.shadowBlur = 0;
  for (let i = 0; i < parts.length; i++) for (let j = i + 1; j < parts.length; j++){
    const a = parts[i], b = parts[j], dx = a.x - b.x, dy = a.y - b.y, d = dx * dx + dy * dy, L = (90 * DPR) ** 2;
    if (d < L){ ctx.strokeStyle = `rgba(${a.c},${(1 - d / L) * .18})`; ctx.lineWidth = DPR; ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke(); }
  }
  if (!reduce) requestAnimationFrame(tick);
}
resize(); addEventListener('resize', resize); requestAnimationFrame(tick);

/* ---------- 光标光晕 & 玻璃高光 ---------- */
const cur = $('#cursor');
addEventListener('pointermove', e => {
  mouse.x = e.clientX; mouse.y = e.clientY;
  if (fine) cur.style.transform = `translate(${e.clientX - 210}px,${e.clientY - 210}px)`;
  const el = e.target.closest && e.target.closest('.glass');
  if (el){ const r = el.getBoundingClientRect(); el.style.setProperty('--mx', e.clientX - r.left + 'px'); el.style.setProperty('--my', e.clientY - r.top + 'px'); }
}, {passive: true});
if (!fine) cur.style.display = 'none'; else cur.style.left = cur.style.top = '0';

/* ---------- 花朵：善意盛开 ---------- */
const P = {
  round: (l, w) => `M0 0C${w} ${-l*.25} ${w*1.05} ${-l*.85} 0 ${-l}C${-w*1.05} ${-l*.85} ${-w} ${-l*.25} 0 0Z`,
  notch: (l, w) => `M0 0C${w} ${-l*.3} ${w*1.1} ${-l*.9} ${w*.32} ${-l}Q${w*.12} ${-l*.99} 0 ${-l*.86}Q${-w*.12} ${-l*.99} ${-w*.32} ${-l}C${-w*1.1} ${-l*.9} ${-w} ${-l*.3} 0 0Z`,
  point: (l, w) => `M0 0C${w} ${-l*.35} ${w*.75} ${-l*.8} 0 ${-l}C${-w*.75} ${-l*.8} ${-w} ${-l*.35} 0 0Z`,
  slim:  (l, w) => `M0 0C${w} ${-l*.2} ${w} ${-l*.9} 0 ${-l}C${-w} ${-l*.9} ${-w} ${-l*.2} 0 0Z`,
};
const SPECIES = {
  sakura: {sp: 60, layers: [{n:5, len:40, w:20, shape:'notch', c:['#ffeef3','#ffc4d6','#ff9fbd'], vein:'#ff8fab'}],
    center: {r:6, c:['#fff3c4','#ffb35c'], stamens:14, sl:17, sc:'#ff8fab', sd:'#ffcf6a'}},
  daisy: {sp: 90, layers: [{n:18, len:43, w:6, shape:'slim', c:['#fff1e6','#ffffff','#fff4f7']}, {n:14, len:33, w:5.5, shape:'slim', rot:10, c:['#ffeedd','#ffffff','#fffaf3']}],
    center: {r:12, c:['#ffe78f','#ffb43b'], dots:46, dc:'#d98a14', dr:.9}},
  cosmos: {sp: 70, layers: [{n:8, len:43, w:15, shape:'notch', c:['#f8f2ff','#dccdff','#b39cff'], vein:'#9b84f5'}],
    center: {r:8, c:['#fff3ad','#ffc24d'], dots:24, dc:'#e09a1c', dr:1}},
  lotus: {sp: 120, layers: [{n:8, len:45, w:15, shape:'point', c:['#fff5f8','#ffd0de','#ff9fbd'], vein:'#ff8fab'}, {n:8, len:33, w:12, shape:'point', rot:22.5, c:['#fffafc','#ffe3ec','#ffbfd2']}, {n:6, len:21, w:9, shape:'point', c:['#ffffff','#fff0f5','#ffd6e3']}],
    center: {r:6, c:['#fff6c9','#ffd36b'], dots:10, dc:'#e3a83a', dr:.8}},
  plumeria: {sp: 80, layers: [{n:5, len:40, w:17, shape:'round', shift:6, tw:-14, c:['#ffc93f','#fff3d6','#ffffff']}],
    center: {r:4, c:['#ffe27a','#ffc23a']}},
  ranunculus: {sp: 140, layers: [{n:7, len:41, w:20, shape:'round', c:['#ffd9c2','#ffc3a1','#ff9e7a']}, {n:6, len:31, w:16, shape:'round', rot:25, c:['#ffe6d6','#ffcfb3','#ffae8a']}, {n:5, len:21, w:12, shape:'round', rot:10, c:['#fff0e6','#ffdcc6','#ffbe9e']}],
    center: {r:5, c:['#ffe8a8','#f0a35c']}},
  forget: {sp: 65, layers: [{n:5, len:34, w:18, shape:'round', c:['#f4f9ff','#bcdcff','#8ec5ff'], vein:'#6fb0f5'}],
    center: {r:7, c:['#fff6c2','#ffd25a'], ring:'#fff'}},
};
let fid = 0;
function flower(name){
  const sp = SPECIES[name], id = 'f' + (fid++);
  let defs = '', body = '';
  sp.layers.forEach((L, li) => {
    const g = id + 'l' + li;
    defs += `<linearGradient id="${g}" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="0" y2="${-L.len}"><stop offset="0" stop-color="${L.c[0]}"/><stop offset=".55" stop-color="${L.c[1]}"/><stop offset="1" stop-color="${L.c[2]}"/></linearGradient>`;
    let petals = '';
    for (let k = 0; k < L.n; k++){
      const a = 360 / L.n * k + (L.rot || 0);
      petals += `<g transform="rotate(${a}) translate(${L.shift || 0} 0) rotate(${L.tw || 0})"><path d="${P[L.shape](L.len, L.w)}" fill="url(#${g})" stroke="rgba(255,255,255,.75)" stroke-width=".6"/>` +
        (L.vein ? `<path d="M0 ${-L.len * .14}V${-L.len * .68}" stroke="${L.vein}" stroke-width=".7" stroke-linecap="round" opacity=".35"/>` : '') + `</g>`;
    }
    body += `<g class="layer" style="--ld:${-li * 1.3}s">${petals}</g>`;
  });
  const C = sp.center;
  defs += `<radialGradient id="${id}c" cx=".38" cy=".32"><stop offset="0" stop-color="${C.c[0]}"/><stop offset="1" stop-color="${C.c[1]}"/></radialGradient>`;
  let core = '';
  if (C.stamens) for (let k = 0; k < C.stamens; k++)
    core += `<g transform="rotate(${360 / C.stamens * k + (k % 2) * 8})"><path d="M0 0V${-C.sl * (k % 2 ? .78 : 1)}" stroke="${C.sc}" stroke-width=".7" opacity=".8"/><circle cy="${-C.sl * (k % 2 ? .78 : 1)}" r="1.5" fill="${C.sd}"/></g>`;
  core += `<circle r="${C.r}" fill="url(#${id}c)"${C.ring ? ` stroke="${C.ring}" stroke-width="1.6"` : ''}/>`;
  if (C.dots) for (let k = 1; k <= C.dots; k++){
    const a = k * 2.39996, rr = Math.sqrt(k / C.dots) * C.r * .82;
    core += `<circle cx="${(Math.cos(a) * rr).toFixed(2)}" cy="${(Math.sin(a) * rr).toFixed(2)}" r="${C.dr}" fill="${C.dc}" opacity=".55"/>`;
  }
  core += `<ellipse cx="${-C.r * .3}" cy="${-C.r * .35}" rx="${C.r * .35}" ry="${C.r * .22}" fill="#fff" opacity=".55"/>`;
  return `<svg viewBox="0 0 100 100"><defs>${defs}</defs><g transform="translate(50 50)"><g class="spin" style="--sp:${sp.sp}s">${body}<g class="core">${core}</g></g></g></svg>`;
}
const cast = [
  ['sakura',     8, 20, 124, -8],
  ['daisy',     83, 15, 116,  6],
  ['ranunculus',12, 66, 108,  5],
  ['lotus',     79, 61, 138, -4],
  ['forget',    46, 83,  74,  3],
  ['cosmos',    66,  6,  90,  0],
  ['plumeria',  29,  5,  80,  8],
];
const host = $('#critters');
cast.forEach(([name, x, y, size, r], i) => {
  const el = document.createElement('div');
  el.className = 'bloom';
  el.style.cssText = `left:${x}%;top:${y}%;--s:${size}px;--d:${6 + i * .7}s;--r:${r}deg;--dl:${-i * .9}s;--od:${.2 + i * .12}s`;
  el.innerHTML = flower(name);
  el.addEventListener('click', e => {
    el.querySelector('svg').animate([{rotate:'0deg', scale:1}, {rotate:'200deg', scale:1.2}, {rotate:'360deg', scale:1}], {duration:1200, easing:'cubic-bezier(.2,.8,.2,1)'});
    petals(e.clientX, e.clientY, 14, SPECIES[name].layers[0].c);
  });
  host.appendChild(el);
});
// 视差
const crits = $$('.bloom');
addEventListener('scroll', () => {
  const y = scrollY;
  crits.forEach((el, i) => el.style.translate = `0 ${-y * (.15 + i * .05)}px`);
}, {passive: true});
// 小花点缀（环绕、头像）
$$('[data-f]').forEach(el => el.innerHTML = flower(el.dataset.f));
// 飘落的花瓣
const RAIN = [['#fff1f5','#ffb8cc'],['#fff','#ffd3de'],['#fff4ec','#ffc7a8'],['#f6f0ff','#d4c4ff']];
const rain = $('#rain');
if (!reduce) for (let i = 0; i < 16; i++){
  const [a, b] = RAIN[i % RAIN.length], d = document.createElement('i');
  d.style.cssText = `--x:${Math.random() * 100}%;--w:${8 + Math.random() * 9}px;--t:${11 + Math.random() * 10}s;--dl:${-Math.random() * 20}s;--sx:${(Math.random() - .5) * 140}px;--a:${a};--b:${b}`;
  rain.appendChild(d);
}
// 点击 / 双击：花瓣绽放
function petals(x, y, n = 12, cols){
  for (let i = 0; i < n; i++){
    const c = cols || RAIN[Math.random() * RAIN.length | 0];
    const p = document.createElement('span'); p.className = 'petal';
    p.style.setProperty('--a', c[0]); p.style.setProperty('--b', c[c.length - 1]);
    p.style.setProperty('--w', 9 + Math.random() * 9 + 'px');
    document.body.appendChild(p);
    const ang = Math.random() * Math.PI * 2, dist = 40 + Math.random() * 90;
    const bx = Math.cos(ang) * dist, by = Math.sin(ang) * dist * .7 - 30;
    const fx = bx + (Math.random() - .5) * 120, fy = by + 160 + Math.random() * 160, rot = (Math.random() - .5) * 720;
    const base = `translate(${x}px,${y}px) translate(-50%,-50%)`;
    p.animate([
      {transform: `${base} scale(.2) rotate(0deg)`, opacity: 0},
      {transform: `${base} translate(${bx}px,${by}px) scale(1) rotate(${rot * .3}deg) rotateX(60deg)`, opacity: 1, offset: .25},
      {transform: `${base} translate(${(bx + fx) / 2 + 20}px,${(by + fy) / 2}px) rotate(${rot * .65}deg) rotateX(220deg)`, opacity: .9, offset: .6},
      {transform: `${base} translate(${fx}px,${fy}px) rotate(${rot}deg) rotateX(400deg)`, opacity: 0}
    ], {duration: 2000 + Math.random() * 1200, easing: 'cubic-bezier(.25,.7,.35,1)', delay: i * 18, fill: 'both'}).onfinish = () => p.remove();
  }
}

/* ---------- 巨型莲花随滚动绽放 ---------- */
const giant = $('#giant');
SPECIES.lotusGiant = {sp: 1, layers: [
  {n:12, len:48, w:13, shape:'point', c:['#fff3f7','#ffc9da','#ff9fbd'], vein:'#ff8fab'},
  {n:10, len:40, w:12, shape:'point', rot:18, c:['#fff7fa','#ffd9e5','#ffb2c8'], vein:'#ff9fbd'},
  {n:8,  len:30, w:11, shape:'point', rot:8,  c:['#fffbfd','#ffe7ef','#ffc6d6']},
  {n:6,  len:19, w:9,  shape:'point', rot:30, c:['#ffffff','#fff1f6','#ffd6e3']}],
  center: {r:7, c:['#fff6c9','#ffcf5a'], dots:18, dc:'#e3a83a', dr:.8}};
giant.innerHTML = flower('lotusGiant');
const gLayers = $$('.layer', giant), gSpin = $('.spin', giant), gCore = $('.core', giant);
function bloomGiant(){
  const r = giant.parentElement.getBoundingClientRect(), vh = innerHeight;
  const k = Math.max(0, Math.min(1, (vh - r.top) / (r.height + vh * .2)));
  const e = 1 - Math.pow(1 - k, 2);
  gLayers.forEach((L, i) => {
    const li = Math.max(0, Math.min(1, e * 1.5 - i * .14));
    L.style.transform = `rotate(${(1 - li) * (40 + i * 25)}deg) scale(${.12 + li * .88})`;
    L.style.opacity = Math.min(1, li * 1.6);
  });
  gSpin.style.transform = `rotate(${k * 60}deg)`;
  gCore.style.transform = `scale(${.3 + e * .7})`;
}
addEventListener('scroll', bloomGiant, {passive: true}); bloomGiant();

/* ---------- 导航 ---------- */
const nav = $('#nav');
addEventListener('scroll', () => nav.classList.toggle('scrolled', scrollY > 40), {passive: true});
const burger = $('#burger');
const setMenu = open => { nav.classList.toggle('open', open); burger.setAttribute('aria-expanded', open); };
burger.addEventListener('click', () => setMenu(!nav.classList.contains('open')));
$$('#mmenu a').forEach(a => a.addEventListener('click', () => setMenu(false)));
addEventListener('scroll', () => { if (nav.classList.contains('open') && Math.abs(scrollY - (setMenu.y ?? scrollY)) > 60) setMenu(false); setMenu.y = scrollY; }, {passive: true});

/* ---------- 进场动画 ---------- */
const io = new IntersectionObserver(es => es.forEach(e => { if (e.isIntersecting){ e.target.classList.add('in'); io.unobserve(e.target); } }), {threshold: .12, rootMargin: '0px 0px -40px'});
$$('.reveal').forEach(el => io.observe(el));

/* ---------- 数字滚动 ---------- */
const cio = new IntersectionObserver(es => es.forEach(e => {
  if (!e.isIntersecting) return; cio.unobserve(e.target);
  const el = e.target, to = +el.dataset.count, dec = +(el.dataset.dec || 0), suf = el.dataset.suf || '', t0 = performance.now(), dur = 1800;
  const step = now => { const k = Math.min(1, (now - t0) / dur), v = to * (1 - Math.pow(1 - k, 4)); el.textContent = v.toFixed(dec) + suf; if (k < 1) requestAnimationFrame(step); };
  requestAnimationFrame(step);
}), {threshold: .5});
$$('[data-count]').forEach(el => cio.observe(el));

/* ---------- 理念文字随滚动点亮 ---------- */
const man = $('#manifesto');
(function split(){
  const out = [];
  man.childNodes.forEach(n => {
    const hl = n.nodeType === 1, txt = n.textContent;
    [...txt].forEach(ch => out.push(`<span class="w${hl ? ' hl' : ''}">${ch}</span>`));
  });
  man.innerHTML = out.join('');
})();
const ws = $$('.w', man);
function lightUp(){
  const r = man.getBoundingClientRect(), vh = innerHeight;
  const k = Math.max(0, Math.min(1, (vh * .85 - r.top) / (r.height + vh * .35)));
  const n = Math.floor(k * ws.length);
  ws.forEach((w, i) => w.classList.toggle('on', i < n));
}
addEventListener('scroll', lightUp, {passive: true}); lightUp();

/* ---------- 3D 倾斜 ---------- */
if (fine && !reduce) $$('.tilt').forEach(el => {
  el.addEventListener('pointermove', e => {
    const r = el.getBoundingClientRect(), px = (e.clientX - r.left) / r.width - .5, py = (e.clientY - r.top) / r.height - .5;
    el.style.transform = `perspective(1000px) rotateX(${-py * 6}deg) rotateY(${px * 6}deg) translateZ(0)`;
  });
  el.addEventListener('pointerleave', () => el.style.transform = '');
});

/* ---------- 磁吸按钮 ---------- */
if (fine && !reduce) $$('.magnetic').forEach(el => {
  el.addEventListener('pointermove', e => { const r = el.getBoundingClientRect(); el.style.transform = `translate(${(e.clientX - r.left - r.width / 2) * .25}px,${(e.clientY - r.top - r.height / 2) * .35}px)`; });
  el.addEventListener('pointerleave', () => el.style.transform = '');
});

/* ---------- 均衡器条 ---------- */
const bars = $('#bars');
for (let i = 0; i < 14; i++){ const b = document.createElement('i'); b.style.animationDelay = (-Math.random() * 1.8) + 's'; b.style.animationDuration = (1.2 + Math.random()) + 's'; bars.appendChild(b); }

/* ---------- 打字机终端 ---------- */
const BASE = location.origin.startsWith('http') ? location.origin : 'https://magicai.click';
const snippets = {
  curl: {title: 'terminal — zsh', code:
`<span class="c"># 一行命令，问候整个世界</span>
<span class="f">curl</span> ${BASE}/v1/chat/completions \\
  -H <span class="s">"Authorization: Bearer $HAPPY_KEY"</span> \\
  -H <span class="s">"Content-Type: application/json"</span> \\
  -d <span class="s">'{
    "model": "claude-sonnet-4-5",
    "messages": [{"role":"user","content":"你好，世界 🌱"}],
    "stream": true
  }'</span>`},
  py: {title: 'hello.py — python', code:
`<span class="k">from</span> openai <span class="k">import</span> OpenAI

client = <span class="f">OpenAI</span>(
    base_url=<span class="s">"${BASE}/v1"</span>,
    api_key=<span class="s">"sk-happy-..."</span>,
)

resp = client.chat.completions.<span class="f">create</span>(
    model=<span class="s">"gpt-4o"</span>,
    messages=[{<span class="s">"role"</span>: <span class="s">"user"</span>, <span class="s">"content"</span>: <span class="s">"写一首关于森林的诗"</span>}],
)
<span class="f">print</span>(resp.choices[<span class="n">0</span>].message.content)`},
  js: {title: 'index.mjs — node', code:
`<span class="k">import</span> Anthropic <span class="k">from</span> <span class="s">"@anthropic-ai/sdk"</span>;

<span class="k">const</span> happy = <span class="k">new</span> <span class="f">Anthropic</span>({
  baseURL: <span class="s">"${BASE}"</span>,
  apiKey: process.env.<span class="n">HAPPY_KEY</span>,
});

<span class="k">const</span> msg = <span class="k">await</span> happy.messages.<span class="f">create</span>({
  model: <span class="s">"claude-sonnet-4-5"</span>,
  max_tokens: <span class="n">1024</span>,
  messages: [{ role: <span class="s">"user"</span>, content: <span class="s">"万物可爱，对吗？"</span> }],
});`}
};
const codeEl = $('#code'); let typer, typed = false;
function type(key){
  clearTimeout(typer);
  const {title, code} = snippets[key]; $('#termTitle').textContent = title;
  // 将 HTML 拆为 tokens：标签整体输出，文字逐字输出
  const tokens = code.match(/<[^>]+>|&[a-z]+;|[\s\S]/g); let i = 0, html = '';
  const step = () => {
    let burst = 2 + (Math.random() * 3 | 0);
    while (burst-- && i < tokens.length){ html += tokens[i]; if (tokens[i][0] === '<') burst++; i++; }
    codeEl.innerHTML = html + '<span class="caret"></span>';
    if (i < tokens.length) typer = setTimeout(step, 16);
  };
  if (reduce){ codeEl.innerHTML = code; return; }
  step();
}
$$('#tabs button').forEach(b => b.addEventListener('click', () => { $$('#tabs button').forEach(x => x.classList.toggle('on', x === b)); type(b.dataset.k); }));
new IntersectionObserver((es, o) => { if (es[0].isIntersecting && !typed){ typed = true; type('curl'); o.disconnect(); } }, {threshold: .3}).observe(codeEl);

/* ---------- 模型花园跑马灯 ---------- */
const models = [
  ['Claude Opus', '#d97757', '✳︎'], ['Claude Sonnet', '#e09a72', '✳︎'], ['GPT-5', '#10a37f', '◎'], ['GPT-4o', '#19c37d', '◎'],
  ['Gemini 2.5 Pro', '#4f8df5', '✦'], ['Gemini Flash', '#7aa8ff', '✦'], ['DeepSeek', '#4d6bfe', '◆'], ['Qwen', '#7b61ff', '❖'],
  ['Kimi', '#1d1d1f', '◐'], ['GLM', '#3a7bfd', '⬡'], ['Grok', '#333', '✕'], ['Llama', '#0866ff', '▲'],
  ['Mistral', '#ff7000', '▦'], ['Doubao', '#2b6bff', '◉'], ['MiniMax', '#e8485b', '◈'], ['Embedding', '#5fd3ae', '∿']
];
const chip = ([n, c, s]) => `<div class="chip glass"><i style="background:${c}">${s}</i>${n}</div>`;
const half = Math.ceil(models.length / 2);
const row1 = models.slice(0, half).map(chip).join(''), row2 = models.slice(half).map(chip).join('');
$('#t1').innerHTML = row1 + row1 + row1 + row1; $('#t2').innerHTML = row2 + row2 + row2 + row2;

/* ---------- 双击任意处撒播善意 ---------- */
addEventListener('dblclick', e => { if (!e.target.closest('a,button,pre,summary')) petals(e.clientX, e.clientY, 16); });
})();
