package com.lixen.vif;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;

import androidx.test.ext.junit.runners.AndroidJUnit4;

import com.lixen.vif.game.Game;
import com.lixen.vif.game.Probe;

import java.nio.ByteBuffer;

import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class CellDrawerTest {
    /** The Canvas drawer paints each strike's probe to the pixels Go's raster draws for it. */
    @Test
    public void theCanvasDrawerPaintsTheRastersPixels() {
        for (int height : new int[] {12, 16, 24}) {
            Probe probe = Game.newProbe(16, height);
            CellDrawer.OnCanvas drawer = new CellDrawer.OnCanvas(probe.atlas());
            assertEquals("the font has no strike of this height", height, drawer.cellHeight);
            drawer.apply(probe.frame());
            ByteBuffer got = ByteBuffer.allocate(drawer.screen.getByteCount());
            drawer.screen.copyPixelsToBuffer(got);
            assertArrayEquals("the strike " + height + " pixels tall", probe.pixels(), got.array());
        }
    }
}
