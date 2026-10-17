package com.lixen.vif;

import static org.junit.Assert.assertArrayEquals;

import androidx.test.ext.junit.runners.AndroidJUnit4;

import com.lixen.vif.game.Game;
import com.lixen.vif.game.Probe;

import java.nio.ByteBuffer;

import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class CellDrawerTest {
    /** The Canvas drawer paints the probe to the pixels Go's raster draws for it. */
    @Test
    public void theCanvasDrawerPaintsTheRastersPixels() throws Exception {
        Probe probe = Game.newProbe(16);
        CellDrawer.OnCanvas drawer = new CellDrawer.OnCanvas(Game.atlas());
        drawer.apply(probe.frame());
        ByteBuffer got = ByteBuffer.allocate(drawer.screen.getByteCount());
        drawer.screen.copyPixelsToBuffer(got);
        assertArrayEquals(probe.pixels(), got.array());
    }
}
