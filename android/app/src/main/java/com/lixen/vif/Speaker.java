package com.lixen.vif;

import android.media.AudioAttributes;
import android.media.AudioFormat;
import android.media.AudioTrack;

/**
 * The mixer's speakers: 44.1 kHz 16-bit stereo PCM into an AudioTrack, dropped while
 * paused. The track holds one mixer period, the latency the terminal's backends are
 * opened at, and asks for the low-latency output path: a second period here was a
 * game tick between a sound's cause and its hearing.
 */
final class Speaker implements com.lixen.vif.game.Speaker {
    private static final int RATE = 44100;
    private static final int BUFFER = RATE * 4 / 20; // the mixer's 50 ms period

    private final AudioTrack track;
    private volatile boolean paused;
    private volatile boolean closed; // the activity releases the track as it finishes

    Speaker() {
        int min = AudioTrack.getMinBufferSize(RATE, AudioFormat.CHANNEL_OUT_STEREO, AudioFormat.ENCODING_PCM_16BIT);
        track = new AudioTrack.Builder()
                .setAudioAttributes(new AudioAttributes.Builder()
                        .setUsage(AudioAttributes.USAGE_GAME)
                        .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                        .build())
                .setAudioFormat(new AudioFormat.Builder()
                        .setSampleRate(RATE)
                        .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                        .setChannelMask(AudioFormat.CHANNEL_OUT_STEREO)
                        .build())
                .setTransferMode(AudioTrack.MODE_STREAM)
                .setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY)
                .setBufferSizeInBytes(Math.max(min, BUFFER))
                .build();
        track.play();
    }

    @Override
    public void write(byte[] pcm) {
        if (!paused && !closed) {
            track.write(pcm, 0, pcm.length, AudioTrack.WRITE_BLOCKING);
        }
    }

    /** The speakers are the activity's, not a run's: a scenario change keeps them. */
    @Override
    public synchronized void close() {
        if (!closed) {
            closed = true;
            track.stop();
            track.release();
        }
    }

    /** Flushing frees a write blocked on the paused track. */
    synchronized void pause() {
        if (!closed) {
            paused = true;
            track.pause();
            track.flush();
        }
    }

    synchronized void resume() {
        if (!closed) {
            paused = false;
            track.play();
        }
    }
}
