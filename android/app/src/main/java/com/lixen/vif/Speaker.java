package com.lixen.vif;

import android.media.AudioAttributes;
import android.media.AudioFormat;
import android.media.AudioTrack;

/** The mixer's speakers: 44.1 kHz 16-bit stereo PCM into an AudioTrack, dropped while paused. */
final class Speaker implements com.lixen.vif.game.Speaker {
    private static final int RATE = 44100;
    private static final int BUFFER = 2 * RATE * 4 / 20; // two of the mixer's 50 ms periods

    private final AudioTrack track;
    private volatile boolean paused;
    private boolean closed; // the run closes the speakers before the activity pauses

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
                .setBufferSizeInBytes(Math.max(min, BUFFER))
                .build();
        track.play();
    }

    @Override
    public void write(byte[] pcm) {
        if (!paused) {
            track.write(pcm, 0, pcm.length, AudioTrack.WRITE_BLOCKING);
        }
    }

    @Override
    public synchronized void close() {
        closed = true;
        track.stop();
        track.release();
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
