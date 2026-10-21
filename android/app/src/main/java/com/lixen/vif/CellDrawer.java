package com.lixen.vif;

import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.Rect;

import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.IntBuffer;

/**
 * Paints the game's frames as Game.frame hands them over and posts the picture to
 * the view's surface, stretched over all of it. OnCanvas paints cells from the
 * atlas texture; a GLES drawer takes the same frames. FromRaster shows Go's
 * software raster, the fallback.
 */
interface CellDrawer {
    /** Applies a frame from the run; false when it changed nothing. */
    boolean apply(byte[] frame);

    /** Draws what the drawer holds over the whole surface. */
    void draw(Canvas surface);

    /** The strip's height on a surface this tall: the last row at twice its size. */
    int stripHeight(int surfaceHeight);

    /** Draws the last row's left half, the status bar's mode and input, above a keyboard covering the bottom pixels. */
    void drawStrip(Canvas surface, int covered);

    /**
     * The picture painted so far at the atlas's size, kept between frames. The GPU
     * stretches it over the surface, filtered, so the grid leaves no margin; painting
     * each cell at the screen's size would cost the CPU every enlarged pixel.
     */
    abstract class Picture implements CellDrawer {
        final int cellWidth, cellHeight;
        Bitmap screen;
        private final Paint pixels = new Paint();
        private final Rect onSurface = new Rect(), stripSrc = new Rect(), stripDst = new Rect();

        Picture(byte[] atlas) {
            IntBuffer a = words(atlas, 0);
            cellWidth = a.get(0);
            cellHeight = a.get(1);
            pixels.setFilterBitmap(true);
        }

        /** Sizes the picture to a grid, keeping it while the grid holds. */
        final void size(int cols, int rows) {
            int w = cols * cellWidth, h = rows * cellHeight;
            if (screen == null || screen.getWidth() != w || screen.getHeight() != h) {
                screen = Bitmap.createBitmap(Math.max(1, w), Math.max(1, h), Bitmap.Config.ARGB_8888);
                resized();
            }
        }

        void resized() {
        }

        @Override
        public final void draw(Canvas surface) {
            if (screen == null) {
                surface.drawColor(Color.BLACK);
                return;
            }
            onSurface.set(0, 0, surface.getWidth(), surface.getHeight());
            surface.drawBitmap(screen, null, onSurface, pixels);
        }

        @Override
        public final int stripHeight(int surfaceHeight) {
            return screen == null ? 0 : 2 * surfaceHeight * cellHeight / screen.getHeight();
        }

        // Twice the size each way, so the glyphs keep their shape: the left half fills
        // the width, and the run keeps its input to that half while the keyboard is up
        @Override
        public final void drawStrip(Canvas surface, int covered) {
            if (screen == null) {
                return;
            }
            int bottom = surface.getHeight() - covered;
            stripSrc.set(0, screen.getHeight() - cellHeight, screen.getWidth() / 2, screen.getHeight());
            stripDst.set(0, bottom - stripHeight(surface.getHeight()), surface.getWidth(), bottom);
            surface.drawBitmap(screen, stripSrc, stripDst, pixels);
        }
    }

    /** Paints each changed cell: its background, then its glyph in the foreground. */
    final class OnCanvas extends Picture {
        private final Bitmap atlas; // glyphs one above the other, alpha only
        private final Paint paint = new Paint();
        private final Rect src = new Rect(), dst = new Rect();
        private Canvas canvas;

        OnCanvas(byte[] atlas) {
            super(atlas);
            int glyphs = words(atlas, 0).get(2);
            this.atlas = Bitmap.createBitmap(cellWidth, Math.max(1, glyphs * cellHeight), Bitmap.Config.ALPHA_8);
            this.atlas.copyPixelsFromBuffer(ByteBuffer.wrap(atlas, 12, cellWidth * cellHeight * glyphs));
            paint.setAntiAlias(false);
            paint.setFilterBitmap(false);
        }

        @Override
        void resized() {
            canvas = new Canvas(screen);
        }

        @Override
        public boolean apply(byte[] frame) {
            IntBuffer f = words(frame, 0);
            int cols = f.get(0), n = f.get(2);
            size(cols, f.get(1));
            for (int i = 0, at = 3; i < n; i++, at += 4) {
                int cell = f.get(at), slot = f.get(at + 1);
                int x = cell % cols * cellWidth, y = cell / cols * cellHeight;
                dst.set(x, y, x + cellWidth, y + cellHeight);
                paint.setColor(f.get(at + 3));
                canvas.drawRect(dst, paint);
                if (slot >= 0) {
                    src.set(0, slot * cellHeight, cellWidth, (slot + 1) * cellHeight);
                    paint.setColor(f.get(at + 2));
                    canvas.drawBitmap(atlas, src, dst, paint);
                }
            }
            return n > 0;
        }
    }

    /** Shows the RGBA picture Go's raster painted. */
    final class FromRaster extends Picture {
        FromRaster(byte[] atlas) {
            super(atlas);
        }

        @Override
        public boolean apply(byte[] frame) {
            IntBuffer f = words(frame, 0);
            size(f.get(0), f.get(1));
            screen.copyPixelsFromBuffer(ByteBuffer.wrap(frame, 8, frame.length - 8));
            return true;
        }
    }

    static IntBuffer words(byte[] b, int offset) {
        return ByteBuffer.wrap(b, offset, b.length - offset).slice().order(ByteOrder.LITTLE_ENDIAN).asIntBuffer();
    }
}
