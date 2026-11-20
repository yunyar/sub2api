/*!
 * Happy · 站内页面主题脚本
 * 极光背景 / 飘落花瓣 / Logo 花朵 / 首页回跳；鼠标生花见下方 bloom 部分。
 */
(() => {
  if (window.__happyTheme) return;
  window.__happyTheme = true;
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;

  /* ---------- 花朵生成（与首页同一套） ---------- */
  const P = {
    round: (l, w) => `M0 0C${w} ${-l*.25} ${w*1.05} ${-l*.85} 0 ${-l}C${-w*1.05} ${-l*.85} ${-w} ${-l*.25} 0 0Z`,
    notch: (l, w) => `M0 0C${w} ${-l*.3} ${w*1.1} ${-l*.9} ${w*.32} ${-l}Q${w*.12} ${-l*.99} 0 ${-l*.86}Q${-w*.12} ${-l*.99} ${-w*.32} ${-l}C${-w*1.1} ${-l*.9} ${-w} ${-l*.3} 0 0Z`,
    point: (l, w) => `M0 0C${w} ${-l*.35} ${w*.75} ${-l*.8} 0 ${-l}C${-w*.75} ${-l*.8} ${-w} ${-l*.35} 0 0Z`,
  };
  const LOGO = {
    layers: [
      {n: 5, len: 44, w: 21, shape: 'notch', c: ['#ffe9ef', '#ffb3c8', '#ff8fab']},
      {n: 5, len: 28, w: 13, shape: 'round', rot: 36, c: ['#fff6f8', '#ffd6e1', '#ffc0d1']},
    ],
    center: {r: 8, c: ['#fff3c4', '#ffbf47']},
  };
  let fid = 0;
  function flower(sp) {
    const id = 'hpf' + (fid++);
    let defs = '', body = '';
    sp.layers.forEach((L, li) => {
      const g = id + 'l' + li;
      defs += `<linearGradient id="${g}" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="0" y2="${-L.len}"><stop offset="0" stop-color="${L.c[0]}"/><stop offset=".55" stop-color="${L.c[1]}"/><stop offset="1" stop-color="${L.c[2]}"/></linearGradient>`;
      let petals = '';
      for (let k = 0; k < L.n; k++) petals += `<path transform="rotate(${360 / L.n * k + (L.rot || 0)})" d="${P[L.shape](L.len, L.w)}" fill="url(#${g})" stroke="rgba(255,255,255,.8)" stroke-width=".8"/>`;
      body += `<g class="layer" style="animation-delay:${-li * 1.4}s">${petals}</g>`;
    });
    const C = sp.center;
    defs += `<radialGradient id="${id}c" cx=".38" cy=".32"><stop offset="0" stop-color="${C.c[0]}"/><stop offset="1" stop-color="${C.c[1]}"/></radialGradient>`;
    const core = `<circle r="${C.r}" fill="url(#${id}c)" stroke="#fff" stroke-width="1.4"/><ellipse cx="${-C.r * .3}" cy="${-C.r * .35}" rx="${C.r * .35}" ry="${C.r * .22}" fill="#fff" opacity=".6"/>`;
    return `<svg viewBox="0 0 100 100" aria-hidden="true"><defs>${defs}</defs><g transform="translate(50 50)"><g class="spin">${body}${core}</g></g></svg>`;
  }

  /* ---------- 背景 ---------- */
  function decorate() {
    if (document.querySelector('.hp-aurora')) return;
    const a = document.createElement('div');
    a.className = 'hp-aurora'; a.innerHTML = '<i></i><i></i><i></i><i></i>';
    const g = document.createElement('div'); g.className = 'hp-grain';
    document.body.prepend(a, g);
    if (reduce) return;
    const rain = document.createElement('div'); rain.className = 'hp-rain';
    const COL = [['#fff1f5', '#ffb8cc'], ['#ffffff', '#ffd3de'], ['#fff4ec', '#ffc7a8'], ['#f6f0ff', '#d4c4ff']];
    for (let i = 0; i < 9; i++) {
      const [c1, c2] = COL[i % COL.length], d = document.createElement('i');
      d.style.cssText = `--x:${Math.random() * 100}%;--w:${7 + Math.random() * 7}px;--t:${16 + Math.random() * 14}s;--dl:${-Math.random() * 30}s;--sx:${(Math.random() - .5) * 140}px;--a:${c1};--b:${c2}`;
      rain.appendChild(d);
    }
    document.body.prepend(rain);
  }

  /* ---------- Logo 替换为花 ---------- */
  function flowerLogos() {
    document.querySelectorAll('#app img[src="/logo.svg"]:not([data-hp])').forEach(img => {
      img.dataset.hp = '1';
      img.style.display = 'none';
      const s = document.createElement('span');
      s.className = 'hp-logo-flower';
      s.innerHTML = flower(LOGO);
      img.after(s);
    });
  }

  /* ---------- 站内「首页」回到全新落地页 ---------- */
  const isHome = p => p === '/' || p === '/home';
  function guardHome() {
    if (!isHome(location.pathname) || !document.getElementById('app')) return;
    // 防循环：若服务器未把 / 指向落地页（如本地预览），5 秒内只跳一次
    const last = +sessionStorage.getItem('hp-home-jump') || 0;
    if (Date.now() - last < 5000) return;
    sessionStorage.setItem('hp-home-jump', Date.now());
    location.replace('/');
  }
  for (const k of ['pushState', 'replaceState']) {
    const orig = history[k];
    history[k] = function (st, t, url) {
      const r = orig.apply(this, arguments);
      if (url != null && isHome(new URL(url, location.href).pathname)) guardHome();
      return r;
    };
  }
  addEventListener('popstate', guardHome);

  function boot() {
    decorate();
    flowerLogos();
    new MutationObserver(flowerLogos).observe(document.getElementById('app') || document.body, {childList: true, subtree: true});
  }
  if (document.body) boot(); else addEventListener('DOMContentLoaded', boot);
})();

/*!
 * Happy · 控制台增强（登录后页面）
 * 侧边栏流动指示器 / 页面过场 / 仪表盘问候横幅 / 数字滚动 / 卡片聚光与倾斜 /
 * 按钮花瓣 / 复制撒花 / 弹窗弹性入场 / 空状态花朵
 * 原则：不替换 Vue 管理的节点，只追加自己的元素；数字动画只改同一个文本节点，Vue 一旦更新立即让位。
 */
(() => {
  if (window.__happyConsole) return;
  window.__happyConsole = true;
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const fine = matchMedia('(pointer: fine)').matches;
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const raf = fn => requestAnimationFrame(fn);

  /* ================= 花瓣迸发 ================= */
  const PETAL = [['#fff1f5', '#ffb8cc'], ['#ffffff', '#ffd3de'], ['#fff4ec', '#ffc7a8'], ['#f6f0ff', '#d4c4ff'], ['#fffbe8', '#ffe08a']];
  function burst(x, y, n = 8, spread = 1) {
    if (reduce) return;
    for (let i = 0; i < n; i++) {
      const [a, b] = PETAL[Math.random() * PETAL.length | 0];
      const p = document.createElement('span');
      p.className = 'hp-petal';
      p.style.cssText = `--a:${a};--b:${b};--w:${7 + Math.random() * 8}px`;
      document.body.appendChild(p);
      const ang = Math.random() * Math.PI * 2, d = (30 + Math.random() * 70) * spread;
      const bx = Math.cos(ang) * d, by = Math.sin(ang) * d * .7 - 24 * spread;
      const fx = bx + (Math.random() - .5) * 90, fy = by + 110 + Math.random() * 120, rot = (Math.random() - .5) * 640;
      const base = `translate(${x}px,${y}px) translate(-50%,-50%)`;
      p.animate([
        {transform: `${base} scale(.2)`, opacity: 0},
        {transform: `${base} translate(${bx}px,${by}px) rotate(${rot * .3}deg) rotateX(60deg)`, opacity: 1, offset: .25},
        {transform: `${base} translate(${fx}px,${fy}px) rotate(${rot}deg) rotateX(380deg)`, opacity: 0}
      ], {duration: 1500 + Math.random() * 900, easing: 'cubic-bezier(.25,.7,.35,1)', delay: i * 14, fill: 'both'}).onfinish = () => p.remove();
    }
  }
  window.happyBurst = burst;

  /* ================= 侧边栏：流动指示器 ================= */
  let pill, pillNav;
  function placePill(instant) {
    const nav = $('#app .sidebar-nav');
    if (!nav) return;
    if (pillNav !== nav) {
      pillNav = nav;
      pill = document.createElement('div');
      pill.className = 'hp-nav-pill';
      pill.innerHTML = '<i class="hp-nav-bud"></i>';
      nav.appendChild(pill);
      nav.classList.add('hp-has-pill');
      nav.addEventListener('scroll', () => placePill(true), {passive: true});
      instant = true;
    }
    if (!pill.isConnected) nav.appendChild(pill);
    const act = $('.sidebar-link-active', nav);
    if (!act) { pill.style.opacity = 0; return; }
    const nr = nav.getBoundingClientRect(), ar = act.getBoundingClientRect();
    const top = ar.top - nr.top + nav.scrollTop, left = ar.left - nr.left + nav.scrollLeft;
    const prev = parseFloat(pill.dataset.top || top);
    pill.dataset.top = top;
    pill.style.transition = instant ? 'none' : '';
    pill.style.opacity = 1;
    pill.style.transform = `translate(${left}px,${top}px)`;
    pill.style.width = ar.width + 'px';
    pill.style.height = ar.height + 'px';
    if (!instant && Math.abs(prev - top) > 4) {
      pill.classList.remove('hp-moving'); void pill.offsetWidth; pill.classList.add('hp-moving');
    }
  }

  /* ================= 页面过场 ================= */
  let lastPath = location.pathname + location.hash;
  function onRoute() {
    const now = location.pathname + location.hash;
    if (now === lastPath) return;
    lastPath = now;
    const main = $('#app main');
    if (main && !reduce) {
      main.classList.remove('hp-enter'); void main.offsetWidth; main.classList.add('hp-enter');
    }
    setTimeout(() => placePill(false), 30);
    setTimeout(scan, 120);
  }
  for (const k of ['pushState', 'replaceState']) {
    const o = history[k];
    history[k] = function () { const r = o.apply(this, arguments); setTimeout(onRoute, 0); return r; };
  }
  addEventListener('popstate', () => setTimeout(onRoute, 0));

  /* ================= 仪表盘：问候横幅 ================= */
  const LINES = [
    '愿每一次调用，都带着善意抵达。',
    '代码会生长，像花一样。',
    '今天，也温柔地创造一点什么吧。',
    '慢一点也没关系，花也是慢慢开的。',
    '你写下的每一行，都在照亮某个人。',
    '万物共生，你也是其中可爱的一员。',
    '把复杂交给我们，把灵感留给你。',
  ];
  function greeting() {
    const h = new Date().getHours();
    return h < 5 ? '夜深了' : h < 11 ? '早上好' : h < 13 ? '中午好' : h < 18 ? '下午好' : '晚上好';
  }
  function heroFlower() {
    // 多层莲花，花瓣逐层舒展
    const lay = [[12, 46, 12, ['#fff3f7', '#ffc9da', '#ff9fbd']], [10, 36, 11, ['#fff7fa', '#ffd9e5', '#ffb2c8']], [8, 26, 9, ['#ffffff', '#ffe7ef', '#ffc6d6']]];
    let defs = '', g = '';
    lay.forEach(([n, l, w, c], i) => {
      defs += `<linearGradient id="hpb${i}" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="0" y2="${-l}"><stop offset="0" stop-color="${c[0]}"/><stop offset=".55" stop-color="${c[1]}"/><stop offset="1" stop-color="${c[2]}"/></linearGradient>`;
      let ps = '';
      for (let k = 0; k < n; k++) ps += `<path transform="rotate(${360 / n * k + i * 14})" d="M0 0C${w} ${-l * .35} ${w * .75} ${-l * .8} 0 ${-l}C${-w * .75} ${-l * .8} ${-w} ${-l * .35} 0 0Z" fill="url(#hpb${i})" stroke="rgba(255,255,255,.8)" stroke-width=".6"/>`;
      g += `<g class="hp-hero-layer" style="--i:${i}">${ps}</g>`;
    });
    return `<svg viewBox="-55 -55 110 110" aria-hidden="true"><defs>${defs}<radialGradient id="hpbc" cx=".4" cy=".35"><stop offset="0" stop-color="#fff6c9"/><stop offset="1" stop-color="#ffbf47"/></radialGradient></defs><g class="hp-hero-spin">${g}<circle r="7" fill="url(#hpbc)" stroke="#fff" stroke-width="1.2" class="hp-hero-core"/></g></svg>`;
  }
  function dashBanner() {
    const isDash = location.pathname === '/dashboard' || document.documentElement.dataset.hpMock === 'dashboard' && /dashboard|^$/.test(location.hash.slice(1));
    const main = $('#app main');
    const old = $('.hp-hello');
    if (!isDash || !main) { old && old.remove(); return; }
    if (old && main.contains(old)) return;
    const name = (($('#app header') || {}).innerText || '').match(/(?:欢迎回来|Welcome back)[，,\s]*([^\s\n，,!！]{1,20})/);
    const el = document.createElement('section');
    el.className = 'hp-hello';
    el.innerHTML = `
      <div class="hp-hello-text">
        <p class="hp-hello-eyebrow">${new Date().toLocaleDateString('zh-CN', {month: 'long', day: 'numeric', weekday: 'long'})}</p>
        <h2>${greeting()}${name ? '，' + name[1] : ''} <span class="hp-wave">🌸</span></h2>
        <p class="hp-hello-line">${LINES[new Date().getDate() % LINES.length]}</p>
        <div class="hp-hello-links">
          <a href="/keys">管理密钥</a><a href="/usage">查看用量</a><a href="/model-plaza">逛逛模型</a>
        </div>
      </div>
      <div class="hp-hello-flower">${heroFlower()}</div>`;
    main.prepend(el);
    // 链接走 SPA 路由：触发站内原生导航
    $$('.hp-hello-links a', el).forEach(a => a.addEventListener('click', e => {
      const link = $(`#app .sidebar a[href="${a.getAttribute('href')}"]`);
      if (link) { e.preventDefault(); link.click(); }
    }));
  }

  /* ================= 数字滚动 ================= */
  // 数据异步加载：每当 Vue 改写数字，就从旧值平滑滚到新值。只写同一个文本节点，Vue 再次改写时立即让位。
  const NUM = /(-?\d[\d,]*\.?\d*)/;
  const st = new WeakMap();
  const TIME_LIKE = /\d\s*:\s*\d|\d{4}[-/]\d{1,2}[-/]\d{1,2}/;
  function countUp(node) {
    const text = node.nodeValue;
    let s = st.get(node);
    if (s && s.active) { if (text === s.mine) return; s.active = false; } // Vue 中途改写
    if (s && s.final === text) return;
    const m = text.match(NUM);
    if (!m) { st.set(node, {final: text, num: s ? s.num : NaN}); return; }
    const raw = m[1], target = parseFloat(raw.replace(/,/g, ''));
    const from = s && isFinite(s.num) ? s.num : 0;
    s = {final: text, num: target, active: false, mine: text};
    st.set(node, s);
    if (reduce || !isFinite(target) || target === from) return;
    const dec = (raw.split('.')[1] || '').length, comma = raw.includes(',');
    const fmt = v => {
      let o = v.toFixed(dec);
      if (comma) { const [a, b] = o.split('.'); o = a.replace(/\B(?=(\d{3})+(?!\d))/g, ',') + (b ? '.' + b : ''); }
      return text.slice(0, m.index) + o + text.slice(m.index + raw.length);
    };
    const t0 = performance.now(), dur = from ? 700 : 1300;
    s.active = true; s.mine = fmt(from); node.nodeValue = s.mine;
    const step = now => {
      if (!s.active) return;
      if (node.nodeValue !== s.mine) { s.active = false; return; }
      const k = Math.min(1, (now - t0) / dur), e = 1 - Math.pow(1 - k, 4);
      s.mine = k < 1 ? fmt(from + (target - from) * e) : text;
      node.nodeValue = s.mine;
      if (k < 1) raf(step); else s.active = false;
    };
    raf(step);
    // 兜底：标签页在后台时 rAF 暂停，到点直接写回真实值
    setTimeout(() => { if (s.active && node.nodeValue === s.mine) { node.nodeValue = text; s.mine = text; s.active = false; } }, dur + 400);
  }
  function scanNumbers() {
    const vh = innerHeight;
    $$('#app main .card .text-xl.font-bold, #app main .card .text-2xl.font-bold, #app main .card .text-3xl.font-bold, #app .stat-value').forEach(el => {
      el.classList.contains('hp-num') || el.classList.add('hp-num');
      const r = el.getBoundingClientRect();
      if (r.bottom < 0 || r.top > vh) return;
      // 倒计时 / 时间 / 日期（如 29:58、2026-11-20）不做数字滚动，否则每秒改写都会被动画打乱
      if (el.closest('[data-hp-static]')) return;
      const tn = [...el.childNodes].find(n => n.nodeType === 3 && n.nodeValue.trim());
      if (tn && !TIME_LIKE.test(tn.nodeValue)) countUp(tn);
    });
  }
  addEventListener('scroll', () => schedule(), {passive: true});

  /* ================= 卡片：聚光 + 轻微倾斜 ================= */
  if (fine) {
    let tiltEl = null;
    document.addEventListener('pointermove', e => {
      const card = e.target.closest && e.target.closest('#app .card, #app .stat-card, .hp-hello');
      if (tiltEl && tiltEl !== card) { tiltEl.style.transform = ''; tiltEl = null; }
      if (!card) return;
      const r = card.getBoundingClientRect();
      card.style.setProperty('--hp-mx', (e.clientX - r.left) + 'px');
      card.style.setProperty('--hp-my', (e.clientY - r.top) + 'px');
      if (!reduce && r.height < 200 && r.width < 520 && card.closest('.grid')) {
        const px = (e.clientX - r.left) / r.width - .5, py = (e.clientY - r.top) / r.height - .5;
        card.style.transform = `perspective(900px) rotateX(${-py * 5}deg) rotateY(${px * 5}deg) translateY(-2px)`;
        tiltEl = card;
      }
    }, {passive: true});
    document.addEventListener('pointerleave', () => { if (tiltEl) tiltEl.style.transform = ''; }, true);
  }

  /* ================= 按钮花瓣 / 复制撒花 ================= */
  document.addEventListener('click', e => {
    const b = e.target.closest && e.target.closest('button, a, [role="button"]');
    if (!b || b.disabled) return;
    const label = (b.innerText + ' ' + (b.title || '') + ' ' + (b.getAttribute('aria-label') || '')).toLowerCase();
    if (/复制|copy/.test(label)) burst(e.clientX, e.clientY, 16, 1.3);
    else if (b.matches('#app .btn-primary, #app .bg-gradient-to-r.from-primary-500, #app .bg-primary-500, #app .bg-primary-600')) burst(e.clientX, e.clientY, 7, .8);
  }, true);

  /* ================= 空状态：花朵插画 ================= */
  function emptyFlowers() {
    $$('#app .empty-state-icon:not([data-hp])').forEach(el => {
      el.dataset.hp = '1';
      const f = document.createElement('span');
      f.className = 'hp-empty-flower';
      f.innerHTML = heroFlower().replace(/hpb/g, 'hpe' + Math.random().toString(36).slice(2, 6));
      el.appendChild(f);
    });
  }

  /* ================= 调度 ================= */
  let pending = false;
  function scan() {
    pending = false;
    placePill(false);
    dashBanner();
    scanNumbers();
    emptyFlowers();
  }
  function schedule() { if (!pending) { pending = true; setTimeout(scan, 60); } }
  function boot() {
    const app = $('#app');
    if (!app) return;
    new MutationObserver(muts => {
      // 只关心结构与 class 变化（忽略自己写入的元素）
      for (const m of muts) {
        const t = m.target.nodeType === 3 ? m.target.parentElement : m.target;
        if (!t || t.closest('.hp-hello, .hp-nav-pill, .hp-empty-flower, .playground-markdown')) continue;
        if (m.type === 'characterData') { const s0 = st.get(m.target); if (s0 && s0.active && m.target.nodeValue === s0.mine) continue; }
        schedule(); break;
      }
    }).observe(app, {childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ['class']});
    addEventListener('resize', () => placePill(true));
    scan();
  }
  if (document.readyState === 'loading') addEventListener('DOMContentLoaded', boot); else boot();
})();

/*!
 * Happy · Flowing Light —— 流光
 * 1. 边框光：悬停在按钮、卡片、输入框上，离鼠标最近的一段边框被点亮，随鼠标沿边框滑行。
 * 2. 涟漪：点击 / 轻点处泛起一圈细细的彩虹光环。
 * 只沿边框出现，不遮挡任何内容；鼠标移动本身不留拖尾。可独立引入任意页面。
 */
(() => {
  if (window.__happyGlow) return;
  window.__happyGlow = true;
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (reduce) return;
  const fine = matchMedia('(pointer: fine)').matches;

  const PAL = [[255, 168, 128], [255, 133, 166], [178, 150, 255], [130, 190, 255], [110, 214, 186]];
  const colorAt = (t, a) => {               // t 任意实数，沿色环循环
    const n = PAL.length, x = ((t % n) + n) % n, i = Math.floor(x), f = x - i;
    const A = PAL[i], B = PAL[(i + 1) % n];
    return `rgba(${A.map((v, k) => Math.round(v + (B[k] - v) * f)).join(',')},${a})`;
  };

  let cv, ctx, W = 0, H = 0, D = 1, running = false;
  const mouse = {x: -1e4, y: -1e4, seen: false};

  /* ---------- 边框光 ---------- */
  const FRAME = 'button,a.btn,.btn,input,textarea,select,.card,.stat-card,.card-glass,.glass,.badge,.chip,.pill,[role="button"],.sidebar-link';
  const SKIP = '.happy-no-glow,iframe,video,canvas';
  const edges = [];                          // {el, s, a, on}

  /* ---------- 涟漪 ---------- */
  const ripples = [];

  function init() {
    cv = document.createElement('canvas');
    cv.className = 'happy-glow-layer';
    cv.setAttribute('aria-hidden', 'true');
    cv.style.cssText = 'position:fixed;inset:0;width:100vw;height:100vh;pointer-events:none;z-index:2147483000';
    document.body.appendChild(cv);
    ctx = cv.getContext('2d');
    resize();
    addEventListener('resize', resize);
    if (fine) {
      addEventListener('pointermove', onMove, {passive: true});
      document.addEventListener('pointerleave', () => { mouse.seen = false; edges.forEach(e => e.on = false); });
    }
    addEventListener('pointerdown', onDown, {passive: true});
  }
  function resize() {
    D = Math.min(devicePixelRatio || 1, 2);
    W = innerWidth; H = innerHeight;
    cv.width = W * D; cv.height = H * D;
  }
  const kick = () => { if (!running) { running = true; requestAnimationFrame(frame); } };

  function onMove(e) {
    mouse.seen = true;
    mouse.x = e.clientX; mouse.y = e.clientY;
    hover(e.target);
    kick();
  }
  function onDown(e) {
    ripples.push({x: e.clientX, y: e.clientY, t0: performance.now()});
    if (e.pointerType !== 'mouse') {          // 触屏：轻点的元素边框闪一下
      const el = frameOf(e.target);
      if (el) { mouse.x = e.clientX; mouse.y = e.clientY; hover(e.target); setTimeout(() => edges.forEach(x => { if (x.el === el) x.on = false; }), 700); }
    }
    kick();
  }

  function frameOf(t) {
    if (!t || t.nodeType !== 1 || t.closest(SKIP)) return null;
    const el = t.closest(FRAME);
    if (!el) return null;
    const r = el.getBoundingClientRect();
    if (r.width < 20 || r.height < 16 || r.width * r.height > W * H * .6) return null;
    return el;
  }
  function hover(t) {
    const el = frameOf(t);
    for (const e of edges) e.on = e.el === el;
    if (el && !edges.some(e => e.el === el)) {
      const tr = track(el);
      if (tr) edges.push({el, s: tr.near(mouse.x, mouse.y), a: 0, on: true});
      while (edges.length > 3) edges.shift();
    }
  }

  // 圆角矩形外轮廓参数化：s ∈ [0, P)
  function track(el) {
    const r = el.getBoundingClientRect();
    if (!r.width) return null;
    const rad = Math.min(parseFloat(getComputedStyle(el).borderTopLeftRadius) || 0, r.width / 2, r.height / 2);
    const L = r.left + .5, T = r.top + .5, Rr = r.right - .5, B = r.bottom - .5;
    const w = Rr - L - 2 * rad, h = B - T - 2 * rad, arc = Math.PI * rad / 2;
    const seg = [w, arc, h, arc, w, arc, h, arc], P = seg.reduce((a, b) => a + b, 0);
    const at = s => {
      s = ((s % P) + P) % P;
      let i = 0; while (s > seg[i] && i < 7) { s -= seg[i]; i++; }
      const A = seg[i] ? s / seg[i] * Math.PI / 2 : 0;
      switch (i) {
        case 0: return [L + rad + s, T];
        case 1: return [Rr - rad + Math.sin(A) * rad, T + rad - Math.cos(A) * rad];
        case 2: return [Rr, T + rad + s];
        case 3: return [Rr - rad + Math.cos(A) * rad, B - rad + Math.sin(A) * rad];
        case 4: return [Rr - rad - s, B];
        case 5: return [L + rad - Math.sin(A) * rad, B - rad + Math.cos(A) * rad];
        case 6: return [L, B - rad - s];
        default: return [L + rad - Math.cos(A) * rad, T + rad - Math.sin(A) * rad];
      }
    };
    const near = (x, y) => { let best = 0, bd = 1e12; for (let i = 0; i < 180; i++) { const s = P * i / 180, [px, py] = at(s), d = (px - x) ** 2 + (py - y) ** 2; if (d < bd) { bd = d; best = s; } } return best; };
    return {P, at, near};
  }

  /* ---------- 绘制 ---------- */
  function drawEdges(now) {
    let alive = false;
    for (let j = edges.length - 1; j >= 0; j--) {
      const e = edges[j];
      if (!e.el.isConnected) { edges.splice(j, 1); continue; }
      e.a += ((e.on ? 1 : 0) - e.a) * (e.on ? .16 : .1);
      if (!e.on && e.a < .02) { edges.splice(j, 1); continue; }
      alive = true;
      const tr = track(e.el); if (!tr) continue;
      // 沿最短方向滑向鼠标最近点
      if (e.on) { let d = tr.near(mouse.x, mouse.y) - e.s; d -= Math.round(d / tr.P) * tr.P; e.s += d * .22; }
      const len = Math.min(tr.P * .32, 170), steps = 36, hue = now / 2400;
      ctx.lineCap = 'round';
      for (let i = 0; i < steps; i++) {
        const t0 = i / steps, t1 = (i + 1) / steps, tm = (t0 + t1) / 2;
        const [x0, y0] = tr.at(e.s - len / 2 + len * t0), [x1, y1] = tr.at(e.s - len / 2 + len * t1);
        const prof = Math.pow(Math.sin(Math.PI * tm), 1.6) * e.a;
        ctx.strokeStyle = colorAt(hue + tm * 2.2, .95 * prof);
        ctx.shadowColor = colorAt(hue + tm * 2.2, .9 * prof); ctx.shadowBlur = 12;
        ctx.lineWidth = 2.2;
        ctx.beginPath(); ctx.moveTo(x0, y0); ctx.lineTo(x1, y1); ctx.stroke();
      }
    }
    ctx.shadowBlur = 0;
    return alive;
  }

  function drawRipples(now) {
    for (let i = ripples.length - 1; i >= 0; i--) {
      const r = ripples[i], k = (now - r.t0) / 750;
      if (k >= 1) { ripples.splice(i, 1); continue; }
      const e = 1 - Math.pow(1 - k, 3), rad = 6 + 34 * e, a = (1 - k) * .8;
      const g = ctx.createConicGradient ? ctx.createConicGradient(now / 900, r.x, r.y) : null;
      if (g) { PAL.forEach((c, j) => g.addColorStop(j / PAL.length, `rgba(${c.join(',')},${a})`)); g.addColorStop(1, `rgba(${PAL[0].join(',')},${a})`); }
      ctx.strokeStyle = g || colorAt(1, a); ctx.lineWidth = 1.6 * (1 - k) + .4;
      ctx.beginPath(); ctx.arc(r.x, r.y, rad, 0, 6.283); ctx.stroke();
    }
    return ripples.length > 0;
  }

  function frame(now) {
    ctx.setTransform(D, 0, 0, D, 0, 0);
    ctx.clearRect(0, 0, W, H);
    const a = drawEdges(now), c = drawRipples(now);
    if (a || c) requestAnimationFrame(frame);
    else { running = false; ctx.clearRect(0, 0, W, H); }
  }

  if (document.body) init(); else addEventListener('DOMContentLoaded', init);
})();
