/* The page's speakers: a worklet fed the mixer's 44.1 kHz 16-bit stereo PCM, a
   buffer a message, through a ring of a second of frames. Playing waits until the
   ring holds two buffers, so one may be as late as a whole mixer period; run dry,
   it plays silence and waits for two again; full, the oldest frames go. */
'use strict';

class VifSpeaker extends AudioWorkletProcessor {
    constructor() {
        super();
        this.cap = sampleRate;
        this.left = new Float32Array(this.cap);
        this.right = new Float32Array(this.cap);
        this.head = 0;        // the next frame to play
        this.held = 0;        // frames queued behind it
        this.lead = 0;        // frames held before playing: two buffers, known at the first
        this.playing = false;
        this.port.onmessage = e => this.push(e.data);
    }

    push(data) {
        const pcm = new Int16Array(data.buffer, data.byteOffset, data.byteLength >> 1);
        const n = pcm.length >> 1;
        if (!this.lead) this.lead = Math.min(2 * n, this.cap >> 1);
        if (this.held + n > this.cap) {
            const drop = this.held + n - this.cap;
            this.head = (this.head + drop) % this.cap;
            this.held -= drop;
        }
        let at = (this.head + this.held) % this.cap;
        for (let i = 0; i < n; i++) {
            this.left[at] = pcm[2 * i] / 32768;
            this.right[at] = pcm[2 * i + 1] / 32768;
            if (++at === this.cap) at = 0;
        }
        this.held += n;
    }

    // Outputs arrive zeroed, so an underrun is silence by leaving them so.
    process(inputs, outputs) {
        const out = outputs[0];
        const l = out[0], r = out[1] || out[0], n = l.length;
        if (this.held >= this.lead) this.playing = true;
        if (!this.playing || this.held < n) {
            this.playing = false;
            return true;
        }
        for (let i = 0; i < n; i++) {
            l[i] = this.left[this.head];
            r[i] = this.right[this.head];
            if (++this.head === this.cap) this.head = 0;
        }
        this.held -= n;
        return true;
    }
}

registerProcessor('vif-speaker', VifSpeaker);
