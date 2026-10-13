package com.jolufasoft.capimux;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.AtomicFile;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

final class ModelStore {
    private final AtomicFile file;
    ModelStore(Context context) { file = new AtomicFile(path(context)); }
    static File path(Context context) { return new File(context.getFilesDir(), "multiagent-policy.json"); }
    JSONObject read() throws Exception {
        return new JSONObject(new String(file.readFully(), StandardCharsets.UTF_8));
    }
    List<String> models() throws Exception {
        JSONArray array = read().getJSONArray("models");
        List<String> result = new ArrayList<>();
        for (int i = 0; i < array.length(); i++) result.add(array.getString(i));
        return result;
    }
    void migrate(Context context) throws Exception {
        if (file.getBaseFile().exists()) return;
        SharedPreferences old = context.getSharedPreferences("engine", Context.MODE_PRIVATE);
        List<String> models = new ArrayList<>();
        for (String id : old.getString("models", "").split(",")) {
            if (!id.trim().isEmpty()) models.add(id.trim());
        }
        save(models, old.getBoolean("external", false));
    }
    void save(List<String> models, boolean external) throws Exception {
        JSONObject value = new JSONObject();
        value.put("models", new JSONArray(models));
        value.put("external_enabled", external);
        FileOutputStream out = null;
        try {
            out = file.startWrite();
            out.write(value.toString().getBytes(StandardCharsets.UTF_8));
            file.finishWrite(out);
        } catch (Exception error) {
            if (out != null) file.failWrite(out);
            throw error;
        }
    }
}
