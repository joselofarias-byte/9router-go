package com.jolufasoft.capimux;

import android.Manifest;
import android.annotation.SuppressLint;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;
import android.view.View;
import android.view.WindowInsets;
import android.webkit.CookieManager;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import java.io.ByteArrayInputStream;
import java.io.File;

public final class MainActivity extends Activity {
    private final Handler ui = new Handler(Looper.getMainLooper());
    private WebView panel;
    private TextView status;
    private boolean panelLoaded;
    private boolean restarting;
    private final Runnable refreshStatus = new Runnable() {
        @Override public void run() {
            String current = EngineService.status();
            if (!current.contentEquals(status.getText())) status.setText(current);
            if (EngineService.ready() && !panelLoaded) {
                panelLoaded = true;
                panel.loadUrl(NavigationPolicy.ORIGIN + "/dashboard");
            }
            if (!EngineService.ready()) panelLoaded = false;
            ui.postDelayed(this, 750);
        }
    };

    @Override protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        buildUI();
        if (Build.VERSION.SDK_INT >= 33) {
            getOnBackInvokedDispatcher().registerOnBackInvokedCallback(0, () -> {
                if (panel.canGoBack()) panel.goBack(); else finish();
            });
            if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 1);
            }
        }
        startEngine();
    }

    @SuppressLint("SetJavaScriptEnabled")
    private void buildUI() {
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackgroundColor(Color.rgb(17, 24, 39));
        root.setPadding(12, 8, 12, 0);
        root.setOnApplyWindowInsetsListener((view, insets) -> {
            view.setPadding(12, insets.getSystemWindowInsetTop() + 8, 12, insets.getSystemWindowInsetBottom());
            return insets;
        });
        TextView title = new TextView(this);
        title.setText("Capimux");
        title.setTextColor(Color.rgb(34, 211, 238));
        title.setTextSize(24);
        root.addView(title);
        status = new TextView(this);
        status.setTextColor(Color.LTGRAY);
        status.setTextSize(12);
        root.addView(status);
        LinearLayout controls = row(root);
        button(controls, "Iniciar", this::startEngine);
        button(controls, "Detener", () -> stopService(new Intent(this, EngineService.class)));
        button(controls, "Recargar", () -> {
            if (EngineService.ready()) panel.reload();
        });
        LinearLayout tools = row(root);
        button(tools, "Modelos", this::configureModels);
        button(tools, "Registro", this::showLog);
        button(tools, "Batería", () -> {
            try { startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)); }
            catch (ActivityNotFoundException e) { Toast.makeText(this, "Abrí los ajustes de batería del teléfono.", Toast.LENGTH_SHORT).show(); }
        });
        panel = new WebView(this);
        panel.setBackgroundColor(Color.rgb(17, 24, 39));
        WebSettings settings = panel.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setAllowFileAccess(false);
        settings.setAllowContentAccess(false);
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        CookieManager.getInstance().setAcceptThirdPartyCookies(panel, false);
        WebView.setWebContentsDebuggingEnabled(false);
        panel.setWebViewClient(new WebViewClient() {
            @Override public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                String url = request.getUrl().toString();
                if (NavigationPolicy.isLocal(url)) return false;
                if (request.isForMainFrame() && NavigationPolicy.isExternalWeb(url)) {
                    try { startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(url))); }
                    catch (ActivityNotFoundException e) { Toast.makeText(MainActivity.this, "No hay navegador disponible.", Toast.LENGTH_SHORT).show(); }
                }
                return true;
            }
            @Override public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest request) {
                if (NavigationPolicy.isLocal(request.getUrl().toString())) return null;
                return new WebResourceResponse("text/plain", "UTF-8", 403, "Blocked", java.util.Collections.emptyMap(), new ByteArrayInputStream(new byte[0]));
            }
        });
        root.addView(panel, new LinearLayout.LayoutParams(-1, 0, 1));
        setContentView(root);
    }

    private LinearLayout row(LinearLayout parent) {
        LinearLayout row = new LinearLayout(this);
        parent.addView(row, new LinearLayout.LayoutParams(-1, -2));
        return row;
    }

    private void button(LinearLayout row, String text, Runnable action) {
        Button button = new Button(this);
        button.setText(text);
        button.setTextSize(12);
        button.setAllCaps(false);
        button.setOnClickListener(view -> action.run());
        row.addView(button, new LinearLayout.LayoutParams(0, -2, 1));
    }

    private void startEngine() {
        if (!EngineService.idle() && EngineService.status().startsWith("Deteniendo")) {
            if (!restarting) { restarting = true; awaitStopped(50); }
            return;
        }
        try { EngineService.start(this); }
        catch (RuntimeException e) {
            Toast.makeText(this, "No se pudo iniciar el servicio. Abrí nuevamente la app.", Toast.LENGTH_LONG).show();
        }
    }

    private void configureModels() {
        if (!EngineService.ready()) {
            Toast.makeText(this, "Iniciá el motor para cargar los modelos.", Toast.LENGTH_SHORT).show();
            return;
        }
        new ModelPicker(this,
            () -> panel.loadUrl(NavigationPolicy.ORIGIN + "/dashboard/providers"),
            () -> panel.loadUrl(NavigationPolicy.ORIGIN + "/dashboard/multiagent")).show();
    }

    private void awaitStopped(int attempts) {
        if (EngineService.idle()) { restarting = false; startEngine(); return; }
        if (attempts == 0) {
            restarting = false;
            Toast.makeText(this, "El motor sigue cerrando. Intentá Iniciar cuando se detenga.", Toast.LENGTH_LONG).show();
            return;
        }
        ui.postDelayed(() -> awaitStopped(attempts - 1), 500);
    }

    private void showLog() {
        String text;
        try { text = new BoundedLog(new File(getFilesDir(), "engine.log")).tail(); }
        catch (Exception e) { text = "No se pudo leer el registro."; }
        TextView view = new TextView(this);
        view.setTextIsSelectable(true);
        view.setTextSize(11);
        view.setPadding(24, 12, 24, 12);
        view.setText(text);
        android.widget.ScrollView scroll = new android.widget.ScrollView(this);
        scroll.addView(view);
        new AlertDialog.Builder(this).setTitle("Registro del motor").setView(scroll).setPositiveButton("Cerrar", null).show();
    }

    @Override protected void onStart() { super.onStart(); ui.post(refreshStatus); }
    @Override protected void onStop() { ui.removeCallbacks(refreshStatus); CookieManager.getInstance().flush(); super.onStop(); }
    @Override protected void onDestroy() {
        ui.removeCallbacksAndMessages(null);
        panel.destroy();
        super.onDestroy();
    }
    @Override public void onBackPressed() { if (panel.canGoBack()) panel.goBack(); else super.onBackPressed(); }
}
