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

  let cv, ctx, W = 0, H = 0, D = 1, running = false, lastMove = 0;
  const mouse = {x: -1e4, y: -1e4, seen: false};
  // 低性能模式（html.hp-lite）下整个流光层停用
  const lite = () => document.documentElement.classList.contains('hp-lite');
  const radii = new WeakMap();

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
      document.addEventListener('pointerleave', () => { mouse.seen = false; edges.forEach(e => e.on = false); kick(); });
      // 滚动时卡片移动，光带要跟着重画
      addEventListener('scroll', () => { if (edges.length) { lastMove = performance.now(); kick(); } }, {passive: true, capture: true});
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
    if (lite()) return;
    lastMove = performance.now();
    mouse.seen = true;
    mouse.x = e.clientX; mouse.y = e.clientY;
    hover(e.target);
    kick();
  }
  function onDown(e) {
    if (lite()) return;
    lastMove = performance.now();
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
    // 圆角只读一次（getComputedStyle 每帧调用会触发样式计算）
    let r0 = radii.get(el);
    if (r0 === undefined) { r0 = parseFloat(getComputedStyle(el).borderTopLeftRadius) || 0; radii.set(el, r0); }
    const rad = Math.min(r0, r.width / 2, r.height / 2);
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
    let alive = false, busy = false;
    for (let j = edges.length - 1; j >= 0; j--) {
      const e = edges[j];
      if (!e.el.isConnected) { edges.splice(j, 1); continue; }
      e.a += ((e.on ? 1 : 0) - e.a) * (e.on ? .16 : .1);
      if (!e.on && e.a < .02) { edges.splice(j, 1); continue; }
      alive = true;
      if (!e.on || e.a < .98) busy = true;
      const tr = track(e.el); if (!tr) continue;
      // 沿最短方向滑向鼠标最近点
      if (e.on) { let d = tr.near(mouse.x, mouse.y) - e.s; d -= Math.round(d / tr.P) * tr.P; e.s += d * .22; if (Math.abs(d) > .5) busy = true; }
      const len = Math.min(tr.P * .32, 170), steps = 36, hue = now / 2400;
      ctx.lineCap = 'round';
      for (let i = 0; i < steps; i++) {
        const t0 = i / steps, t1 = (i + 1) / steps, tm = (t0 + t1) / 2;
        const [x0, y0] = tr.at(e.s - len / 2 + len * t0), [x1, y1] = tr.at(e.s - len / 2 + len * t1);
        const prof = Math.pow(Math.sin(Math.PI * tm), 1.6) * e.a;
        // 用一道宽而淡的描边代替 shadowBlur（shadowBlur 每段都要做一次高斯模糊，很耗 GPU）
        ctx.beginPath(); ctx.moveTo(x0, y0); ctx.lineTo(x1, y1);
        ctx.strokeStyle = colorAt(hue + tm * 2.2, .28 * prof); ctx.lineWidth = 7; ctx.stroke();
        ctx.strokeStyle = colorAt(hue + tm * 2.2, .95 * prof); ctx.lineWidth = 2.2; ctx.stroke();
      }
    }
    return {alive, busy};
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
    const {alive, busy} = drawEdges(now), c = drawRipples(now);
    // 鼠标静止且光带已就位时停帧：画面保持最后一帧，不再每帧重绘
    if (c || busy || (alive && now - lastMove < 160)) requestAnimationFrame(frame);
    else { running = false; if (!alive) ctx.clearRect(0, 0, W, H); }
  }

  if (document.body) init(); else addEventListener('DOMContentLoaded', init);
})();
