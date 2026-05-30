/* ════════════════════════════════════════════════════════════════════════
   Pulsar — live LLM-call pulse field for Ajah
   Plain-JS canvas engine + simulated event stream. No JSX here.
   Encoding:
     angle    = feature (fixed radial sector)
     distance = latency (slow calls crawl farther out)
     speed    = latency (slow calls travel slower)
     size     = cost
     color    = quality (green / amber / red)   — overridden red on a flare
     ring     = PII masked
     flare    = RAG contradiction or very-low quality
   ════════════════════════════════════════════════════════════════════════ */

(function () {
  const TAU = Math.PI * 2;
  const rand = (a, b) => a + Math.random() * (b - a);
  const pick = (a) => a[(Math.random() * a.length) | 0];
  const clamp = (x, a, b) => (x < a ? a : x > b ? b : x);
  const map = (x, a, b, c, d) => c + ((clamp(x, a, b) - a) / (b - a)) * (d - c);

  // ── On-brand palette (brightened for glow on the dark field) ──────────────
  const C = {
    green:   '#27BE8E',
    amber:   '#D7A23E',
    red:     '#E36B42',
    redHard: '#D8483A',
    grid:    'rgba(255,255,255,0.055)',
    gridIn:  'rgba(255,255,255,0.09)',
    tick:    'rgba(255,255,255,0.10)',
    label:   '#5C6378',
    pii:     '#C8D0E6',
  };

  // ── Features → fixed angular sectors ──────────────────────────────────────
  const FEATURES = [
    { name: 'chat',      w: 42, lat: 1150, cost: 0.00072 },
    { name: 'summarize', w: 20, lat: 2050, cost: 0.00210 },
    { name: 'classify',  w: 16, lat: 820,  cost: 0.00110 },
    { name: 'translate', w: 12, lat: 760,  cost: 0.00090 },
    { name: 'embed',     w: 10, lat: 470,  cost: 0.00028 },
  ];
  const FW = FEATURES.reduce((s, f) => s + f.w, 0);
  const MODELS = ['gpt-4o', 'llama-3.3-70b', 'claude-3-5', 'gemini-1.5', 'mistral'];

  function featureAngle(name) {
    const i = FEATURES.findIndex((f) => f.name === name);
    // start at top, go clockwise; slight offset so no sector sits dead-vertical
    return -Math.PI / 2 + 0.32 + (i * TAU) / FEATURES.length;
  }

  function weightedFeature() {
    let r = Math.random() * FW;
    for (const f of FEATURES) { if ((r -= f.w) <= 0) return f; }
    return FEATURES[0];
  }

  function qualityColor(q, flare) {
    if (flare) return C.redHard;
    if (q >= 0.8) return C.green;
    if (q >= 0.5) return C.amber;
    return C.red;
  }

  let _id = 0;
  function genEvent() {
    const f = weightedFeature();
    const model = pick(MODELS);
    const latency = clamp(
      f.lat * rand(0.6, 1.5) + (Math.random() < 0.07 ? rand(1100, 2600) : 0),
      280, 4200
    );
    const cost = clamp(f.cost * rand(0.55, 1.9), 0.00006, 0.0058);
    const r = Math.random();
    const quality = r < 0.06 ? 0
      : r < 0.14 ? rand(0.40, 0.60)
      : r < 0.30 ? rand(0.60, 0.80)
      : rand(0.80, 0.99);
    const pii = Math.random() < (f.name === 'translate' || f.name === 'chat' ? 0.11 : 0.04);
    let rag = 'not_applicable';
    if (f.name === 'summarize' || f.name === 'chat') {
      const rr = Math.random();
      rag = rr < 0.55 ? 'supported' : rr < 0.73 ? 'partially_supported'
          : rr < 0.86 ? 'unsupported' : 'contradicted';
    }
    const flare = rag === 'contradicted' || (quality > 0 && quality < 0.45);
    return {
      id: ++_id, t: Date.now(), feature: f.name, fconf: f, model,
      latency: Math.round(latency), cost, quality, pii, rag, flare,
    };
  }

  // ── Engine ────────────────────────────────────────────────────────────────
  class PulsarField {
    constructor(canvas) {
      this.cv = canvas;
      this.ctx = canvas.getContext('2d');
      this.particles = [];
      this.ripples = [];
      this.flares = [];
      this.core = 0;
      this.spin = 0;
      this.listeners = [];
      this.cfg = { liveliness: 7, rate: 64, paused: false, accent: '#185FA5', sectors: true };
      this.acc = 0; this.nextIn = 0; this._raf = null; this.last = 0;
      this.resize();
      this._ro = new ResizeObserver(() => this.resize());
      this._ro.observe(canvas.parentElement);
    }

    setConfig(c) { Object.assign(this.cfg, c); }
    on(cb) { this.listeners.push(cb); }

    resize() {
      const p = this.cv.parentElement;
      const rect = p.getBoundingClientRect();
      const w = Math.round(rect.width), h = Math.round(rect.height);
      if (w <= 0 || h <= 0) return; // layout not flushed yet — leave canvas alone
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      // canvas is position:absolute; inset:0 — CSS stretches it. Only size the backing buffer.
      this.cv.width = Math.round(w * dpr);
      this.cv.height = Math.round(h * dpr);
      this.ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      this.W = w; this.H = h; this.cx = w / 2; this.cy = h / 2;
      this.R = (Math.min(w, h) / 2) * 0.84;
      this._lastW = w; this._lastH = h;
    }

    interval() { return (60000 / Math.max(this.cfg.rate, 1)) * rand(0.35, 1.85); }

    start() {
      this.last = performance.now();
      this.nextIn = this.interval();
      const loop = (ts) => {
        const dt = Math.min(ts - this.last, 48);
        this.last = ts;
        // self-heal: if the parent changed size (or was 0 at mount), re-measure
        const p = this.cv.parentElement;
        if (p) {
          const rect = p.getBoundingClientRect();
          const w = Math.round(rect.width), h = Math.round(rect.height);
          if (w > 0 && h > 0 && (w !== this._lastW || h !== this._lastH)) this.resize();
        }
        this.step(dt);
        this.draw();
        this._raf = requestAnimationFrame(loop);
      };
      this._raf = requestAnimationFrame(loop);
    }
    stop() { cancelAnimationFrame(this._raf); this._ro.disconnect(); }

    burst(n) { for (let i = 0; i < n; i++) this.emit(genEvent()); }

    emit(evt) {
      for (const cb of this.listeners) cb(evt);
      if (!this.R) return; // not measured yet — skip visuals, ticker/counters still fed above
      const liv = this.cfg.liveliness / 7;
      const angle = featureAngle(evt.feature) + rand(-0.17, 0.17);
      const targetR = map(evt.latency, 380, 3200, 0.42, 0.99) * this.R;
      const travelDur = map(evt.latency, 380, 3200, 480, 2100);
      const size = map(evt.cost, 0.0002, 0.0042, 2.6, 9.2);
      const color = qualityColor(evt.quality, evt.flare);
      this.particles.push({
        angle, r: 0, targetR, travelDur, t: 0, size, color,
        pii: evt.pii, flare: evt.flare,
        life: travelDur + rand(1500, 2300), trail: [],
      });
      this.ripples.push({
        cr: 0, maxR: targetR * 1.06, color, t: 0,
        life: map(evt.latency, 380, 3200, 850, 1650),
        w: (0.8 + size * 0.16) * (0.7 + 0.5 * liv),
      });
      this.core = Math.min(this.core + 0.55 + size * 0.045, 3.4);
      if (evt.flare) {
        this.flares.push({
          angle, r: targetR, color,
          text: evt.rag === 'contradicted' ? 'contradicted' : 'low quality',
          t: 0, life: 2300,
        });
      }
    }

    step(dt) {
      const liv = this.cfg.liveliness / 7;
      if (!this.cfg.paused) {
        this.acc += dt;
        if (this.acc >= this.nextIn) { this.acc = 0; this.nextIn = this.interval(); this.emit(genEvent()); }
      }
      this.spin += dt * 0.00004 * (0.4 + liv);

      for (let i = this.particles.length - 1; i >= 0; i--) {
        const p = this.particles[i];
        p.t += dt;
        const prog = clamp(p.t / p.travelDur, 0, 1);
        const ease = 1 - Math.pow(1 - prog, 2.2);
        p.r = ease * p.targetR;
        const inA = Math.min(p.t / 110, 1);
        const outA = 1 - clamp((p.t - (p.life - 760)) / 760, 0, 1);
        p.alpha = inA * outA;
        const x = this.cx + Math.cos(p.angle) * p.r;
        const y = this.cy + Math.sin(p.angle) * p.r;
        p.x = x; p.y = y;
        p.trail.push({ x, y, a: p.alpha });
        const maxTrail = Math.round(7 + 12 * liv);
        if (p.trail.length > maxTrail) p.trail.shift();
        if (p.t >= p.life) this.particles.splice(i, 1);
      }
      for (let i = this.ripples.length - 1; i >= 0; i--) {
        const rp = this.ripples[i];
        rp.t += dt;
        const prog = rp.t / rp.life;
        rp.cr = prog * rp.maxR;
        rp.alpha = (1 - prog) * (prog < 0.1 ? prog / 0.1 : 1);
        if (prog >= 1) this.ripples.splice(i, 1);
      }
      for (let i = this.flares.length - 1; i >= 0; i--) {
        const fl = this.flares[i];
        fl.t += dt;
        if (fl.t >= fl.life) this.flares.splice(i, 1);
      }
      this.core *= Math.pow(0.93, dt / 16);
    }

    draw() {
      if (!this.R) return; // not measured yet
      const ctx = this.ctx;
      const { cx, cy, R } = this;
      const liv = this.cfg.liveliness / 7;
      const accent = this.cfg.accent || '#185FA5';
      ctx.clearRect(0, 0, this.W, this.H);

      // ── guide rings ──
      const rings = [0.3, 0.55, 0.8, 1.0];
      for (let i = 0; i < rings.length; i++) {
        ctx.beginPath();
        ctx.arc(cx, cy, R * rings[i], 0, TAU);
        ctx.strokeStyle = i === rings.length - 1 ? C.gridIn : C.grid;
        ctx.lineWidth = 1;
        ctx.stroke();
      }

      // ── rim tick marks ──
      ctx.strokeStyle = C.tick;
      ctx.lineWidth = 1;
      for (let a = 0; a < TAU; a += TAU / 60) {
        const major = Math.abs((a % (TAU / 12))) < 0.001;
        const r1 = R * (major ? 0.965 : 0.982);
        ctx.beginPath();
        ctx.moveTo(cx + Math.cos(a + this.spin) * r1, cy + Math.sin(a + this.spin) * r1);
        ctx.lineTo(cx + Math.cos(a + this.spin) * R, cy + Math.sin(a + this.spin) * R);
        ctx.stroke();
      }

      // ── feature sectors + labels ──
      if (this.cfg.sectors) {
        ctx.save();
        ctx.font = '600 10px -apple-system, BlinkMacSystemFont, system-ui, sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        for (const f of FEATURES) {
          const a = featureAngle(f.name);
          ctx.beginPath();
          ctx.moveTo(cx + Math.cos(a) * R * 0.3, cy + Math.sin(a) * R * 0.3);
          ctx.lineTo(cx + Math.cos(a) * R, cy + Math.sin(a) * R);
          ctx.strokeStyle = 'rgba(255,255,255,0.04)';
          ctx.lineWidth = 1;
          ctx.stroke();
          const lr = R + 16;
          ctx.fillStyle = C.label;
          ctx.fillText(f.name, cx + Math.cos(a) * lr, cy + Math.sin(a) * lr);
        }
        ctx.restore();
      }

      // ── ripples ──
      for (const rp of this.ripples) {
        const cr = Math.max(0, rp.cr);
        if (!isFinite(cr)) continue;
        ctx.beginPath();
        ctx.arc(cx, cy, cr, 0, TAU);
        ctx.strokeStyle = withAlpha(rp.color, rp.alpha * 0.55);
        ctx.lineWidth = Math.max(0.3, rp.w);
        ctx.stroke();
      }

      // ── particle trails ──
      for (const p of this.particles) {
        if (p.trail.length < 2) continue;
        ctx.beginPath();
        ctx.moveTo(p.trail[0].x, p.trail[0].y);
        for (let i = 1; i < p.trail.length; i++) ctx.lineTo(p.trail[i].x, p.trail[i].y);
        ctx.strokeStyle = withAlpha(p.color, p.alpha * 0.22);
        ctx.lineWidth = Math.max(1, p.size * 0.4);
        ctx.lineCap = 'round';
        ctx.stroke();
      }

      // ── particles ──
      for (const p of this.particles) {
        ctx.save();
        ctx.globalAlpha = p.alpha;
        ctx.shadowColor = p.color;
        ctx.shadowBlur = p.size * (1.8 + 1.4 * liv);
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.size, 0, TAU);
        ctx.fillStyle = p.color;
        ctx.fill();
        ctx.shadowBlur = 0;
        // crisp core dot
        ctx.beginPath();
        ctx.arc(p.x, p.y, Math.max(1, p.size * 0.45), 0, TAU);
        ctx.fillStyle = '#F4F6FC';
        ctx.globalAlpha = p.alpha * 0.8;
        ctx.fill();
        // PII ring
        if (p.pii) {
          ctx.globalAlpha = p.alpha * 0.85;
          ctx.beginPath();
          ctx.arc(p.x, p.y, p.size + 3.5, 0, TAU);
          ctx.strokeStyle = C.pii;
          ctx.lineWidth = 1;
          ctx.setLineDash([2, 2.5]);
          ctx.stroke();
          ctx.setLineDash([]);
        }
        ctx.restore();
      }

      // ── flare labels ──
      ctx.save();
      ctx.font = '600 10px -apple-system, BlinkMacSystemFont, system-ui, sans-serif';
      ctx.textBaseline = 'middle';
      for (const fl of this.flares) {
        const prog = fl.t / fl.life;
        const a = 1 - clamp((prog - 0.55) / 0.45, 0, 1);
        const lift = -8 * prog;
        const x = cx + Math.cos(fl.angle) * (fl.r + 12);
        const y = cy + Math.sin(fl.angle) * (fl.r + 12) + lift;
        const txt = fl.text;
        const w = ctx.measureText(txt).width + 14;
        ctx.globalAlpha = a;
        roundRect(ctx, x - w / 2, y - 9, w, 18, 4);
        ctx.fillStyle = withAlpha(fl.color, 0.18);
        ctx.fill();
        ctx.strokeStyle = withAlpha(fl.color, 0.55);
        ctx.lineWidth = 1;
        ctx.stroke();
        ctx.fillStyle = '#F2D6CC';
        ctx.textAlign = 'center';
        ctx.fillText(txt, x, y + 0.5);
      }
      ctx.restore();

      // ── core ──
      const coreR = R * 0.16 * (0.4 + this.core * 0.2);
      const g = ctx.createRadialGradient(cx, cy, 0, cx, cy, Math.max(coreR, 2));
      g.addColorStop(0, withAlpha(accent, 0.55 + 0.12 * this.core));
      g.addColorStop(0.5, withAlpha(accent, 0.16));
      g.addColorStop(1, withAlpha(accent, 0));
      ctx.beginPath();
      ctx.arc(cx, cy, Math.max(coreR, 2), 0, TAU);
      ctx.fillStyle = g;
      ctx.fill();
      ctx.beginPath();
      ctx.arc(cx, cy, 3 + this.core * 0.9, 0, TAU);
      ctx.fillStyle = lighten(accent);
      ctx.shadowColor = accent;
      ctx.shadowBlur = 12 + this.core * 6;
      ctx.fill();
      ctx.shadowBlur = 0;
    }
  }

  // ── color helpers ─────────────────────────────────────────────────────────
  function hexToRgb(h) {
    h = h.replace('#', '');
    if (h.length === 3) h = h.split('').map((c) => c + c).join('');
    const n = parseInt(h, 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  }
  function withAlpha(hex, a) {
    const [r, g, b] = hexToRgb(hex);
    return `rgba(${r},${g},${b},${clamp(a, 0, 1)})`;
  }
  function lighten(hex) {
    const [r, g, b] = hexToRgb(hex);
    return `rgb(${Math.min(255, r + 70)},${Math.min(255, g + 70)},${Math.min(255, b + 70)})`;
  }
  function roundRect(ctx, x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
  }

  window.PulsarField = PulsarField;
  window.PulsarMeta = { FEATURES, MODELS, COLORS: C, genEvent };
})();
