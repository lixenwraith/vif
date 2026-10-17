(function() {
    'use strict';

    /* The files beside the page, by the names web/ gives them. A site that
       publishes them under other names, as content-hashed ones, says so on this
       script's element: data-wasm, data-worker and data-exec (wasm_exec.js). */
    const files = (document.currentScript && document.currentScript.dataset) || {};
    const WASM_PATH = files.wasm || 'vif.wasm';
    const WORKER_PATH = files.worker || 'worker.js';
    const EXEC_PATH = files.exec || 'wasm_exec.js';

    /* A touch-first screen (phone, tablet), decided once at load, keeps the system
       keyboard down on taps and gets the key strip to raise it. A cell is CELL CSS
       pixels tall and half as wide, the aspect the game is authored at; a phone
       starts at 6, which holds the map's width in its screen, and a pinch, or
       Ctrl and the wheel, takes the cell between CELL_LEAST and CELL_MOST. */
    const touch = matchMedia('(pointer: coarse)').matches;
    const CELL = touch && Math.min(screen.width, screen.height) < 600 ? 6 : 16;
    const CELL_LEAST = 4, CELL_MOST = 40;
    let cellBase = CELL;    // the cell the grid is fitted at: CELL until a zoom moves it
    const FONT = '"JetBrains Mono", "Fira Code", "SF Mono", Menlo, "DejaVu Sans Mono", Consolas, monospace';

    const container = document.getElementById('terminal');
    const canvas = document.getElementById('screen');
    const input = document.getElementById('input');
    let program;            // the Worker running Go; see worker.js
    let drawer;             // the screen, below
    let cols = 0, rows = 0; // the grid the page reported to Go

    // Posts an event to Go; before the program starts, the start message carries the grid.
    function send(ev) {
        if (program) program.postMessage({ type: 'event', event: ev });
    }

    // === The screen ===
    /* One WebGL quad over the frame. A fragment finds its cell in two data textures,
       the cell's colors and glyph slot, and mixes the colors by the glyph's mask,
       sampled from an atlas rasterized from FONT at the cell's device size, so every
       glyph is drawn pixel for pixel at any zoom. A frame arrives as the cells changed
       since the last one (pkg/glyph, Frames.Take): index, slot, foreground, background. */
    const VERTEX = `
attribute vec2 a_pos;
uniform vec4 u_quad;
varying vec2 v_uv;
void main() {
    v_uv = a_pos;
    gl_Position = vec4(u_quad.xy + a_pos * u_quad.zw, 0.0, 1.0);
}`;
    const FRAGMENT = `
#ifdef GL_FRAGMENT_PRECISION_HIGH
precision highp float;
#else
precision mediump float;
#endif
uniform sampler2D u_fg;
uniform sampler2D u_bg;
uniform sampler2D u_mask;
uniform vec2 u_grid;
uniform vec2 u_slots;
varying vec2 v_uv;
void main() {
    vec2 pos = v_uv * u_grid;
    vec2 cuv = (floor(pos) + 0.5) / u_grid;
    vec4 fg = texture2D(u_fg, cuv);
    vec4 bg = texture2D(u_bg, cuv);
    float slot = floor(fg.a * 255.0 + 0.5) + floor(bg.a * 255.0 + 0.5) * 256.0 - 1.0;
    float m = 0.0;
    if (slot >= 0.0) {
        vec2 at = vec2(mod(slot, u_slots.x), floor(slot / u_slots.x));
        m = texture2D(u_mask, (at + fract(pos)) / u_slots).a;
    }
    gl_FragColor = vec4(mix(bg.rgb, fg.rgb, m), 1.0);
}`;
    const ATLAS_COLS = 16;   // slots a row of the mask, which keeps it under every GPU's texture limit

    /* rasterizeMask draws every glyph, white on transparent, at the cell's device size:
       FONT sized so its advance is the cell's width, then stretched to the cell's
       height, so box-drawing glyphs meet their neighbours both ways; a glyph wider
       than its cell is clipped to it. Slot i is at column i % ATLAS_COLS. */
    function rasterizeMask(runes, cw, ch) {
        const n = runes.length, slotRows = Math.ceil(n / ATLAS_COLS);
        const c = document.createElement('canvas');
        c.width = ATLAS_COLS * cw;
        c.height = slotRows * ch;
        const ctx = c.getContext('2d');
        ctx.font = ch + 'px ' + FONT;
        let m = ctx.measureText('M');
        const size = Math.max(1, ch * cw / (m.width || ch));
        ctx.font = size + 'px ' + FONT;
        m = ctx.measureText('M');
        const asc = m.fontBoundingBoxAscent || size * 0.8;
        const desc = m.fontBoundingBoxDescent || size * 0.2;
        const sy = ch / (asc + desc);
        ctx.fillStyle = '#fff';
        ctx.textBaseline = 'alphabetic';
        for (let i = 0; i < n; i++) {
            const x = (i % ATLAS_COLS) * cw, y = Math.floor(i / ATLAS_COLS) * ch;
            ctx.save();
            ctx.beginPath();
            ctx.rect(x, y, cw, ch);
            ctx.clip();
            ctx.setTransform(1, 0, 0, sy, 0, 0);
            ctx.fillText(runes[i], x + (cw - ctx.measureText(runes[i]).width) / 2, y / sy + asc);
            ctx.restore();
        }
        return { canvas: c, rows: slotRows };
    }

    // runesOf reads the texture pkg/glyph encodes: cell size and slot count as words,
    // the masks, then each slot's rune; only the runes are used here
    function runesOf(data) {
        const head = new Uint32Array(data.buffer, data.byteOffset, 3);
        const n = head[2], at = 12 + head[0] * head[1] * n;
        return Array.from(new Uint32Array(data.slice(at, at + 4 * n).buffer), c => String.fromCodePoint(c));
    }

    // applyFrame takes a frame into a drawer's cell arrays: the cells changed since
    // the one held, or all after a resize; it returns the changed indices
    function applyFrame(d, data) {
        const w = new Uint32Array(data.buffer, data.byteOffset, data.byteLength >> 2);
        const cols = w[0], rows = w[1], n = w[2];
        if (cols !== d.cols || rows !== d.rows) {
            d.cols = cols;
            d.rows = rows;
            d.fg = new Uint8Array(cols * rows * 4);
            d.bg = new Uint8Array(cols * rows * 4);
            d.resized();
        }
        const fg = d.fg, bg = d.bg, changed = new Uint32Array(n);
        for (let i = 0, at = 3; i < n; i++, at += 4) {
            const idx = w[at], o = idx * 4, slot = (w[at + 1] | 0) + 1, f = w[at + 2], b = w[at + 3];
            fg[o] = f >>> 16 & 255; fg[o + 1] = f >>> 8 & 255; fg[o + 2] = f & 255; fg[o + 3] = slot & 255;
            bg[o] = b >>> 16 & 255; bg[o + 1] = b >>> 8 & 255; bg[o + 2] = b & 255; bg[o + 3] = slot >>> 8 & 255;
            changed[i] = idx;
        }
        return changed;
    }

    // The frame a drawer holds in device pixels, and where it is drawn: at the top,
    // centred when narrower than the canvas
    function frameOf(d) { return [d.cols * d.cw, d.rows * d.ch]; }
    function originOf(d) { return [Math.max(0, Math.floor((canvas.width - d.cols * d.cw) / 2)), 0]; }

    function Drawer(canvas) {
        const gl = canvas.getContext('webgl', { alpha: false, antialias: false, depth: false, stencil: false });
        if (!gl) throw new Error('WebGL is unavailable');
        this.gl = gl;
        this.runes = [];                // the atlas's glyphs, by slot
        this.cw = 0; this.ch = 0;       // a cell in device pixels
        this.cols = 0; this.rows = 0;   // the frame held
        this.fg = null; this.bg = null; // its cells: RGB and a byte of the slot each
        this.dirty = false;
        this.lost = false;
        canvas.addEventListener('webglcontextlost', e => { e.preventDefault(); this.lost = true; });
        canvas.addEventListener('webglcontextrestored', () => {
            this.lost = false;
            this.setup();
            this.rasterize();
            this.dirty = true;
            this.draw();
            send({ type: 'sync' });
        });
        this.setup();
    }

    Drawer.prototype.setup = function() {
        const gl = this.gl;
        const program = gl.createProgram();
        for (const [kind, src] of [[gl.VERTEX_SHADER, VERTEX], [gl.FRAGMENT_SHADER, FRAGMENT]]) {
            const shader = gl.createShader(kind);
            gl.shaderSource(shader, src);
            gl.compileShader(shader);
            if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS) && !gl.isContextLost()) {
                throw new Error('shader: ' + gl.getShaderInfoLog(shader));
            }
            gl.attachShader(program, shader);
        }
        gl.linkProgram(program);
        if (!gl.getProgramParameter(program, gl.LINK_STATUS) && !gl.isContextLost()) {
            throw new Error('shader: ' + gl.getProgramInfoLog(program));
        }
        gl.useProgram(program);
        gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
        gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([0, 0, 1, 0, 0, 1, 1, 1]), gl.STATIC_DRAW);
        const pos = gl.getAttribLocation(program, 'a_pos');
        gl.enableVertexAttribArray(pos);
        gl.vertexAttribPointer(pos, 2, gl.FLOAT, false, 0, 0);
        this.u = {};
        for (const name of ['u_quad', 'u_grid', 'u_slots', 'u_fg', 'u_bg', 'u_mask']) {
            this.u[name] = gl.getUniformLocation(program, name);
        }
        this.tex = {};
        [['u_fg', 0], ['u_bg', 1], ['u_mask', 2]].forEach(([name, unit]) => {
            const t = gl.createTexture();
            gl.activeTexture(gl.TEXTURE0 + unit);
            gl.bindTexture(gl.TEXTURE_2D, t);
            gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
            gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
            gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
            gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
            gl.uniform1i(this.u[name], unit);
            this.tex[name] = t;
        });
        gl.clearColor(0, 0, 0, 1);
        this.masked = false;   // the mask holds the atlas at the cell's size
    };

    Drawer.prototype.setAtlas = function(data) {
        this.runes = runesOf(data);
        this.rasterize();
    };

    // rasterize uploads the glyph mask at the cell's device size
    Drawer.prototype.rasterize = function() {
        if (!this.runes.length || !this.cw || this.lost) return;
        const mask = rasterizeMask(this.runes, this.cw, this.ch);
        const gl = this.gl;
        gl.activeTexture(gl.TEXTURE2);
        gl.bindTexture(gl.TEXTURE_2D, this.tex.u_mask);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, mask.canvas);
        gl.uniform2f(this.u.u_slots, ATLAS_COLS, mask.rows);
        this.masked = true;
    };

    Drawer.prototype.resized = function() {};
    Drawer.prototype.apply = function(data) {
        applyFrame(this, data);
        this.dirty = true;
    };
    Drawer.prototype.frame = function() { return frameOf(this); };
    Drawer.prototype.origin = function() { return originOf(this); };

    Drawer.prototype.draw = function() {
        const gl = this.gl;
        if (this.lost) return;
        const W = canvas.width, H = canvas.height;
        gl.viewport(0, 0, W, H);
        gl.clear(gl.COLOR_BUFFER_BIT);
        if (!this.cols || !this.masked) return;
        if (this.dirty) {
            gl.activeTexture(gl.TEXTURE0);
            gl.bindTexture(gl.TEXTURE_2D, this.tex.u_fg);
            gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, this.cols, this.rows, 0, gl.RGBA, gl.UNSIGNED_BYTE, this.fg);
            gl.activeTexture(gl.TEXTURE1);
            gl.bindTexture(gl.TEXTURE_2D, this.tex.u_bg);
            gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, this.cols, this.rows, 0, gl.RGBA, gl.UNSIGNED_BYTE, this.bg);
            gl.uniform2f(this.u.u_grid, this.cols, this.rows);
            this.dirty = false;
        }
        const [fw, fh] = this.frame(), [ox, oy] = this.origin();
        gl.uniform4f(this.u.u_quad, -1 + 2 * ox / W, 1 - 2 * oy / H, 2 * fw / W, -2 * fh / H);
        gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    };

    /* Painter is the screen without WebGL: each changed cell's background and then
       its glyph, cut from the mask and tinted through a scratch cell, painted into a
       picture the frame's size that is then drawn on the canvas. Slower on a large
       grid, which the page says once at the start. */
    function Painter(canvas) {
        this.ctx = canvas.getContext('2d', { alpha: false });
        if (!this.ctx) throw new Error('Canvas 2D is unavailable');
        this.runes = [];
        this.cw = 0; this.ch = 0;
        this.cols = 0; this.rows = 0;
        this.fg = null; this.bg = null;
        this.mask = null;
        this.lost = false;
        this.picture = document.createElement('canvas');
        this.pic = this.picture.getContext('2d', { alpha: false });
        this.scratch = document.createElement('canvas');
        this.sc = this.scratch.getContext('2d');
    }

    Painter.prototype.setAtlas = function(data) {
        this.runes = runesOf(data);
        this.rasterize();
    };

    // rasterize draws the mask at the cell's device size and paints every cell again with it
    Painter.prototype.rasterize = function() {
        if (!this.runes.length || !this.cw) return;
        this.mask = rasterizeMask(this.runes, this.cw, this.ch).canvas;
        this.scratch.width = this.cw;
        this.scratch.height = this.ch;
        this.resized();
        if (this.fg) for (let i = 0; i < this.cols * this.rows; i++) this.paint(i);
    };

    Painter.prototype.resized = function() {
        const [fw, fh] = this.frame();
        this.picture.width = Math.max(1, fw);
        this.picture.height = Math.max(1, fh);
        this.pic.fillStyle = '#000';
        this.pic.fillRect(0, 0, this.picture.width, this.picture.height);
    };

    Painter.prototype.apply = function(data) {
        const changed = applyFrame(this, data);
        if (!this.mask) return;
        for (let i = 0; i < changed.length; i++) this.paint(changed[i]);
    };

    Painter.prototype.paint = function(i) {
        const fg = this.fg, bg = this.bg, o = i * 4, cw = this.cw, ch = this.ch;
        const x = (i % this.cols) * cw, y = Math.floor(i / this.cols) * ch;
        const pic = this.pic;
        pic.fillStyle = 'rgb(' + bg[o] + ',' + bg[o + 1] + ',' + bg[o + 2] + ')';
        pic.fillRect(x, y, cw, ch);
        const slot = fg[o + 3] + bg[o + 3] * 256 - 1;
        if (slot < 0) return;
        const sc = this.sc;
        sc.globalCompositeOperation = 'copy';
        sc.drawImage(this.mask, (slot % ATLAS_COLS) * cw, Math.floor(slot / ATLAS_COLS) * ch, cw, ch, 0, 0, cw, ch);
        sc.globalCompositeOperation = 'source-in';
        sc.fillStyle = 'rgb(' + fg[o] + ',' + fg[o + 1] + ',' + fg[o + 2] + ')';
        sc.fillRect(0, 0, cw, ch);
        pic.drawImage(this.scratch, x, y);
    };

    Painter.prototype.frame = function() { return frameOf(this); };
    Painter.prototype.origin = function() { return originOf(this); };

    Painter.prototype.draw = function() {
        const ctx = this.ctx;
        ctx.fillStyle = '#000';
        ctx.fillRect(0, 0, canvas.width, canvas.height);
        if (!this.cols || !this.mask) return;
        const [ox, oy] = this.origin();
        ctx.drawImage(this.picture, ox, oy);
    };

    // === Fitting ===
    let cellCSS = CELL;   // a cell's height in CSS pixels as drawn now; smaller under a keyboard

    // cell sizes the drawer's cell to the device pixels cellCSS covers, whole ones at
    // exactly the game's aspect, rasterizing the glyphs again when that changed
    function cell() {
        const dpr = window.devicePixelRatio || 1;
        const cw = Math.max(1, Math.round(cellCSS * dpr / 2)), ch = 2 * cw;
        if (cw !== drawer.cw || ch !== drawer.ch) {
            drawer.cw = cw;
            drawer.ch = ch;
            drawer.rasterize();
        }
    }

    // size backs the canvas with the container's device pixels
    function size() {
        const dpr = window.devicePixelRatio || 1;
        const rect = container.getBoundingClientRect();
        const W = Math.max(1, Math.round(rect.width * dpr)), H = Math.max(1, Math.round(rect.height * dpr));
        if (canvas.width !== W || canvas.height !== H) {
            canvas.width = W;
            canvas.height = H;
        }
    }

    // fit sizes the grid to the cells the container holds at cellBase and reports it
    // to Go when it changed; the grid is never fitted at a keyboard's smaller cell
    function fit() {
        cellCSS = cellBase;
        size();
        cell();
        const c = Math.max(1, Math.floor(canvas.width / drawer.cw));
        const r = Math.max(1, Math.floor(canvas.height / drawer.ch));
        if (c !== cols || r !== rows) {
            cols = c;
            rows = r;
            send({ type: 'resize', cols: cols, rows: rows });
        }
        drawer.draw();
        fitted();
        if (touch) layoutKeyboard();
    }

    // cellAt is the cell drawn under a pointer event, the nearest edge cell past the frame
    function cellAt(e) {
        const rect = canvas.getBoundingClientRect();
        const dpr = window.devicePixelRatio || 1;
        const [ox, oy] = drawer.origin();
        const fc = drawer.cols || cols, fr = drawer.rows || rows;
        const x = Math.floor(((e.clientX - rect.left) * dpr - ox) / drawer.cw);
        const y = Math.floor(((e.clientY - rect.top) * dpr - oy) / drawer.ch);
        return [Math.min(Math.max(x, 0), fc - 1), Math.min(Math.max(y, 0), fr - 1)];
    }

    function initScreen() {
        if (typeof WebAssembly === 'undefined') {
            showMessage('WebAssembly is not supported by this browser.', true);
            return false;
        }
        try {
            drawer = new Drawer(canvas);
        } catch (err) {
            // Brave and other Chromium builds offer no WebGL on a blocklisted GPU
            try {
                drawer = new Painter(canvas);
            } catch (err2) {
                showMessage('vif needs a canvas, which this browser does not provide (' + err2.message + ').', true);
                return false;
            }
            notice('No WebGL (' + err.message + '): drawing on a 2D canvas, slower on a large grid.');
        }
        if (touch) {
            document.getElementById('strip').hidden = false;   // before the first fit
            // Focus raises no keyboard while so marked; hardware keys still type through.
            input.setAttribute('inputmode', 'none');
        }
        fit();
        if (touch) wireTouch();
        return true;
    }

    /* Launch arguments, from the page and from the query string. This is how a
       session reaches the build: the fleet page hands out ?arg=-join=<ws url>,
       and nothing here constructs or validates that URL — the allocator issued
       it and the browser's own CSP decides whether it may be opened. */
    function launchArguments() {
        const pageArgs = Array.isArray(window.VIF_ARGS) ? window.VIF_ARGS : [];
        const queryArgs = new URLSearchParams(window.location.search).getAll('arg');
        const args = pageArgs.concat(queryArgs);

        if (args.length > 64 || args.some(arg => typeof arg !== 'string' || arg.length > 1024)) {
            throw new Error('invalid vif launch arguments');
        }
        return ['vif'].concat(args);
    }

    function showExit(code, reason) {
        if (code === 0) {
            showMessage('vif has exited. Reload to start again.', false);
            return;
        }
        reason = (reason || 'exit code ' + code).replace(/\.$/, '');
        showMessage('vif stopped: ' + reason + '. Reload to retry.', true);
    }

    // === WASM Loading ===
    const MB = 1048576; // 1024 * 1024 bytes in 1 Megabyte
    const fmtProgress = (received, total) => total
        ? (received / MB).toFixed(1) + ' / ' + (total / MB).toFixed(1) + ' MB'
        : (received / MB).toFixed(1) + ' MB';

    async function loadWasm() {
        const loading = document.getElementById('loading');
        let argv, bytes;

        try {
            // Before the fetch, so a refused argument vector is not paid for with
            // a download first.
            argv = launchArguments();

            const response = await fetch(WASM_PATH);
            // Missing before: instantiateStreaming was fed the 404 page and
            // reported a magic-word error instead of the HTTP status.
            if (!response.ok) throw new Error('HTTP ' + response.status);

            /* Buffered rather than instantiateStreaming, to report progress: a
               tee()'d body cannot be handed to instantiateStreaming because both
               branches share one source. Cost is that compilation starts after
               the download instead of during it. */
            if (response.body) {
                const reader = response.body.getReader();
                const chunks = [];
                let received = 0;
                let total = Number(response.headers.get('content-length')) || 0;
                for (;;) {
                    const { done, value } = await reader.read();
                    if (done) break;
                    chunks.push(value);
                    received += value.length;
                    if (total && received > total) total = 0;   // compressed length; drop it
                    loading.textContent = 'Loading vif… ' + fmtProgress(received, total);
                }
                bytes = new Uint8Array(received);
                let offset = 0;
                for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
            } else {
                bytes = new Uint8Array(await response.arrayBuffer());
            }

            loading.textContent = 'Compiling…';
        } catch (err) {
            console.error('vif: WASM load failed', err);
            showMessage('Failed to load vif (' + err.message + '). Reload to retry.', true);
            return;
        }

        program = new Worker(WORKER_PATH);
        program.onerror = function(e) {
            if (!program) return;   // a timer Go left behind throws after exit; the exit stands
            showMessage('Failed to start vif (' + e.message + '). Reload to retry.', true);
        };
        program.onmessage = function(e) {
            const msg = e.data;
            switch (msg.type) {
            case 'atlas':
                drawer.setAtlas(msg.data);
                break;
            case 'frame':
                loading.classList.add('hidden');   // compiled and drawing
                // Every frame is reported drawn, or Go holds the next until it is.
                try {
                    drawer.apply(msg.data);
                    drawer.draw();
                } finally {
                    send({ type: 'drawn' });
                }
                break;
            case 'pointer':
                pointer = { enabled: msg.enabled, motion: msg.motion };
                lastCell = null;
                break;
            case 'exit':
                showExit(msg.code, msg.reason);
                stop();
                break;
            case 'failed':
                showMessage('Failed to load vif (' + msg.reason + '). Reload to retry.', true);
                stop();
                break;
            }
        };
        // Resolved here: the worker would resolve a relative name against its own URL.
        program.postMessage({ type: 'start', exec: new URL(EXEC_PATH, document.baseURI).href,
            bytes: bytes.buffer, argv: argv, cols: cols, rows: rows }, [bytes.buffer]);
        document.addEventListener('visibilitychange', function() {
            if (program) program.postMessage({ type: 'visibility', hidden: document.hidden });
        });
        wireHandlers();
    }

    /* An ended program takes no more messages: a resize or a key posted to it
       would fail there, and its error would replace the reason it ended. */
    function stop() {
        if (!program) return;
        program.terminate();
        program = null;
        if (touch) input.blur();   // nothing left to type into, and a keyboard would cover the message
    }

    function focus() {
        input.focus({ preventScroll: true });
    }

    // === Keys ===
    /* Keys arrive on the hidden field: a hardware key as keydown, a soft keyboard's
       or a paste's text as input events once the browser has composed it. A key the
       game takes never reaches the field; one left to the browser (a Meta chord, a
       Ctrl chord a terminal has no byte for, paste) keeps its meaning there. */
    const NAMED = /^(Escape|Enter|Tab|Backspace|Delete|Arrow(Up|Down|Left|Right)|Home|End|Page(Up|Down)|Insert|F([1-9]|1[0-2]))$/;
    const CTRL = /^[a-zA-Z @[\\\]^_?]$/;   // the characters whose Ctrl chord a terminal sends

    function mods(e) {
        return (e.shiftKey ? 1 : 0) | (e.altKey ? 2 : 0) | (e.ctrlKey ? 4 : 0) | (e.metaKey ? 8 : 0);
    }

    function onKey(e) {
        if (e.isComposing || e.metaKey) return;
        // Ctrl-Shift-V and Shift-Insert paste, as in a terminal; the text arrives below
        if (e.shiftKey && ((e.ctrlKey && e.key === 'V') || e.key === 'Insert')) return;
        const char = [...e.key].length === 1;
        if (!(char ? !e.ctrlKey || CTRL.test(e.key) : NAMED.test(e.key))) return;
        send({ type: 'key', key: e.key, mods: mods(e) });
        e.preventDefault();
    }

    function onInput(e) {
        if (e.isComposing || e.inputType === 'insertCompositionText') return;   // taken whole at its end
        switch (e.inputType) {
        case 'insertText':
            if (e.data) send({ type: 'text', text: e.data });
            break;
        case 'insertLineBreak':
        case 'insertParagraph':
            send({ type: 'key', key: 'Enter', mods: 0 });
            break;
        case 'deleteContentBackward':
            send({ type: 'key', key: 'Backspace', mods: 0 });
            break;
        }
        input.value = '';
    }

    function onCompositionEnd(e) {
        if (e.data) send({ type: 'text', text: e.data });
        input.value = '';
    }

    function onPaste(e) {
        const text = e.clipboardData && e.clipboardData.getData('text');
        if (text) send({ type: 'text', text: text });
        e.preventDefault();
    }

    // === Pointer ===
    /* The pointer is reported at its cell in the terminal's numbering: buttons left
       1, middle 2, right 3, wheel up 4 and down 5, back 6, forward 7; actions press 1,
       release 2, move 3, drag 4. A touch is the left button, as on Android; motion
       once per cell, and only while Go asked for it. */
    let pointer = { enabled: false, motion: false };
    let lastCell = null;
    const BUTTONS = [1, 2, 3, 6, 7];   // PointerEvent.button to the terminal's

    function held(e) {
        if (e.pointerType === 'touch') return 1;
        return e.buttons & 1 ? 1 : e.buttons & 4 ? 2 : e.buttons & 2 ? 3 : 0;
    }

    function mouse(e, button, action) {
        const [x, y] = cellAt(e);
        send({ type: 'mouse', x: x, y: y, button: button, action: action, mods: mods(e) });
        lastCell = [x, y];
    }

    function onPointerDown(e) {
        focus();   // keys follow the pointer
        e.preventDefault();
        if (e.pointerType === 'touch') {
            pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
            if (pointers.size === 2) {
                if (pointer.enabled && lastCell) mouse(e, 1, 2);
                pinch = { dist: pinchDistance(), cell: cellBase };
                return;
            }
            if (pointers.size > 2) return;
        }
        if (!pointer.enabled) return;
        const button = e.pointerType === 'touch' ? 1 : BUTTONS[e.button];
        if (!button) return;
        canvas.setPointerCapture(e.pointerId);
        mouse(e, button, 1);
    }

    function onPointerUp(e) {
        if (e.pointerType === 'touch') {
            pointers.delete(e.pointerId);
            if (pinch) {
                if (pointers.size === 0) pinch = null;
                return;
            }
        }
        if (!pointer.enabled) return;
        const button = e.pointerType === 'touch' ? 1 : BUTTONS[e.button];
        if (button) mouse(e, button, 2);
    }

    function onPointerMove(e) {
        if (pinch) {
            onPinchMove(e);
            return;
        }
        if (!pointer.enabled) return;
        const button = held(e);
        if (!button && !pointer.motion) return;
        const [x, y] = cellAt(e);
        if (lastCell && lastCell[0] === x && lastCell[1] === y) return;
        mouse(e, button, button ? 4 : 3);
    }

    function onWheel(e) {
        e.preventDefault();
        if (e.ctrlKey) {   // a trackpad's pinch, or Ctrl and the wheel: zoom, never the page's
            if (e.deltaY) zoomTo(cellBase * (e.deltaY < 0 ? 1.1 : 1 / 1.1));
            return;
        }
        if (pointer.enabled && e.deltaY) mouse(e, e.deltaY < 0 ? 4 : 5, 1);
    }

    // === Zoom ===
    /* A zoom moves the cell the grid is fitted at, in half pixels between the
       bounds, and refits: the grid changes, and Go sees a resize as it does for a
       window, so a solo map follows it and a shared one shows more or less of
       itself. A pinch is two pointers on the canvas; the first is released to the
       game when the second lands, so no press is left held under the gesture. */
    const pointers = new Map();   // active pointers on the canvas, by id
    let pinch = null;             // { dist, cell }: the pinch's start
    let zoomFrame = 0;

    function zoomTo(cell) {
        cell = Math.min(CELL_MOST, Math.max(CELL_LEAST, Math.round(cell * 2) / 2));
        if (cell === cellBase) return;
        cellBase = cell;
        if (!zoomFrame) zoomFrame = requestAnimationFrame(function() { zoomFrame = 0; fit(); });
    }

    function pinchDistance() {
        const [a, b] = [...pointers.values()];
        return Math.hypot(a.x - b.x, a.y - b.y);
    }

    function onPinchMove(e) {
        pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
        if (pointers.size === 2 && pinch) zoomTo(pinch.cell * pinchDistance() / pinch.dist);
    }

    // === Event Wiring ===
    function wireHandlers() {
        input.addEventListener('keydown', onKey);
        input.addEventListener('input', onInput);
        input.addEventListener('compositionend', onCompositionEnd);
        input.addEventListener('paste', onPaste);

        canvas.addEventListener('pointerdown', onPointerDown);
        canvas.addEventListener('pointerup', onPointerUp);
        canvas.addEventListener('pointercancel', onPointerUp);
        canvas.addEventListener('pointermove', onPointerMove);
        canvas.addEventListener('wheel', onWheel, { passive: false });
        canvas.addEventListener('contextmenu', function(e) { e.preventDefault(); });

        let fitTimer = 0;
        function handleResize() {
            // Where a soft keyboard shrinks the page itself, the grid must not follow it.
            if ((keyboard || keyboardUp) && !pageChanged()) {
                refitPending = true;
                return;
            }
            clearTimeout(fitTimer);
            fitTimer = setTimeout(fit, 50);
        }

        // A resolution query matches one ratio only, so re-arm on every change.
        let dprQuery = null;
        function onDprChange() { watchDpr(); handleResize(); }
        function watchDpr() {
            if (dprQuery) dprQuery.removeEventListener('change', onDprChange);
            dprQuery = window.matchMedia('(resolution: ' + window.devicePixelRatio + 'dppx)');
            dprQuery.addEventListener('change', onDprChange);
        }

        new ResizeObserver(handleResize).observe(container);
        watchDpr();

        // Focus for keyboard capture; an iframe may need a click first
        focus();
    }

    // === Touch: key strip and system keyboard ===
    const KEYBOARD_MIN = 100;   // px of screen a soft keyboard takes at least; less is a toolbar
    const CELL_MIN = 7;         // smallest cell the grid is drawn at to fit above the keyboard
    let keyboard = false;       // the player asked for the system keyboard
    let keyboardSeen = false;   // and it came up, so its going away is the system dismissing it
    let keyboardUp = false;     // a keyboard the player asked for covers the page, or is still leaving
    let refitPending = false;   // a resize put off while that keyboard was asked for or up
    let fitW = 0, fitH = 0;     // page size the grid was last fitted to
    let fitFull = false;        // and whether it was fullscreen then
    let layoutKeyboard = function() {};

    function fitted() {
        fitW = document.documentElement.clientWidth;
        fitH = document.documentElement.clientHeight;
        fitFull = document.fullscreenElement != null;
        keyboardSeen = false;   // seen over the old page; the layout after the fit looks again
        refitPending = false;   // this fit took the resizes put off before it
    }

    // A rotation or a fullscreen change: the page itself resized, which no keyboard does.
    function pageChanged() {
        return document.documentElement.clientWidth !== fitW
            || (document.fullscreenElement != null) !== fitFull;
    }

    function wireTouch() {
        const strip = document.getElementById('strip');
        const kbdButton = document.getElementById('kbd');
        const escButton = document.getElementById('esc');
        const fullButton = document.getElementById('full');
        const vv = window.visualViewport;

        // A strip tap must leave focus, and with it the keyboard, where it is.
        strip.addEventListener('pointerdown', function(e) { e.preventDefault(); });
        strip.addEventListener('click', function(e) {
            const b = e.target.closest('button');
            if (!b) return;
            if (b.dataset.key) send({ type: 'key', key: b.dataset.key, mods: 0 });
            else if (b === kbdButton) setKeyboard(!keyboard, true);
            else if (b === fullButton) toggleFullscreen();
        });

        function setKeyboard(on, refocus) {
            keyboard = on;
            keyboardSeen = false;
            input.setAttribute('inputmode', on ? 'text' : 'none');
            // A focused field keeps the keyboard it has: refocusing inside the tap is what
            // raises or drops it, and the only way iOS raises one at all.
            if (refocus) {
                input.blur();
                focus();
            }
            kbdButton.setAttribute('aria-pressed', on);
            escButton.hidden = !on;
            layoutKeyboard();
        }

        /* While a keyboard covers the page the grid keeps its size, which is the solo
           map's, and is drawn at a smaller cell between the strip and the keyboard;
           below CELL_MIN it keeps the bottom rows, where commands are typed, in view.
           A zoom under a keyboard refits once the keyboard is down (fit, above). */
        function drawAt(h) {
            if (cellCSS !== h) {
                cellCSS = h;
                cell();
                drawer.draw();
            }
            return drawer.frame()[1] / (window.devicePixelRatio || 1);   // the grid's CSS height
        }

        let fullHeight = 0;   // the grid's height at cellBase
        let lastFit = null;   // the cell found for a height, kept while the keyboard holds still
        layoutKeyboard = function() {
            if (pageChanged()) return;   // measured against the old page; the refit lays out again
            const up = !!vv && fitH - vv.height * vv.scale > KEYBOARD_MIN;
            if (keyboard && up) keyboardSeen = true;
            else if (keyboard && keyboardSeen) return setKeyboard(false, false);   // dismissed by the system
            keyboardUp = up && (keyboard || keyboardUp);

            if (cellCSS === cellBase) {
                fullHeight = drawAt(cellBase);
                lastFit = null;
            }
            let shift = 0;
            if (up) {
                const avail = vv.height - strip.offsetHeight;
                let h = lastFit && lastFit.avail === avail ? lastFit.cell
                    : Math.max(CELL_MIN, Math.min(cellBase, Math.floor(2 * cellBase * avail / fullHeight) / 2));
                let drawn;
                while ((drawn = drawAt(h)) > avail && h > CELL_MIN) h -= 0.5;
                lastFit = { avail: avail, cell: h };
                shift = Math.max(0, drawn - avail);
            } else {
                drawAt(cellBase);
                // Only once the page is whole again does a put-off refit see its real size.
                if (!keyboard && refitPending) {
                    refitPending = false;
                    fit();
                }
            }
            // The page follows the visible area if the browser pans it to the keyboard.
            document.body.style.transform = up && vv.offsetTop ? 'translateY(' + vv.offsetTop + 'px)' : '';
            container.style.transform = shift ? 'translateY(' + -shift + 'px)' : '';
        };

        if (vv) {
            let queued = false;
            const schedule = function() {
                if (queued) return;
                queued = true;
                requestAnimationFrame(function() { queued = false; layoutKeyboard(); });
            };
            vv.addEventListener('resize', schedule);
            vv.addEventListener('scroll', schedule);
        }

        // Fullscreen refits the grid as a rotation does; iPhone Safari has no API for it.
        if (document.fullscreenEnabled) {
            fullButton.hidden = false;
            document.addEventListener('fullscreenchange', function() {
                fullButton.setAttribute('aria-pressed', document.fullscreenElement !== null);
                fit();   // even where the size holds
            });
        }
        function toggleFullscreen() {
            const done = document.fullscreenElement
                ? document.exitFullscreen()
                : document.documentElement.requestFullscreen({ navigationUI: 'hide' });
            done.catch(function() {});
        }
    }

    // notice shows a line at the top for a while, over a game that goes on
    function notice(msg) {
        const el = document.getElementById('notice');
        el.textContent = msg;
        el.hidden = false;
        setTimeout(function() { el.hidden = true; }, 5000);
    }

    function showMessage(msg, isError) {
        const el = document.getElementById('loading');
        el.textContent = msg;
        el.classList.remove('hidden');
        el.classList.toggle('error', isError);
    }

    // === Entry Point ===
    document.addEventListener('DOMContentLoaded', function() {
        if (initScreen()) loadWasm();
    });
})();
