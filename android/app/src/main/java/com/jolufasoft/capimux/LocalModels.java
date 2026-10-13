package com.jolufasoft.capimux;

import android.content.Context;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

final class LocalModels {
    static final class Entry {
        final String id;
        final String provider;
        final String label;
        Entry(String id, String provider) {
            this.id = id;
            this.provider = provider;
            int slash = id.indexOf('/');
            this.label = slash >= 0 ? id.substring(slash + 1) : id;
        }
    }
    static List<Entry> load(Context context) throws Exception {
        HttpURLConnection connection = (HttpURLConnection) new URL(NavigationPolicy.ORIGIN + "/api/multiagent/models").openConnection();
        connection.setConnectTimeout(3000);
        connection.setReadTimeout(10000);
        connection.setInstanceFollowRedirects(false);
        try {
            // Initialize the gateway's auth identity, then derive it entirely in
            // app-private native code; never expose the token to JavaScript.
            connection.setRequestProperty("x-9r-cli-token", cliToken(context));
            if (connection.getResponseCode() != 200) throw new IllegalStateException("El motor no pudo cargar los modelos.");
            byte[] body = boundedRead(connection.getInputStream());
            JSONArray data = new JSONObject(new String(body, StandardCharsets.UTF_8)).getJSONArray("data");
            Map<String, Entry> entries = new LinkedHashMap<>();
            for (int i = 0; i < data.length(); i++) {
                JSONObject item = data.getJSONObject(i);
                String id = item.getString("id");
                entries.put(id, new Entry(id, item.optString("provider", item.optString("owned_by", "Proveedor"))));
            }
            List<Entry> result = new ArrayList<>(entries.values());
            result.sort(Comparator.comparing((Entry e) -> e.provider).thenComparing(e -> e.label));
            return result;
        } finally { connection.disconnect(); }
    }
    private static String cliToken(Context context) throws Exception {
        File base = new File(context.getFilesDir(), "engine");
        File secretFile = new File(base, "auth/cli-secret");
        if (!secretFile.exists()) {
            HttpURLConnection probe = (HttpURLConnection) new URL(NavigationPolicy.ORIGIN + "/api/multiagent/models").openConnection();
            probe.setConnectTimeout(3000);
            probe.setReadTimeout(3000);
            probe.setInstanceFollowRedirects(false);
            probe.setRequestProperty("x-9r-cli-token", "initialize-local-auth");
            try { probe.getResponseCode(); } finally { probe.disconnect(); }
        }
        String machine = new String(Files.readAllBytes(new File(base, "machine-id").toPath()), StandardCharsets.UTF_8).trim();
        String secret = new String(Files.readAllBytes(secretFile.toPath()), StandardCharsets.UTF_8).trim();
        byte[] digest = MessageDigest.getInstance("SHA-256").digest((machine + "9r-cli-auth" + secret).getBytes(StandardCharsets.UTF_8));
        StringBuilder hex = new StringBuilder();
        for (int i = 0; i < 8; i++) hex.append(String.format(java.util.Locale.ROOT, "%02x", digest[i] & 255));
        return hex.toString();
    }
    private static byte[] boundedRead(InputStream input) throws Exception {
        try (InputStream in = input; ByteArrayOutputStream out = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[4096];
            int count;
            while ((count = in.read(buffer)) != -1) {
                if (out.size() + count > 2 * 1024 * 1024) throw new IllegalStateException("La lista de modelos es demasiado grande.");
                out.write(buffer, 0, count);
            }
            return out.toByteArray();
        }
    }
}
