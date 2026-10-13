package com.jolufasoft.capimux;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.RandomAccessFile;
import java.nio.charset.StandardCharsets;

final class BoundedLog {
    private static final int LIMIT = 256 * 1024;
    private final File file;

    BoundedLog(File file) { this.file = file; }

    synchronized void append(byte[] data, int length) throws IOException {
        try (FileOutputStream out = new FileOutputStream(file, true)) { out.write(data, 0, length); }
        if (file.length() <= LIMIT) return;
        byte[] tail = new byte[LIMIT / 2];
        try (RandomAccessFile input = new RandomAccessFile(file, "r")) {
            input.seek(input.length() - tail.length);
            input.readFully(tail);
        }
        try (FileOutputStream out = new FileOutputStream(file, false)) { out.write(tail); }
    }

    synchronized String tail() throws IOException {
        if (!file.exists()) return "Todavía no hay registros.";
        try (RandomAccessFile input = new RandomAccessFile(file, "r")) {
            int length = (int) Math.min(input.length(), 16000);
            byte[] data = new byte[length];
            input.seek(input.length() - length);
            input.readFully(data);
            return new String(data, StandardCharsets.UTF_8);
        }
    }
}
