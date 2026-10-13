package com.jolufasoft.capimux;
import org.junit.Test;
import java.io.File;
import static org.junit.Assert.*;

public class BoundedLogTest {
    @Test public void logKeepsRecentOutputWithoutGrowingForever() throws Exception {
        File file = File.createTempFile("capimux-log", ".txt");
        try {
            BoundedLog log = new BoundedLog(file);
            byte[] chunk = new byte[4096];
            java.util.Arrays.fill(chunk, (byte) 'a');
            for (int i = 0; i < 100; i++) log.append(chunk, chunk.length);
            log.append("latest".getBytes(java.nio.charset.StandardCharsets.UTF_8), 6);
            assertTrue(file.length() <= 256 * 1024);
            assertTrue(log.tail().endsWith("latest"));
        } finally { file.delete(); }
    }
}
